package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

const (
	queryLimit      = 10000
	connectionLimit = 10000
	resultLimit     = 5000
	eventLimit      = 500
)

type PageRequest struct {
	Page     int
	Size     int
	Snapshot int64
}

type Page[T any] struct {
	Items         []T   `json:"items"`
	Page          int   `json:"page"`
	PageSize      int   `json:"page_size"`
	Total         int   `json:"total"`
	Pages         int   `json:"pages"`
	Snapshot      int64 `json:"snapshot"`
	RetentionDays int   `json:"retention_days"`
	MaxRecords    int   `json:"max_records"`
}

// Only internal table/column names enter these statements. IDs also bound storage
// between scheduled age cleanups, even under sustained query traffic.
func (s *Store) appendLog(table, column string, at time.Time, value string, limit int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = appendLogTx(tx, table, column, at, value, limit); err != nil {
		return err
	}
	return tx.Commit()
}
func appendLogTx(tx *sql.Tx, table, column string, at time.Time, value string, limit int) error {
	r, err := tx.Exec("INSERT INTO "+table+"(time,"+column+") VALUES(?,?)", at.UTC().Format(time.RFC3339Nano), value)
	if err != nil {
		return err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM "+table+" WHERE id<=?", id-int64(limit))
	return err
}

func (s *Store) AddQuery(q Query) error {
	b, err := json.Marshal(q)
	if err != nil {
		return err
	}
	return s.appendLog("queries", "payload", q.Time, string(b), queryLimit)
}

type Connection struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Client  string    `json:"client"`
	Method  string    `json:"method"`
	Domain  string    `json:"domain"`
	Address string    `json:"address"`
}

func (s *Store) AddConnection(c Connection) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.appendLog("connections", "payload", c.Time, string(b), connectionLimit)
}

func (s *Store) ConnectionPage(req PageRequest) (Page[Connection], error) {
	return readPage(s, "connections", "payload", "", req, 7, connectionLimit, func(id int64, _, value string) (Connection, error) {
		var c Connection
		err := json.Unmarshal([]byte(value), &c)
		c.ID = id
		return c, err
	})
}

func readPage[T any](s *Store, table, column, filter string, req PageRequest, days, limit int, decode func(int64, string, string) (T, error)) (Page[T], error) {
	p := Page[T]{Items: []T{}, PageSize: req.Size, RetentionDays: days, MaxRecords: limit}
	if req.Page < 1 || req.Size < 1 || req.Size > 100 || req.Snapshot < 0 {
		return p, errors.New("分页参数无效")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	p.Snapshot = req.Snapshot
	if p.Snapshot == 0 {
		if err = tx.QueryRow("SELECT COALESCE(MAX(id),0) FROM " + table).Scan(&p.Snapshot); err != nil {
			return p, err
		}
	}
	// The snapshot excludes new inserts while browsing, while retention deletions
	// remain visible. Count and rows come from the same SQLite read transaction.
	where := " WHERE id<=?" + filter
	if err = tx.QueryRow("SELECT COUNT(*) FROM "+table+where, p.Snapshot).Scan(&p.Total); err != nil {
		return p, err
	}
	p.Pages = max(1, (p.Total+req.Size-1)/req.Size)
	p.Page = min(req.Page, p.Pages)
	rows, err := tx.Query("SELECT id,time,"+column+" FROM "+table+where+" ORDER BY id DESC LIMIT ? OFFSET ?", p.Snapshot, req.Size, (p.Page-1)*req.Size)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var at, value string
		if err = rows.Scan(&id, &at, &value); err != nil {
			return p, err
		}
		item, err := decode(id, at, value)
		if err != nil {
			return p, err
		}
		p.Items = append(p.Items, item)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	if err = rows.Close(); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

func (s *Store) QueryPage(req PageRequest) (Page[Query], error) {
	return readPage(s, "queries", "payload", "", req, 7, queryLimit, func(id int64, _, value string) (Query, error) {
		var q Query
		err := json.Unmarshal([]byte(value), &q)
		q.ID = id
		return q, err
	})
}

func (s *Store) ResultPage(req PageRequest, filter string) (Page[Result], error) {
	where := ""
	switch filter {
	case "", "all":
	case "throughput", "health":
		where = " AND json_extract(payload,'$.kind')='" + filter + "'"
	case "failed":
		where = " AND json_extract(payload,'$.ok')=0"
	default:
		return Page[Result]{}, errors.New("记录筛选无效")
	}
	return readPage(s, "results", "payload", where, req, 30, resultLimit, func(id int64, _, value string) (Result, error) {
		var r Result
		err := json.Unmarshal([]byte(value), &r)
		r.ID = id
		return r, err
	})
}

func (s *Store) EventPage(req PageRequest) (Page[Event], error) {
	return readPage(s, "events", "message", "", req, 30, eventLimit, func(id int64, at, value string) (Event, error) {
		return Event{ID: id, Time: at, Message: value}, nil
	})
}

func (s *Store) pruneAt(now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, log := range []struct {
		table       string
		days, limit int
	}{{"connections", 7, connectionLimit}, {"queries", 7, queryLimit}, {"results", 30, resultLimit}, {"events", 30, eventLimit}} {
		if _, err = tx.Exec("DELETE FROM "+log.table+" WHERE julianday(time)<julianday(?) OR id NOT IN (SELECT id FROM "+log.table+" ORDER BY id DESC LIMIT ?)", now.AddDate(0, 0, -log.days).Format(time.RFC3339Nano), log.limit); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM daily WHERE day<? OR rowid NOT IN (SELECT rowid FROM daily ORDER BY day DESC,domain LIMIT 10000)", now.AddDate(0, 0, -89).Format("2006-01-02")); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Freed database pages are reused; truncate the WAL when readers permit it.
	_, err = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (s *Store) maintain(ctx context.Context, interval time.Duration) {
	cleanup := func() {
		if err := s.Prune(); err != nil && ctx.Err() == nil {
			slog.Warn("记录清理失败，将在下次重试", "error", err)
		}
	}
	cleanup()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
