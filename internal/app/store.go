package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

type Store struct{ db *sql.DB }

func OpenStore(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", filepath.Join(dir, "assistant.db"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	var schema int
	if e = db.QueryRow("PRAGMA user_version").Scan(&schema); e != nil {
		db.Close()
		return nil, e
	}
	if schema > 3 {
		db.Close()
		return nil, fmt.Errorf("数据库版本 %d 高于本程序支持版本", schema)
	}
	s := &Store{db}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;
 CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS results(id INTEGER PRIMARY KEY AUTOINCREMENT,time TEXT NOT NULL,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT,time TEXT NOT NULL,message TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS queries(id INTEGER PRIMARY KEY AUTOINCREMENT,time TEXT NOT NULL,payload TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS results_time ON results(time);
 CREATE INDEX IF NOT EXISTS events_time ON events(time);
 CREATE INDEX IF NOT EXISTS queries_time ON queries(time);
 CREATE TABLE IF NOT EXISTS daily(day TEXT NOT NULL,domain TEXT NOT NULL,tests INTEGER NOT NULL DEFAULT 0,failures INTEGER NOT NULL DEFAULT 0,health_checks INTEGER NOT NULL DEFAULT 0,health_failures INTEGER NOT NULL DEFAULT 0,speed_sum REAL NOT NULL DEFAULT 0,speed_count INTEGER NOT NULL DEFAULT 0,peak_mbps REAL NOT NULL DEFAULT 0,bytes INTEGER NOT NULL DEFAULT 0,switches INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(day,domain));
 DROP TABLE IF EXISTS budgets;
 PRAGMA user_version=3;`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Get(k string, v any) error {
	var b string
	e := s.db.QueryRow("SELECT value FROM metadata WHERE key=?", k).Scan(&b)
	if e != nil {
		return e
	}
	return json.Unmarshal([]byte(b), v)
}
func (s *Store) Set(k string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, string(b))
	return e
}
func (s *Store) AddResult(r Result) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = appendLogTx(tx, "results", "payload", r.Time, string(b), resultLimit); err != nil {
		return err
	}
	if err = addDailyResult(tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) History(limit int) ([]Result, error) {
	rows, e := s.db.Query("SELECT id,payload FROM results ORDER BY id DESC LIMIT ?", limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Result{}
	for rows.Next() {
		var r Result
		var b string
		var id int64
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &r); e != nil {
			return nil, e
		}
		r.ID = id
		out = append(out, r)
	}
	return out, rows.Err()
}

type Event struct {
	ID      int64  `json:"id"`
	Time    string `json:"time"`
	Message string `json:"message"`
}

func (s *Store) Event(msg string) error {
	return s.appendLog("events", "message", time.Now().UTC(), msg, eventLimit)
}
func (s *Store) Prune() error {
	return s.pruneAt(time.Now().UTC())
}

func (s *Store) SaveState(c Config, selections map[string]Selection, version uint64, extra ...map[string]any) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	values := map[string]any{"config": c, "selections": selections, "version": version}
	for _, items := range extra {
		for k, v := range items {
			values[k] = v
		}
	}
	var oldJSON string
	old := map[string]Selection{}
	if err := tx.QueryRow("SELECT value FROM metadata WHERE key='selections'").Scan(&oldJSON); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if oldJSON != "" {
		if err := json.Unmarshal([]byte(oldJSON), &old); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	for k, v := range selections {
		if v.Family == "A" && v.IP != "" && v.ValidUntil.After(now) && (old[k].IP != v.IP || !old[k].ValidUntil.After(now)) {
			if _, err := tx.Exec("INSERT INTO daily(day,domain,switches) VALUES(?,?,1) ON CONFLICT(day,domain) DO UPDATE SET switches=switches+1", now.Format("2006-01-02"), v.Domain); err != nil {
				return err
			}
		}
	}
	for k, v := range values {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, string(b)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
