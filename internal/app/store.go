package app

import (
	"context"
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
	if schema > 1 {
		db.Close()
		return nil, fmt.Errorf("数据库版本 %d 高于本程序支持版本", schema)
	}
	s := &Store{db}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;
 CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS results(id INTEGER PRIMARY KEY AUTOINCREMENT,time TEXT NOT NULL,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS budgets(day TEXT PRIMARY KEY,used INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT,time TEXT NOT NULL,message TEXT NOT NULL);
 PRAGMA user_version=1;`)
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
	_, e = s.db.Exec("INSERT INTO results(time,payload) VALUES(?,?)", r.Time.Format(time.RFC3339Nano), string(b))
	return e
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
	Time    string `json:"time"`
	Message string `json:"message"`
}

func (s *Store) Event(msg string) error {
	_, e := s.db.Exec("INSERT INTO events(time,message) VALUES(?,?)", time.Now().UTC().Format(time.RFC3339), msg)
	return e
}
func (s *Store) Events() ([]Event, error) {
	rows, e := s.db.Query("SELECT time,message FROM events ORDER BY id DESC LIMIT 100")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.Time, &v.Message); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Used(day string) (int64, error) {
	var n int64
	e := s.db.QueryRow("SELECT used FROM budgets WHERE day=?", day).Scan(&n)
	if errors.Is(e, sql.ErrNoRows) {
		e = nil
	}
	return n, e
}

// Reservations survive a crash; unconsumed bytes are released only on a clean settlement.
func (s *Store) Reserve(day string, n, limit int64) (bool, error) {
	tx, e := s.db.BeginTx(context.Background(), nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT OR IGNORE INTO budgets(day,used) VALUES(?,0)", day); e != nil {
		return false, e
	}
	r, e := tx.Exec("UPDATE budgets SET used=used+? WHERE day=? AND used+?<=?", n, day, n, limit)
	if e != nil {
		return false, e
	}
	rows, e := r.RowsAffected()
	if e != nil {
		return false, e
	}
	if rows == 0 {
		return false, nil
	}
	return true, tx.Commit()
}
func (s *Store) Settle(day string, reserved, actual int64) error {
	if actual > reserved {
		actual = reserved
	}
	_, e := s.db.Exec("UPDATE budgets SET used=MAX(0,used-?) WHERE day=?", reserved-actual, day)
	return e
}
func (s *Store) Prune() error {
	_, e := s.db.Exec("DELETE FROM results WHERE id NOT IN (SELECT id FROM results ORDER BY id DESC LIMIT 5000); DELETE FROM events WHERE id NOT IN (SELECT id FROM events ORDER BY id DESC LIMIT 500); DELETE FROM budgets WHERE day < date('now','-90 days')")
	return e
}

func (s *Store) SaveState(c Config, selections map[string]Selection, version uint64) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for k, v := range map[string]any{"config": c, "selections": selections, "version": version} {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, string(b)); e != nil {
			return e
		}
	}
	return tx.Commit()
}
