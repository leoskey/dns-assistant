package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	m, e := NewManager(s, "http://127.0.0.1:1", strings.Repeat("s", 40))
	if e != nil {
		t.Fatal(e)
	}
	c := m.Config()
	c.Rules = c.Rules[:1]
	c.ProbeSeconds = 1
	if e = m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	return m
}
func TestAllCandidatesCompeteRegardlessOfLatency(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	r := c.Rules[0]
	good := httptest.NewServer(http.HandlerFunc(streamSample))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer bad.Close()
	ips := []string{}
	for i := 1; i <= 9; i++ {
		ips = append(ips, "9.9.9."+string(rune('0'+i)))
	}
	m.resolve = func(context.Context, string, Config) []string {
		return append(ips, "9.9.9.9", "2606:4700:4700::1111", "127.0.0.1")
	}
	var dials atomic.Int64
	m.prober = Prober{dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		dials.Add(1)
		target := bad.URL
		if strings.HasPrefix(a, "9.9.9.9:") {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
			target = good.URL
		}
		return (&net.Dialer{}).DialContext(ctx, n, strings.TrimPrefix(target, "http://"))
	}}
	m.execute(context.Background(), Job{RuleID: r.ID, Kind: "throughput"}, r, c)
	if dials.Load() != 9 {
		t.Fatal("candidate omitted or duplicated", dials.Load())
	}
	if got := m.selections[key(r.Domain, "A")]; got.IP != "9.9.9.9" {
		t.Fatal("late/slow-connect fast candidate excluded", got)
	}
	history, e := m.store.History(20)
	if e != nil || len(history) != 9 {
		t.Fatal(len(history), e)
	}
}
func TestProbeUnboundedBytesAndSetupExcluded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-" {
			t.Error("throughput range imposes byte ceiling", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		chunk := bytes.Repeat([]byte("z"), 1<<20)
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
			if _, e := w.Write(chunk); e != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	p := Prober{dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		return (&net.Dialer{}).DialContext(ctx, n, strings.TrimPrefix(server.URL, "http://"))
	}}
	c := DefaultConfig()
	c.ProbeSeconds = 1
	v := p.Probe(context.Background(), c.Rules[0], "1.1.1.1", c, "throughput")
	if !v.OK || v.Bytes <= 8<<20 || v.MeasuredMS < 700 || v.MeasuredMS > 1000 || v.DurationMS < 1150 {
		t.Fatal("byte cap or setup in sample", v)
	}
	if v.MeasuredBytes <= 0 || v.MeasuredBytes >= v.Bytes {
		t.Fatal("warmup not omitted", v)
	}
	short, _ := probeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(bytes.Repeat([]byte("z"), 65536))
	})
	if got := short.Probe(context.Background(), c.Rules[0], "1.1.1.1", c, "throughput"); got.OK || !strings.Contains(got.Error, "样本过短") {
		t.Fatal("tiny sample ranked", got)
	}
}
func TestDNSFallbackRetestAndRecoveryIntegration(t *testing.T) {
	d, _ := startDNS(t)
	m := testManager(t)
	c := m.Config()
	r := c.Rules[0]
	control := httptest.NewServer(d.Handler())
	defer control.Close()
	m.dnsURL = control.URL
	m.token = d.token
	// The controlled DNS fixture uses loopback upstreams only through the explicit test mode.
	up := d.policy.Load().Config.Upstreams
	m.config.Upstreams = up
	c = m.Config()
	m.version = 1
	m.Publish(context.Background())
	if got := query(t, d, r.Domain, dns.TypeA, false, false, false); !strings.Contains(got.String(), "9.9.9.9") {
		t.Fatal("initial default", got)
	}
	m.selections[key(r.Domain, "A")] = Selection{RuleID: r.ID, Domain: r.Domain, Family: "A", IP: "1.1.1.1", ValidUntil: time.Now().Add(time.Hour)}
	m.version++
	m.Publish(context.Background())
	bad, _ := probeFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	m.prober = bad
	m.execute(context.Background(), Job{RuleID: r.ID, Kind: "health"}, r, c)
	if got := query(t, d, r.Domain, dns.TypeA, true, false, false); !strings.Contains(got.String(), "9.9.9.9") || strings.Contains(got.String(), "1.1.1.1") {
		t.Fatal("failed health did not restore default", got)
	}
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	m.prober, _ = probeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			streamSample(w, r)
		case <-r.Context().Done():
		}
	})
	goodProbe := m.prober
	m.prober.dial = func(ctx context.Context, n, addr string) (net.Conn, error) {
		if strings.HasPrefix(addr, "1.1.1.1:") {
			return bad.dial(ctx, n, addr)
		}
		return goodProbe.dial(ctx, n, addr)
	}
	m.resolve = func(context.Context, string, Config) []string { return []string{"8.8.8.8"} }
	done := make(chan struct{})
	go func() { m.execute(context.Background(), Job{RuleID: r.ID, Kind: "throughput"}, r, c); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("retest not started")
	}
	if got := query(t, d, r.Domain, dns.TypeA, false, false, false); !strings.Contains(got.String(), "9.9.9.9") {
		t.Fatal("default not retained during recovery", got)
	}
	close(release)
	<-done
	m.Publish(context.Background())
	if got := query(t, d, r.Domain, dns.TypeA, false, false, false); !strings.Contains(got.String(), "8.8.8.8") {
		t.Fatal("verified recovery not applied", got)
	}
	if got := query(t, d, r.Domain, dns.TypeAAAA, true, false, false); len(got.Answer) != 0 || got.Rcode != 0 {
		t.Fatal("target AAAA", got)
	}
	if got := query(t, d, "ordinary.example", dns.TypeAAAA, false, false, false); !strings.Contains(got.String(), "2606:") {
		t.Fatal("ordinary DNS changed", got)
	}
}
func TestRetestKeepsCurrentAndDefaultCancelsResults(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	r := c.Rules[0]
	old := Selection{RuleID: r.ID, Domain: r.Domain, Family: "A", IP: "1.1.1.1", ValidUntil: time.Now().Add(time.Hour)}
	m.selections[key(r.Domain, "A")] = old
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	m.prober, _ = probeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			streamSample(w, r)
		case <-r.Context().Done():
		}
	})
	m.resolve = func(context.Context, string, Config) []string { return []string{"8.8.8.8"} }
	done := make(chan struct{})
	go func() { m.execute(context.Background(), Job{RuleID: r.ID, Kind: "throughput"}, r, c); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("probe not started")
	}
	m.mu.RLock()
	during := m.selections[key(r.Domain, "A")]
	m.mu.RUnlock()
	if during.IP != old.IP || !during.ValidUntil.After(time.Now()) {
		t.Fatal("retest cleared current", during)
	}
	if e := m.RestoreDefault(r.ID); e != nil {
		t.Fatal(e)
	}
	close(release)
	<-done
	if got := m.selections[key(r.Domain, "A")]; got.ValidUntil.After(time.Now()) {
		t.Fatal("stale result undid restore", got)
	}
}
func TestLowFrequencySchedulePersistsAndBacksOff(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	r := c.Rules[0]
	if c.IntervalSeconds < 86400 || c.HealthSeconds < 1800 || c.PolicySeconds < c.HealthSeconds*2+600 {
		t.Fatal(c)
	}
	now := time.Now().UTC()
	m.last[r.ID+"/throughput"] = now
	m.last[r.ID+"/health"] = now
	if e := m.store.Set("schedule", m.scheduleLocked()); e != nil {
		t.Fatal(e)
	}
	restarted, e := NewManager(m.store, m.dnsURL, m.token)
	if e != nil {
		t.Fatal(e)
	}
	if restarted.nextTestLocked(r.ID, c).Before(now.Add(23 * time.Hour)) {
		t.Fatal("restart causes repeat tests")
	}
	restarted.deferRetryLocked(r.ID, now)
	first := restarted.retry[r.ID]
	restarted.deferRetryLocked(r.ID, now)
	if first.Sub(now) != 15*time.Minute || restarted.retry[r.ID].Sub(now) != 30*time.Minute {
		t.Fatal("recovery retry storm", restarted.retry)
	}
	if !m.Enqueue(Job{RuleID: r.ID, Kind: "throughput"}) || m.Enqueue(Job{RuleID: r.ID, Kind: "health"}) {
		t.Fatal("same domain tasks not merged")
	}
}
func TestLegacyConfigAndSummaryRetention(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	data, _ := json.Marshal(c)
	var old map[string]any
	json.Unmarshal(data, &old)
	old["probe_bytes"] = 65536
	old["daily_bytes"] = 1
	old["interval_seconds"] = 1800
	old["health_seconds"] = 120
	old["policy_seconds"] = 600
	delete(old, "feed_seconds")
	delete(old, "feed_url")
	old["allowed_cidrs"] = []string{"192.168.0.0/16", "fc00::/7"}
	old["upstreams"] = []string{"[2606:4700:4700::1111]:53"}
	data, _ = json.Marshal(old)
	up, e := upgradeConfig(data)
	if e != nil || up.IntervalSeconds != 86400 || up.HealthSeconds != 1800 || len(up.AllowedCIDRs) != 1 || len(up.Upstreams) != 2 || up.Improvement != 0.05 {
		t.Fatal(up, e)
	}
	if strings.Contains(string(mustJSON(t, up)), "daily_bytes") {
		t.Fatal("legacy budget exported")
	}
	now := time.Now().UTC()
	r := result("1.1.1.1", 20, true)
	r.Time = now
	r.Bytes = 100
	if e = m.store.AddResult(r); e != nil {
		t.Fatal(e)
	}
	r.Speed = 40
	r.Bytes = 200
	m.store.AddResult(r)
	r.OK = false
	r.Bytes = 10
	m.store.AddResult(r)
	r.Kind = "health"
	m.store.AddResult(r)
	page, e := m.store.DailyPage(PageRequest{Page: 1, Size: 20})
	if e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	d := page.Items[0]
	if d.Tests != 3 || d.Failures != 1 || d.HealthChecks != 1 || d.HealthFailures != 1 || d.AverageMbps != 30 || d.PeakMbps != 40 || d.Bytes != 320 {
		t.Fatal(d)
	}
	m.store.db.Exec("DELETE FROM results")
	if page, e = m.store.DailyPage(PageRequest{Page: 1, Size: 20}); e != nil || page.Total != 1 {
		t.Fatal("summary deleted with detail", page, e)
	}
	if e = m.store.pruneAt(now.AddDate(0, 0, 91)); e != nil {
		t.Fatal(e)
	}
	if page, e = m.store.DailyPage(PageRequest{Page: 1, Size: 20}); e != nil || page.Total != 0 {
		t.Fatal("summary age retention", page, e)
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

// A custom RoundTripper is used only in feed tests; production always uses the
// public-address resolver and verified HTTPS transport from newFeedClient.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func feedResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestFeedAtomicUpdateAndLastGood(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	c.FeedURL = "https://rules.example.com/download.json"
	if e := m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	body := `{"version":"v2","rules":[{"platform":"Xbox","url":"http://xvcf1.xboxlive.com/sample","candidates":["1.1.1.1"]}]}`
	m.feedClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return feedResponse(body), nil })}
	if e := m.SyncFeed(context.Background()); e != nil {
		t.Fatal(e)
	}
	good := m.Config()
	if good.RuleVersion != "v2" || len(good.Rules) != 1 || !good.Rules[0].Enabled || good.Rules[0].ID == "" || m.feed.Success.IsZero() {
		t.Fatal(good, m.feed)
	}
	for _, bad := range []string{`{"version":"bad","rules":[]}`, `{}`, body + ` {}`, strings.Repeat("x", (1<<20)+1), strings.ReplaceAll(body, "1.1.1.1", "127.0.0.1")} {
		body = bad
		if e := m.SyncFeed(context.Background()); e == nil {
			t.Fatal("invalid feed accepted")
		}
		if m.Config().RuleVersion != "v2" || m.feed.Success.IsZero() || m.feed.Error == "" {
			t.Fatal("last good lost", m.Config(), m.feed)
		}
	}
	restarted, e := NewManager(m.store, m.dnsURL, m.token)
	if e != nil || restarted.Config().RuleVersion != "v2" || restarted.feed.Error == "" {
		t.Fatal("feed state lost after restart", e)
	}
}
func TestFeedRejectsPrivateRedirectAndStaleUpdate(t *testing.T) {
	for _, u := range []string{"http://rules.example.com/a", "https://127.0.0.1/a", "https://192.168.1.1/a", "https://[::1]/a", "https://user:password@rules.example.com/a", "https://localhost/a"} {
		if validateFeedURL(u) == nil {
			t.Fatal("unsafe feed URL", u)
		}
	}
	client := newFeedClient()
	if e := client.CheckRedirect(&http.Request{}, nil); e == nil {
		t.Fatal("redirect allowed")
	}
	transport := client.Transport.(*http.Transport)
	if conn, e := transport.DialContext(context.Background(), "tcp", "127.0.0.1:443"); e == nil {
		conn.Close()
		t.Fatal("private resolved target dialed")
	}
	m := testManager(t)
	c := m.Config()
	c.FeedURL = "https://rules.example.com/a"
	m.SetConfig(c)
	started := make(chan struct{})
	release := make(chan struct{})
	m.feedClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return feedResponse(`{"version":"stale","rules":[{"platform":"Xbox","url":"http://xvcf1.xboxlive.com/sample"}]}`), nil
	})}
	done := make(chan error, 1)
	go func() { done <- m.SyncFeed(context.Background()) }()
	<-started
	if e := m.SyncFeed(context.Background()); e == nil {
		t.Fatal("duplicate sync not rejected")
	}
	c = m.Config()
	c.FeedURL = "https://rules.example.com/b"
	if e := m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; e == nil || m.Config().RuleVersion == "stale" || m.feed.URL != c.FeedURL {
		t.Fatal("stale feed activated", e, m.Config())
	}
}

