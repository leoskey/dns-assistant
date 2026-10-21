package app

import "database/sql"

type DailySummary struct {
	Day            string  `json:"day"`
	Domain         string  `json:"domain"`
	Tests          int     `json:"tests"`
	Failures       int     `json:"failures"`
	HealthChecks   int     `json:"health_checks"`
	HealthFailures int     `json:"health_failures"`
	AverageMbps    float64 `json:"average_mbps"`
	PeakMbps       float64 `json:"peak_mbps"`
	Bytes          int64   `json:"bytes"`
	Switches       int     `json:"switches"`
}

func addDailyResult(tx *sql.Tx, r Result) error {
	tests, failures, checks, healthFailures, count := 0, 0, 0, 0, 0
	speed := 0.0
	if r.Kind == "health" {
		checks = 1
		if !r.OK {
			healthFailures = 1
		}
	} else {
		tests = 1
		if !r.OK {
			failures = 1
		} else {
			count = 1
			speed = r.Speed
		}
	}
	_, err := tx.Exec(`INSERT INTO daily(day,domain,tests,failures,health_checks,health_failures,speed_sum,speed_count,peak_mbps,bytes) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(day,domain) DO UPDATE SET tests=tests+excluded.tests,failures=failures+excluded.failures,health_checks=health_checks+excluded.health_checks,health_failures=health_failures+excluded.health_failures,speed_sum=speed_sum+excluded.speed_sum,speed_count=speed_count+excluded.speed_count,peak_mbps=MAX(peak_mbps,excluded.peak_mbps),bytes=bytes+excluded.bytes`, r.Time.UTC().Format("2006-01-02"), r.Domain, tests, failures, checks, healthFailures, speed, count, speed, r.Bytes)
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM daily WHERE rowid NOT IN (SELECT rowid FROM daily ORDER BY day DESC,domain LIMIT 10000)")
	return err
}
func (s *Store) DailyPage(req PageRequest) (Page[DailySummary], error) {
	p := Page[DailySummary]{Items: []DailySummary{}, RetentionDays: 90, MaxRecords: 10000, PageSize: req.Size}
	if req.Page < 1 || req.Size < 1 || req.Size > 100 || req.Snapshot < 0 {
		return p, errMessage("分页参数无效")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	p.Snapshot = req.Snapshot
	if p.Snapshot == 0 {
		if err = tx.QueryRow("SELECT COALESCE(MAX(rowid),0) FROM daily").Scan(&p.Snapshot); err != nil {
			return p, err
		}
	}
	if err = tx.QueryRow("SELECT COUNT(*) FROM daily WHERE rowid<=?", p.Snapshot).Scan(&p.Total); err != nil {
		return p, err
	}
	p.Pages = max(1, (p.Total+req.Size-1)/req.Size)
	p.Page = min(req.Page, p.Pages)
	rows, err := tx.Query("SELECT day,domain,tests,failures,health_checks,health_failures,COALESCE(speed_sum/NULLIF(speed_count,0),0),peak_mbps,bytes,switches FROM daily WHERE rowid<=? ORDER BY day DESC,domain LIMIT ? OFFSET ?", p.Snapshot, req.Size, (p.Page-1)*req.Size)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var d DailySummary
		if err = rows.Scan(&d.Day, &d.Domain, &d.Tests, &d.Failures, &d.HealthChecks, &d.HealthFailures, &d.AverageMbps, &d.PeakMbps, &d.Bytes, &d.Switches); err != nil {
			rows.Close()
			return p, err
		}
		p.Items = append(p.Items, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}
