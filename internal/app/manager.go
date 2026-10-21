package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/miekg/dns"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type Job struct {
	RuleID     string `json:"rule_id"`
	Kind       string `json:"kind"`
	IP         string `json:"ip,omitempty"`
	Generation uint64 `json:"-"`
	Epoch      uint64 `json:"-"`
}
type Task struct {
	RuleID  string    `json:"rule_id"`
	Kind    string    `json:"kind"`
	State   string    `json:"state"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}
type Query struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	Type      string    `json:"type"`
	Client    string    `json:"client"`
	Time      time.Time `json:"time"`
	Optimized bool      `json:"optimized"`
}
type Manager struct {
	mu            sync.RWMutex
	ruleEpoch     map[string]uint64
	runningRule   string
	store         *Store
	config        Config
	selections    map[string]Selection
	version       uint64
	applied       uint64
	lastSync      time.Time
	tasks         map[string]Task
	queue         chan Job
	healthQueue   chan Job
	generation    uint64
	retry         map[string]time.Time
	retryCount    map[string]int
	feed          FeedStatus
	feedMu        sync.Mutex
	feedClient    *http.Client
	pending       map[string]bool
	last          map[string]time.Time
	cancel        context.CancelFunc
	prober        Prober
	resolve       func(context.Context, string, Config) []string
	dnsURL, token string
	client        *http.Client
	notify        chan struct{}
}

func NewManager(s *Store, dnsURL, token string) (*Manager, error) {
	m := &Manager{store: s, selections: map[string]Selection{}, tasks: map[string]Task{}, queue: make(chan Job, 128), pending: map[string]bool{}, last: map[string]time.Time{}, healthQueue: make(chan Job, 128), generation: 1, ruleEpoch: map[string]uint64{}, retry: map[string]time.Time{}, retryCount: map[string]int{}, dnsURL: dnsURL, token: token, client: &http.Client{Timeout: 5 * time.Second}, notify: make(chan struct{}, 1)}
	m.resolve = resolveCandidates
	m.config = DefaultConfig()
	var raw json.RawMessage
	if e := s.Get("config", &raw); e == nil {
		c, err := upgradeConfig(raw)
		if err != nil {
			return nil, err
		}
		m.config = c
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e := s.Get("selections", &m.selections); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e := s.Get("version", &m.version); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if m.version == 0 {
		m.version = uint64(time.Now().UnixMicro())
	}
	for k, v := range m.selections {
		if v.Family != "A" || v.IP != "" && !PublicIP(v.IP) {
			delete(m.selections, k)
		}
	}
	var schedule Schedule
	if e := s.Get("schedule", &schedule); e == nil {
		m.loadSchedule(schedule)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e := s.Get("feed", &m.feed); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e := s.SaveState(m.config, m.selections, m.version); e != nil {
		return nil, e
	}
	return m, nil
}
func (m *Manager) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, _ := json.Marshal(m.config)
	var c Config
	json.Unmarshal(b, &c)
	return c
}
func (m *Manager) SetConfig(c Config) error { return m.setConfig(c, nil, 0) }
func (m *Manager) setConfig(c Config, feed *FeedStatus, expected uint64) error {
	data, _ := json.Marshal(c)
	json.Unmarshal(data, &c)
	if err := c.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if expected != 0 && expected != m.generation {
		return errors.New("设置已变化，请重新同步规则")
	}
	beforeJSON, _ := json.Marshal(m.config)
	if bytes.Equal(data, beforeJSON) {
		if feed != nil {
			if err := m.store.Set("feed", *feed); err != nil {
				return err
			}
			m.feed = *feed
			m.signal()
		}
		return nil
	}
	next := map[string]Selection{}
	schedule := m.scheduleLocked()
	retained := map[string]bool{}
	for _, r := range c.Rules {
		retained[r.ID] = true
		unchanged := false
		for _, before := range m.config.Rules {
			rb, _ := json.Marshal(r)
			bb, _ := json.Marshal(before)
			if bytes.Equal(rb, bb) {
				unchanged = true
			}
		}
		if !unchanged || !m.config.Enabled && c.Enabled {
			delete(schedule.Last, r.ID+"/throughput")
			delete(schedule.Retry, r.ID)
			delete(schedule.Failures, r.ID)
		}
		if c.HealthSeconds != m.config.HealthSeconds {
			delete(schedule.Last, r.ID+"/health")
		}
		sameSample := false
		for _, old := range m.config.Rules {
			if old.ID == r.ID && old.Domain == r.Domain && old.URL == r.URL && old.MinBytes == r.MinBytes && old.ExpectedPrefix == r.ExpectedPrefix {
				sameSample = true
			}
		}
		if sameSample && r.Enabled {
			if s, ok := m.selections[key(r.Domain, "A")]; ok {
				if !c.Enabled && !s.Fixed {
					s.ValidUntil = time.Now()
					s.Reason = "自动选优已暂停，使用默认解析"
				}
				next[key(r.Domain, "A")] = s
			}
		}
	}
	for k := range schedule.Last {
		found := false
		for _, r := range c.Rules {
			if k == r.ID+"/throughput" || k == r.ID+"/health" {
				found = true
			}
		}
		if !found {
			delete(schedule.Last, k)
		}
	}
	for k := range schedule.Retry {
		if !retained[k] {
			delete(schedule.Retry, k)
			delete(schedule.Failures, k)
		}
	}
	feedState := m.feed
	if c.FeedURL != m.config.FeedURL {
		feedState = FeedStatus{URL: c.FeedURL}
	}
	if feed != nil {
		feedState = *feed
	}
	version := m.version + 1
	if err := m.store.SaveState(c, next, version, map[string]any{"schedule": schedule, "feed": feedState}); err != nil {
		return err
	}
	m.resetJobsLocked()
	m.loadSchedule(schedule)
	m.feed = feedState
	m.config = c
	m.selections = next
	m.version = version
	m.signal()
	return nil
}
func (m *Manager) signal() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}
func (m *Manager) Enqueue(j Job) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := j.RuleID
	if m.pending[k] {
		return false
	}
	found := false
	for _, r := range m.config.Rules {
		if r.ID == j.RuleID && r.Enabled {
			found = true
		}
	}
	if !found {
		return false
	}
	if j.Kind != "health" && j.Kind != "throughput" {
		return false
	}
	j.Generation = m.generation
	j.Epoch = m.ruleEpoch[j.RuleID]
	queue := m.queue
	if j.Kind == "health" {
		queue = m.healthQueue
	}
	select {
	case queue <- j:
		m.pending[k] = true
		m.tasks[j.RuleID] = Task{j.RuleID, j.Kind, "queued", "等待自动探测", time.Now()}
		m.signal()
		return true
	default:
		return false
	}
}
func (m *Manager) Observe(q Query) {
	if !DomainValid(q.Domain) || len(q.Client) > 100 {
		return
	}
	if q.Time.IsZero() {
		q.Time = time.Now().UTC()
	}
	if err := m.store.AddQuery(q); err != nil {
		slog.Warn("查询记录写入失败", "error", err)
	}
	m.mu.Lock()
	c := m.config
	var id string
	for _, r := range c.Rules {
		if r.Domain == q.Domain && r.Enabled {
			id = r.ID
		}
	}
	due := id != "" && !m.nextTestLocked(id, c).After(time.Now())
	m.mu.Unlock()
	if due && c.Enabled {
		m.Enqueue(Job{RuleID: id, Kind: "throughput"})
	}
}
func (m *Manager) Run(ctx context.Context) {
	maintenanceDone := make(chan struct{})
	go func() { m.store.maintain(ctx, time.Hour); close(maintenanceDone) }()
	defer func() { <-maintenanceDone }()
	feedDone := make(chan struct{})
	go func() { m.feedLoop(ctx); close(feedDone) }()
	defer func() { <-feedDone }()
	workerDone := make(chan struct{})
	go func() { m.worker(ctx); close(workerDone) }()
	defer func() { <-workerDone }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastPublish time.Time
	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			if m.cancel != nil {
				m.cancel()
			}
			m.mu.Unlock()
			return
		case <-ticker.C:
			c := m.Config()
			now := time.Now()
			if c.Enabled {
				for _, r := range c.Rules {
					if !r.Enabled {
						continue
					}
					m.mu.RLock()
					health := m.last[r.ID+"/health"]
					m.mu.RUnlock()
					if now.Sub(health) >= time.Duration(c.HealthSeconds)*time.Second && m.hasSelection(r.ID) {
						m.Enqueue(Job{RuleID: r.ID, Kind: "health"})
					} else {
						m.mu.RLock()
						due := !m.nextTestLocked(r.ID, c).After(now)
						m.mu.RUnlock()
						if due {
							m.Enqueue(Job{RuleID: r.ID, Kind: "throughput"})
						}
					}

				}
			}
			if now.Sub(lastPublish) > 10*time.Second {
				m.Publish(ctx)
				lastPublish = now
			}
		}
	}
}
func (m *Manager) worker(ctx context.Context) {
	for {
		var j Job
		select {
		case <-ctx.Done():
			return
		case j = <-m.healthQueue:
		default:
			select {
			case <-ctx.Done():
				return
			case j = <-m.healthQueue:
			case j = <-m.queue:
			}
		}
		m.runJob(ctx, j)
	}
}
func (m *Manager) runJob(ctx context.Context, j Job) {
	m.mu.Lock()
	if j.Generation != m.generation || j.Epoch != m.ruleEpoch[j.RuleID] {
		m.mu.Unlock()
		return
	}
	c := m.config
	var rule Rule
	for _, r := range c.Rules {
		if r.ID == j.RuleID {
			rule = r
			break
		}
	}
	jobCtx, cancel := context.WithCancel(ctx)
	previousCancel := m.cancel
	previousRule := m.runningRule
	m.runningRule = j.RuleID
	m.cancel = cancel
	m.tasks[j.RuleID] = Task{j.RuleID, j.Kind, "running", "正在验证下载节点", time.Now()}
	m.mu.Unlock()
	m.signal()
	if rule.Enabled && (c.Enabled || j.IP != "") {
		m.execute(jobCtx, j, rule, c)
	}
	cancel()
	m.mu.Lock()
	m.cancel = previousCancel
	m.runningRule = previousRule
	if j.Generation != m.generation || j.Epoch != m.ruleEpoch[j.RuleID] {
		m.mu.Unlock()
		return
	}
	delete(m.pending, j.RuleID)
	if j.IP == "" || m.tasks[j.RuleID].State != "error" {
		m.last[j.RuleID+"/"+j.Kind] = time.Now().UTC()
	}
	t := m.tasks[j.RuleID]
	if t.State == "running" {
		t.State = "idle"
		t.Message = "本轮检查完成"
		t.Time = time.Now()
		m.tasks[j.RuleID] = t
	}
	schedule := m.scheduleLocked()
	err := m.store.Set("schedule", schedule)
	m.mu.Unlock()
	if err != nil {
		m.taskError(j, err)
	}
	m.signal()
	m.Publish(ctx)
}

func resolveCandidates(ctx context.Context, domain string, c Config) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, addr := range c.Upstreams {
		up, e := upstream.AddressToUpstream(addr, &upstream.Options{Timeout: 2 * time.Second})
		if e != nil {
			continue
		}
		for _, typ := range []uint16{dns.TypeA} {
			req := new(dns.Msg)
			req.SetQuestion(dns.Fqdn(domain), typ)
			select {
			case <-ctx.Done():
				up.Close()
				return out
			default:
			}
			res, e := up.Exchange(ctx, req)
			if e != nil {
				continue
			}
			for _, rr := range res.Answer {
				var ip string
				switch a := rr.(type) {
				case *dns.A:
					ip = a.A.String()
				}
				if PublicIP(ip) && !seen[ip] {
					seen[ip] = true
					out = append(out, ip)
				}
			}
		}
		up.Close()
	}
	return out
}
func (m *Manager) taskError(j Job, err error) {
	m.mu.Lock()
	if j.Generation != 0 && (j.Generation != m.generation || j.Epoch != m.ruleEpoch[j.RuleID]) {
		m.mu.Unlock()
		return
	}
	m.tasks[j.RuleID] = Task{j.RuleID, j.Kind, "error", err.Error(), time.Now()}
	m.mu.Unlock()
	m.store.Event(j.RuleID + ": " + err.Error())
}
func (m *Manager) execute(ctx context.Context, j Job, r Rule, c Config) {
	m.mu.RLock()
	generation := m.generation
	epoch := m.ruleEpoch[j.RuleID]
	existing := []Selection{}
	for _, s := range m.selections {
		if s.RuleID == r.ID {
			existing = append(existing, s)
		}
	}
	m.mu.RUnlock()
	ips := append([]string{}, r.Candidates...)
	if j.IP != "" {
		ips = []string{j.IP}
	} else if j.Kind == "health" {
		ips = nil
		for _, s := range existing {
			if s.IP != "" {
				ips = append(ips, s.IP)
			}
		}
	} else {
		ips = append(ips, m.resolve(ctx, r.Domain, c)...)
	}
	for _, s := range existing {
		if j.Kind == "throughput" && s.IP != "" && j.IP == "" {
			ips = append(ips, s.IP)
		}
	}
	seen := map[string]bool{}
	unique := []string{}
	for _, ip := range ips {
		if !seen[ip] && PublicIP(ip) {
			seen[ip] = true
			unique = append(unique, ip)
		}
	}
	ips = unique
	if ctx.Err() != nil {
		return
	}
	if len(ips) == 0 {
		m.testFailure(j, "未发现公网 IPv4 候选，保持默认解析")
		return
	}
	results := []Result{}
	roundStarted := time.Now()
	for _, ip := range ips {
		if ctx.Err() != nil {
			return
		}
		if j.Kind == "throughput" {
			if time.Since(roundStarted) >= time.Duration(c.HealthSeconds)*time.Second {
				m.mu.RLock()
				current := m.selections[key(r.Domain, "A")]
				lastHealth := m.last[r.ID+"/health"]
				m.mu.RUnlock()
				if current.IP != "" && time.Since(lastHealth) >= time.Duration(c.HealthSeconds)*time.Second {
					m.execute(ctx, Job{RuleID: r.ID, Kind: "health", Generation: j.Generation, Epoch: j.Epoch}, r, c)
					m.Publish(ctx)
					m.mu.RLock()
					valid := m.selections[key(r.Domain, "A")].ValidUntil.After(time.Now())
					m.mu.RUnlock()
					if !valid {
						return
					}
				}
			}
			select {
			case health := <-m.healthQueue:
				m.runJob(ctx, health)
			default:
			}
		}
		m.mu.RLock()
		stale := generation != m.generation || epoch != m.ruleEpoch[j.RuleID]
		m.mu.RUnlock()
		if stale {
			return
		}
		result := m.prober.Probe(ctx, r, ip, c, j.Kind)
		if ctx.Err() != nil {
			return
		}
		if e := m.store.AddResult(result); e != nil {
			m.taskError(j, e)
			return
		}
		results = append(results, result)
	}
	if j.IP != "" {
		verified := false
		for _, v := range results {
			if v.IP == j.IP && v.OK {
				verified = true
			}
		}
		if !verified {
			m.taskError(j, errors.New("固定节点未通过内容验证，保留原策略"))
			return
		}
	}
	if ctx.Err() != nil || len(results) == 0 {
		return
	}
	m.mu.Lock()
	// A configuration edit cancels in-flight work; results from its old rule cannot become active.
	nowCfg, _ := json.Marshal(m.config)
	oldCfg, _ := json.Marshal(c)
	if generation != m.generation || epoch != m.ruleEpoch[j.RuleID] || !bytes.Equal(nowCfg, oldCfg) {
		m.mu.Unlock()
		return
	}
	next := map[string]Selection{}
	for k, s := range m.selections {
		next[k] = s
	}
	now := time.Now().UTC()
	schedule := m.scheduleLocked()
	recover := false
	if j.Kind == "health" {
		for _, v := range results {
			k := key(r.Domain, "A")
			s := next[k]
			if v.IP != s.IP {
				continue
			}
			if v.OK {
				s.CheckedAt = now
				s.ValidUntil = now.Add(time.Duration(c.PolicySeconds) * time.Second)
				s.Reason = "健康检查通过，保持当前 IP"
			} else {
				s.ValidUntil = now
				s.Fixed = false
				s.Reason = "健康检查失败，已恢复默认解析，等待完整复测"
				recover = true
			}
			next[k] = s
		}
	} else {
		k := key(r.Domain, "A")
		old := next[k]
		if j.IP != "" {
			old = Selection{RuleID: r.ID, Domain: r.Domain, Family: "A", IP: j.IP, Fixed: true, ChangedAt: now}
		}
		next[k] = Choose(old, results, c, now)
		if next[k].IP == "" || !next[k].ValidUntil.After(now) {
			deferRetry(&schedule, r.ID, now)
		} else {
			delete(schedule.Retry, r.ID)
			delete(schedule.Failures, r.ID)
		}
		schedule.Last[r.ID+"/health"] = now
	}
	if recover {
		schedule.Retry[r.ID] = now
	}
	schedule.Last[r.ID+"/"+j.Kind] = now
	version := m.version + 1
	if e := m.store.SaveState(c, next, version, map[string]any{"schedule": schedule}); e != nil {
		m.mu.Unlock()
		m.taskError(j, e)
		return
	}
	if j.Kind == "throughput" && (next[key(r.Domain, "A")].IP == "" || !next[key(r.Domain, "A")].ValidUntil.After(now)) {
		m.tasks[r.ID] = Task{r.ID, j.Kind, "error", "本轮没有合格节点，使用默认解析并等待退避复测", now}
	}
	m.loadSchedule(schedule)
	m.selections = next
	m.version = version
	m.mu.Unlock()
	if recover {
		m.Publish(ctx)
	}
	for _, s := range next {
		if s.RuleID == r.ID {
			m.store.Event(fmt.Sprintf("%s %s：%s", s.Domain, s.Family, s.Reason))
		}
	}
}
func (m *Manager) Publish(ctx context.Context) {
	m.mu.RLock()
	p := Policy{Version: m.version, CreatedAt: time.Now().UTC(), Config: m.config, Selections: []Selection{}}
	for _, s := range m.selections {
		p.Selections = append(p.Selections, s)
	}
	m.mu.RUnlock()
	normalizePolicy(&p)
	b, e := json.Marshal(p)
	if e != nil {
		return
	}
	req, e := http.NewRequestWithContext(ctx, "PUT", m.dnsURL+"/internal/policy", bytes.NewReader(b))
	if e != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	resp, e := m.client.Do(req)
	if e != nil {
		slog.Debug("策略同步等待 DNS", "error", e)
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	resp.Body.Close()
	if resp.StatusCode == 200 {
		m.mu.Lock()
		changed := m.applied < p.Version
		m.applied = max(m.applied, p.Version)
		m.lastSync = time.Now()
		m.mu.Unlock()
		if changed {
			m.signal()
		}
	}
}
func (m *Manager) RestoreDefault(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for _, r := range m.config.Rules {
		if r.ID == id {
			found = true
		}
	}
	if !found {
		return errors.New("规则不存在")
	}
	next := map[string]Selection{}
	for k, s := range m.selections {
		if s.RuleID == id {
			s.Fixed = false
			s.ValidUntil = time.Now()
			s.Reason = "已手动恢复默认解析"
		}
		next[k] = s
	}
	schedule := m.scheduleLocked()
	schedule.Last[id+"/throughput"] = time.Now().UTC()
	delete(schedule.Retry, id)
	delete(schedule.Failures, id)
	version := m.version + 1
	if err := m.store.SaveState(m.config, next, version, map[string]any{"schedule": schedule}); err != nil {
		return err
	}
	m.ruleEpoch[id]++
	if m.runningRule == id && m.cancel != nil {
		m.cancel()
	}
	delete(m.pending, id)
	m.tasks[id] = Task{id, "throughput", "idle", "已恢复默认解析，等待计划或手动复测", time.Now()}
	m.loadSchedule(schedule)
	m.selections = next
	m.version = version
	m.signal()
	return nil
}

func (m *Manager) hasSelection(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.selections {
		if s.RuleID == id && s.IP != "" {
			return true
		}
	}
	return false
}
