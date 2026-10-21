package app

import (
	"time"
)

type Schedule struct {
	Last     map[string]time.Time `json:"last"`
	Retry    map[string]time.Time `json:"retry"`
	Failures map[string]int       `json:"failures"`
}

func (m *Manager) scheduleLocked() Schedule {
	s := Schedule{Last: map[string]time.Time{}, Retry: map[string]time.Time{}, Failures: map[string]int{}}
	for k, v := range m.last {
		s.Last[k] = v
	}
	for k, v := range m.retry {
		s.Retry[k] = v
	}
	for k, v := range m.retryCount {
		s.Failures[k] = v
	}
	return s
}
func (m *Manager) loadSchedule(s Schedule) {
	if s.Last != nil {
		m.last = s.Last
	}
	if s.Retry != nil {
		m.retry = s.Retry
	}
	if s.Failures != nil {
		m.retryCount = s.Failures
	}
}
func (m *Manager) nextTestLocked(id string, c Config) time.Time {
	if retry, ok := m.retry[id]; ok {
		return retry
	}
	return m.last[id+"/throughput"].Add(time.Duration(c.IntervalSeconds) * time.Second)
}
func deferRetry(s *Schedule, id string, now time.Time) {
	s.Failures[id] = min(s.Failures[id]+1, 5)
	s.Retry[id] = now.Add(time.Duration(1<<uint(s.Failures[id]-1)) * 15 * time.Minute)
}
func (m *Manager) deferRetryLocked(id string, now time.Time) {
	s := m.scheduleLocked()
	deferRetry(&s, id, now)
	m.loadSchedule(s)
}
func (m *Manager) testFailure(j Job, reason string) {
	m.mu.Lock()
	if j.Generation != 0 && (j.Generation != m.generation || j.Epoch != m.ruleEpoch[j.RuleID]) {
		m.mu.Unlock()
		return
	}
	if j.Kind == "throughput" {
		m.deferRetryLocked(j.RuleID, time.Now())
	}
	m.mu.Unlock()
	m.taskError(j, errMessage(reason))
}

type errMessage string

func (e errMessage) Error() string { return string(e) }
func (m *Manager) resetJobsLocked() {
	m.generation++
	m.ruleEpoch = map[string]uint64{}
	if m.cancel != nil {
		m.cancel()
	}
	for {
		select {
		case <-m.queue:
		default:
			goto health
		}
	}
health:
	for {
		select {
		case <-m.healthQueue:
		default:
			m.pending = map[string]bool{}
			m.tasks = map[string]Task{}
			return
		}
	}
}