func TestRestoreDefaultDoesNotDiscardOtherDomainTasks(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	c.Rules = DefaultConfig().Rules[:2]
	if e := m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	for _, r := range c.Rules {
		if !m.Enqueue(Job{RuleID: r.ID, Kind: "throughput"}) {
			t.Fatal("enqueue", r.ID)
		}
	}
	if e := m.RestoreDefault(c.Rules[0].ID); e != nil {
		t.Fatal(e)
	}
	if !m.pending[c.Rules[1].ID] || m.pending[c.Rules[0].ID] {
		t.Fatal("unrelated task discarded", m.pending)
	}
	if !m.Enqueue(Job{RuleID: c.Rules[0].ID, Kind: "throughput"}) {
		t.Fatal("new manual test blocked by stale task")
	}
}
func TestUnchangedFeedKeepsQueuedWorkAndFailedWriteKeepsLastGood(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	c.FeedURL = "https://rules.example.com/a"
	m.SetConfig(c)
	body := string(mustJSON(t, RuleFeed{Version: c.RuleVersion, Rules: c.Rules}))
	m.feedClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return feedResponse(body), nil })}
	if e := m.SyncFeed(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !m.Enqueue(Job{RuleID: c.Rules[0].ID, Kind: "throughput"}) {
		t.Fatal("enqueue")
	}
	generation := m.generation
	if e := m.SyncFeed(context.Background()); e != nil {
		t.Fatal(e)
	}
	if m.generation != generation || !m.pending[c.Rules[0].ID] {
		t.Fatal("unchanged feed cancelled test")
	}
	if _, e := m.store.db.Exec("CREATE TRIGGER reject_config BEFORE UPDATE ON metadata WHEN NEW.key='config' BEGIN SELECT RAISE(ABORT,'write denied'); END"); e != nil {
		t.Fatal(e)
	}
	body = strings.Replace(body, c.RuleVersion, "new-version", 1)
	if e := m.SyncFeed(context.Background()); e == nil || m.Config().RuleVersion == "new-version" {
		t.Fatal("uncommitted feed activated", e)
	}
}
func TestHealthRunsBetweenCandidates(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	c.Rules = DefaultConfig().Rules[:2]
	if e := m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	health := c.Rules[1]
	m.selections[key(health.Domain, "A")] = Selection{RuleID: health.ID, Domain: health.Domain, Family: "A", IP: "1.1.1.1", ValidUntil: time.Now().Add(time.Hour)}
	hosts := []string{}
	var hostsMu sync.Mutex
	m.prober, _ = probeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		hostsMu.Lock()
		hosts = append(hosts, r.Host)
		hostsMu.Unlock()
		if r.Host == health.Domain {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(bytes.Repeat([]byte("z"), 65536))
		} else {
			w.WriteHeader(403)
		}
	})
	m.resolve = func(context.Context, string, Config) []string { return []string{"8.8.8.8", "9.9.9.9"} }
	if !m.Enqueue(Job{RuleID: health.ID, Kind: "health"}) {
		t.Fatal("health enqueue")
	}
	r := c.Rules[0]
	m.execute(context.Background(), Job{RuleID: r.ID, Kind: "throughput"}, r, c)
	hostsMu.Lock()
	defer hostsMu.Unlock()
	if len(hosts) != 3 || hosts[0] != health.Domain || m.last[health.ID+"/health"].IsZero() {
		t.Fatal("health starved behind throughput", hosts, m.last)
	}
}
func TestLegacyBackupRestoresThroughAPIAndInvalidFeedIsTransactional(t *testing.T) {
	a, h := testAPI(t)
	c := a.m.Config()
	raw := mustJSON(t, c)
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	fields["probe_bytes"] = 65536
	fields["daily_bytes"] = 1
	fields["interval_seconds"] = 1800
	fields["health_seconds"] = 120
	fields["policy_seconds"] = 600
	body := mustJSON(t, map[string]any{"format": 1, "config": fields})
	if res := call(h, "POST", "/api/v1/restore", string(body), "1", ""); res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	if a.m.Config().IntervalSeconds != 86400 {
		t.Fatal("old frequency retained")
	}
	if res := call(h, "POST", "/api/v1/restore", `{"format":1,"config":{}}`, "1", ""); res.Code != 400 {
		t.Fatal("empty backup restored")
	}
	if res := call(h, "PUT", "/api/v1/config", string(mustJSON(t, fields)), "1", ""); res.Code != 400 {
		t.Fatal("retired config accepted on normal write")
	}
	if res := call(h, "POST", "/api/v1/check", `{"domain":"xvcf1.xboxlive.com","type":"AAAA"}`, "1", ""); res.Code != 400 {
		t.Fatal("IPv6 checker exposed")
	}
}

