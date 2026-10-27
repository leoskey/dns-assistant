package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRecordPagesFilterAndSnapshot(t *testing.T) {
	a, h := testAPI(t)
	for i := 0; i < 43; i++ {
		r := result("1.1.1.1", float64(i), i%2 == 0)
		r.Time = time.Now().UTC()
		if err := a.m.store.AddResult(r); err != nil {
			t.Fatal(err)
		}
		a.m.Observe(Query{Domain: "example.com", Client: "192.168.1.5", Type: "A"})
		if err := a.m.store.Event(fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	for _, endpoint := range []string{"queries", "history", "history/events", "history/daily"} {
		for _, query := range []string{"", "?page_size=10"} {
			var page Page[json.RawMessage]
			resp := call(h, "GET", "/api/v1/"+endpoint+query, "", "", "")
			if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &page) != nil || page.PageSize != 10 || page.Page != 1 || len(page.Items) != min(10, page.Total) {
				t.Fatal("ten-record default", endpoint, query, resp.Body.String())
			}
		}
	}
	var first Page[Result]
	resp := call(h, "GET", "/api/v1/history?page=1&page_size=20&filter=failed", "", "", "")
	if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &first) != nil || first.Total != 21 || len(first.Items) != 20 || first.Pages != 2 {
		t.Fatal(resp.Body.String())
	}
	for _, r := range first.Items {
		if r.OK {
			t.Fatal("filter applied after pagination")
		}
	}
	r := result("8.8.8.8", 100, false)
	r.Time = time.Now().UTC()
	if err := a.m.store.AddResult(r); err != nil {
		t.Fatal(err)
	}
	var second Page[Result]
	url := fmt.Sprintf("/api/v1/history?page=2&page_size=20&filter=failed&snapshot=%d", first.Snapshot)
	resp = call(h, "GET", url, "", "", "")
	if json.Unmarshal(resp.Body.Bytes(), &second) != nil || second.Total != 21 || len(second.Items) != 1 || second.Items[0].ID != 2 {
		t.Fatal(resp.Body.String())
	}
	for _, endpoint := range []string{"queries", "history/events"} {
		var page Page[json.RawMessage]
		resp = call(h, "GET", "/api/v1/"+endpoint+"?page=3&page_size=20", "", "", "")
		if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &page) != nil || page.Total != 43 || len(page.Items) != 3 || page.Page != 3 {
			t.Fatal(endpoint, resp.Body.String())
		}
	}
	for _, query := range []string{"page=0", "page=-1", "page=abc", "page_size=1000", "page_size=0", "snapshot=-1", "snapshot=bad", "page=1&page=2", "snapshot=1&snapshot=2", "filter=unknown"} {
		resp = call(h, "GET", "/api/v1/history?"+query, "", "", "")
		if resp.Code != 400 {
			t.Fatal("invalid page accepted", query, resp.Body.String())
		}
	}
	var status map[string]json.RawMessage
	resp = call(h, "GET", "/api/v1/status", "", "", "")
	json.Unmarshal(resp.Body.Bytes(), &status)
	if _, ok := status["queries"]; ok {
		t.Fatal("status still transfers logs")
	}
}

func TestRecordRetentionBoundsAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, at := range []time.Time{now.AddDate(0, 0, -40), now.AddDate(0, 0, -8), now} {
		q := Query{Domain: "example.com", Client: "192.168.1.5", Type: "A", Time: at}
		if err = s.AddQuery(q); err != nil {
			t.Fatal(err)
		}
		r := result("1.1.1.1", 100, true)
		r.Time = at
		if err = s.AddResult(r); err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.Exec("INSERT INTO events(time,message) VALUES(?,?)", at.Format(time.RFC3339Nano), "event"); err != nil {
			t.Fatal(err)
		}
	}
	c := DefaultConfig()
	if err = s.SaveState(c, map[string]Selection{}, 7); err != nil {
		t.Fatal(err)
	}
	if err = s.pruneAt(now); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"queries": 1, "results": 2, "events": 2} {
		var n int
		if err = s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatal(table, n, err)
		}
	}
	// Bulk insert models an older database or a burst. The next append must
	// enforce the cap without waiting for the hourly age cleanup.
	for _, log := range []struct {
		table, column string
		limit         int
	}{{"queries", "payload", queryLimit}, {"results", "payload", resultLimit}, {"events", "message", eventLimit}} {
		_, err = s.db.Exec("WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO "+log.table+"(time,"+log.column+") SELECT ?, '{}' FROM n", log.limit+5, now.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.appendLog(log.table, log.column, now, "{}", log.limit); err != nil {
			t.Fatal(err)
		}
		var n int
		s.db.QueryRow("SELECT COUNT(*) FROM " + log.table).Scan(&n)
		if n != log.limit {
			t.Fatal(log.table, n)
		}
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version uint64
	if err = s.Get("version", &version); err != nil || version != 7 {
		t.Fatal("cleanup changed active state", version, err)
	}
	page, err := s.QueryPage(PageRequest{Page: 100000, Size: 100})
	if err != nil || page.Page != 100 || page.Total != queryLimit {
		t.Fatal("restart/clamp", page, err)
	}
}

func TestRecordMaintenanceRunsWhileIdleAndStops(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := time.Now().AddDate(0, 0, -40).Format(time.RFC3339Nano)
	if _, err = s.db.Exec("INSERT INTO events(time,message) VALUES(?,?)", old, "startup"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.maintain(ctx, 20*time.Millisecond); close(done) }()
	defer func() { cancel(); <-done }()
	waitEmpty := func() {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			var n int
			if err = s.db.QueryRow("SELECT COUNT(*) FROM events").Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("idle cleanup did not run")
	}
	waitEmpty()
	if _, err = s.db.Exec("INSERT INTO events(time,message) VALUES(?,?)", old, "periodic"); err != nil {
		t.Fatal(err)
	}
	waitEmpty()
}

func TestRecordSchemaMigrationKeepsConfiguration(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	c.RuleSource = "retained"
	if err = s.Set("config", c); err != nil {
		t.Fatal(err)
	}
	r := result("1.1.1.1", 20, true)
	r.Time = time.Now().UTC()
	if err = s.AddResult(r); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DROP TABLE queries; DROP TABLE daily; CREATE TABLE budgets(day TEXT PRIMARY KEY,used INTEGER NOT NULL); INSERT INTO budgets VALUES('2026-10-20',1000); PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var restored Config
	if err = s.Get("config", &restored); err != nil || restored.RuleSource != "retained" {
		t.Fatal(err, restored)
	}
	var schema, budgets int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&schema); err != nil || schema != 3 {
		t.Fatal("schema migration", schema, err)
	}
	s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='budgets'").Scan(&budgets)
	if budgets != 0 {
		t.Fatal("retired budget table remains")
	}
	if history, e := s.History(20); e != nil || len(history) != 1 {
		t.Fatal("migration lost history", history, e)
	}
	if err = s.AddQuery(Query{Domain: "example.com", Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if newer, err := OpenStore(dir); err == nil {
		newer.Close()
		t.Fatal("newer schema accepted")
	} else if !strings.Contains(err.Error(), "999") {
		t.Fatal(err)
	}
}