func TestDailyPaginationSnapshotAndCountCap(t *testing.T) {
	a, h := testAPI(t)
	now := time.Now().UTC().Format("2006-01-02")
	seed := func(n int) {
		t.Helper()
		if _, e := a.m.store.db.Exec("WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT OR IGNORE INTO daily(day,domain) SELECT ?, 'download-'||x||'.example' FROM n", n, now); e != nil {
			t.Fatal(e)
		}
	}
	seed(25)
	var first Page[DailySummary]
	resp := call(h, "GET", "/api/v1/history/daily?page_size=20", "", "", "")
	if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &first) != nil || first.Total != 25 {
		t.Fatal(resp.Body.String())
	}
	r := result("1.1.1.1", 20, true)
	r.Time = time.Now().UTC()
	if e := a.m.store.AddResult(r); e != nil {
		t.Fatal(e)
	}
	var second Page[DailySummary]
	url := fmt.Sprintf("/api/v1/history/daily?page=2&page_size=20&snapshot=%d", first.Snapshot)
	resp = call(h, "GET", url, "", "", "")
	if json.Unmarshal(resp.Body.Bytes(), &second) != nil || second.Total != 25 || len(second.Items) != 5 {
		t.Fatal(resp.Body.String())
	}
	seed(10005)
	if e := a.m.store.AddResult(r); e != nil {
		t.Fatal(e)
	}
	page, e := a.m.store.DailyPage(PageRequest{Page: 100000, Size: 100})
	if e != nil || page.Total != 10000 || page.Page != 100 {
		t.Fatal("summary cap", page.Total, page.Page, e)
	}
}

func TestFeedTimeoutKeepsLastGood(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	c.FeedURL = "https://rules.example.com/a"
	if e := m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	m.feedClient = &http.Client{Timeout: 20 * time.Millisecond, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	if e := m.SyncFeed(context.Background()); e == nil || m.feed.Error == "" || m.Config().RuleVersion != c.RuleVersion {
		t.Fatal("timeout replaced last good", e, m.feed)
	}
}

func TestCancelledDiscoveryCannotRescheduleRestoredDefault(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	r := c.Rules[0]
	started := make(chan struct{})
	m.resolve = func(ctx context.Context, _ string, _ Config) []string { close(started); <-ctx.Done(); return nil }
	if !m.Enqueue(Job{RuleID: r.ID, Kind: "throughput"}) {
		t.Fatal("enqueue")
	}
	j := <-m.queue
	done := make(chan struct{})
	go func() { m.runJob(context.Background(), j); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("discovery not started")
	}
	if e := m.RestoreDefault(r.ID); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled discovery did not exit")
	}
	if _, ok := m.retry[r.ID]; ok || m.nextTestLocked(r.ID, c).Before(time.Now().Add(23*time.Hour)) {
		t.Fatal("old failure overwrote default schedule", m.retry)
	}
}
