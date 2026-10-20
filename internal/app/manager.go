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
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Job struct {
	RuleID string `json:"rule_id"`
	Kind   string `json:"kind"`
	IP     string `json:"ip,omitempty"`
}
type Task struct {
	RuleID  string    `json:"rule_id"`
	Kind    string    `json:"kind"`
	State   string    `json:"state"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}
type Query struct {
	Domain    string    `json:"domain"`
	Type      string    `json:"type"`
	Client    string    `json:"client"`
	Time      time.Time `json:"time"`
	Optimized bool      `json:"optimized"`
}
type Manager struct {
	mu            sync.RWMutex
	store         *Store
	config        Config
	selections    map[string]Selection
	version       uint64
	applied       uint64
	lastSync      time.Time
	tasks         map[string]Task
	queries       []Query
	queue         chan Job
	pending       map[string]bool
	last          map[string]time.Time
	failures      map[string]int
	cancel        context.CancelFunc
	prober        Prober
	resolve       func(context.Context, string, Config) []string
	dnsURL, token string
	client        *http.Client
	notify        chan struct{}
}

func NewManager(s *Store, dnsURL, token string) (*Manager, error) {
	m := &Manager{store: s, selections: map[string]Selection{}, tasks: map[string]Task{}, queue: make(chan Job, 128), pending: map[string]bool{}, last: map[string]time.Time{}, failures: map[string]int{}, dnsURL: dnsURL, token: token, client: &http.Client{Timeout: 5 * time.Second}, notify: make(chan struct{}, 1)}
	m.resolve = resolveCandidates
	m.config = DefaultConfig()
	if e := s.Get("config", &m.config); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e := m.config.Validate(); e != nil {
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
func (m *Manager) SetConfig(c Config) error {
	if e := c.Validate(); e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := map[string]Selection{}
	for k, s := range m.selections {
		keep := false
		for _, r := range c.Rules {
			for _, before := range m.config.Rules {
				if r.ID == s.RuleID && r.Enabled && before.ID == r.ID {
					rb, _ := json.Marshal(r)
					bb, _ := json.Marshal(before)
					keep = bytes.Equal(rb, bb)
				}
			}
		}
		if keep {
			if !c.Enabled && !s.Fixed {
				s.ValidUntil = time.Now()
				s.Reason = "自动选优已暂停"
			}
			next[k] = s
		}
	}
	version := m.version + 1
	if e := m.store.SaveState(c, next, version); e != nil {
		return e
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.config = c
	m.last = map[string]time.Time{}
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
	k := j.RuleID + "/" + j.Kind
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
	select {
	case m.queue <- j:
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
	q.Time = time.Now().UTC()
	m.mu.Lock()
	m.queries = append([]Query{q}, m.queries...)
	if len(m.queries) > 100 {
		m.queries = m.queries[:100]
	}
	c := m.config
	var id string
	for _, r := range c.Rules {
		if r.Domain == q.Domain && r.Enabled {
			id = r.ID
		}
	}
	due := id != "" && time.Since(m.last[id+"/throughput"]) > time.Duration(c.IntervalSeconds)*time.Second
	m.mu.Unlock()
	if due && c.Enabled {
		m.Enqueue(Job{RuleID: id, Kind: "throughput"})
	}
}
func (m *Manager) Run(ctx context.Context) {
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
					last := m.last[r.ID+"/throughput"]
					health := m.last[r.ID+"/health"]
					m.mu.RUnlock()
					if now.Sub(last) >= time.Duration(c.IntervalSeconds)*time.Second {
						m.Enqueue(Job{RuleID: r.ID, Kind: "throughput"})
					} else if now.Sub(health) >= time.Duration(c.HealthSeconds)*time.Second && m.hasSelection(r.ID) {
						m.Enqueue(Job{RuleID: r.ID, Kind: "health"})
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
		select {
		case <-ctx.Done():
			return
		case j := <-m.queue:
			m.mu.Lock()
			c := m.config
			var rule Rule
			for _, r := range c.Rules {
				if r.ID == j.RuleID {
					rule = r
				}
			}
			jobCtx, cancel := context.WithCancel(ctx)
			m.cancel = cancel
			m.tasks[j.RuleID] = Task{j.RuleID, j.Kind, "running", "正在验证下载节点", time.Now()}
			m.mu.Unlock()
			m.signal()
			if rule.Enabled && (c.Enabled || j.IP != "") {
				m.execute(jobCtx, j, rule, c)
			}
			cancel()
			m.mu.Lock()
			m.cancel = nil
			delete(m.pending, j.RuleID+"/"+j.Kind)
			m.last[j.RuleID+"/"+j.Kind] = time.Now()
			if t := m.tasks[j.RuleID]; t.State == "running" {
				t.State = "idle"
				t.Message = "本轮检查完成"
				t.Time = time.Now()
				m.tasks[j.RuleID] = t
			}
			m.mu.Unlock()
			m.signal()
			m.Publish(ctx)
		}
	}
}
func resolveCandidates(ctx context.Context, domain string, c Config) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, addr := range c.Upstreams {
		up, e := upstream.AddressToUpstream(addr, &upstream.Options{Timeout: 2 * time.Second})
		if e != nil {
			continue
		}
		for _, typ := range []uint16{dns.TypeA, dns.TypeAAAA} {
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
				case *dns.AAAA:
					ip = a.AAAA.String()
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
	m.tasks[j.RuleID] = Task{j.RuleID, j.Kind, "error", err.Error(), time.Now()}
	m.mu.Unlock()
	m.store.Event(j.RuleID + ": " + err.Error())
}
func (m *Manager) execute(ctx context.Context, j Job, r Rule, c Config) {
	m.mu.RLock()
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
			if s.Backup != "" {
				ips = append(ips, s.Backup)
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
	if len(ips) > 8 {
		ips = ips[:8]
	}
	if len(ips) == 0 {
		m.taskError(j, errors.New("未发现可用候选；保留普通解析"))
		return
	}
	if j.Kind != "health" && len(ips) > 3 {
		type timed struct {
			ip string
			d  time.Duration
		}
		rank := []timed{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, ip := range ips {
			wg.Add(1)
			go func(ip string) {
				defer wg.Done()
				port := "80"
				if strings.HasPrefix(r.URL, "https:") {
					port = "443"
				}
				start := time.Now()
				conn, e := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip, port))
				if e != nil {
					return
				}
				conn.Close()
				mu.Lock()
				rank = append(rank, timed{ip, time.Since(start)})
				mu.Unlock()
			}(ip)
		}
		wg.Wait()
		sort.Slice(rank, func(i, j int) bool { return rank[i].d < rank[j].d })
		ips = nil
		for _, x := range rank[:min(3, len(rank))] {
			ips = append(ips, x.ip)
		}
		// Keep the incumbent in the comparison even when another candidate connects sooner.
		for _, s := range existing {
			if s.IP != "" && !contains(ips, s.IP) {
				ips = append(ips, s.IP)
			}
		}
	}
	loc, _ := time.LoadLocation(c.Timezone)
	day := time.Now().In(loc).Format("2006-01-02")
	results := []Result{}
	for _, ip := range ips {
		if ctx.Err() != nil {
			return
		}
		amount := c.ProbeBytes
		budget := c.DailyBytes * 9 / 10
		if j.Kind == "health" {
			amount = 65536
			budget = c.DailyBytes
		}
		ok, e := m.store.Reserve(day, amount, budget)
		if e != nil {
			m.taskError(j, e)
			return
		}
		if !ok {
			m.taskError(j, errors.New("今日测速预算不足，延后本轮探测"))
			break
		}
		result := m.prober.Probe(ctx, r, ip, c, j.Kind)
		if e = m.store.Settle(day, amount, result.Bytes); e != nil {
			m.taskError(j, e)
			return
		}
		if e = m.store.AddResult(result); e != nil {
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
	if j.Kind == "throughput" {
		history, e := m.store.History(1000)
		if e != nil {
			m.taskError(j, e)
			return
		}
		results = rankedHistory(results, history, time.Now())
	}
	m.mu.Lock()
	// A configuration edit cancels in-flight work; results from its old rule cannot become active.
	nowCfg, _ := json.Marshal(m.config)
	oldCfg, _ := json.Marshal(c)
	if !bytes.Equal(nowCfg, oldCfg) {
		m.mu.Unlock()
		return
	}
	next := map[string]Selection{}
	for k, s := range m.selections {
		next[k] = s
	}
	now := time.Now().UTC()
	if j.Kind == "health" {
		passed := map[string]bool{}
		tested := map[string]bool{}
		for _, v := range results {
			passed[v.IP] = v.OK
			tested[v.IP] = true
		}
		for _, s := range existing {
			k := key(s.Domain, s.Family)
			if !tested[s.IP] {
				continue
			}
			if passed[s.IP] {
				s.ValidUntil = now.Add(time.Duration(c.PolicySeconds) * time.Second)
				m.failures[k] = 0
			} else {
				m.failures[k]++
				if m.failures[k] >= 2 {
					if !s.Fixed && s.Backup != "" && passed[s.Backup] {
						s.IP = s.Backup
						s.Backup = ""
						s.Speed = 0
						s.ChangedAt = now
						s.ValidUntil = now.Add(time.Duration(c.PolicySeconds) * time.Second)
						s.Reason = "原节点失效，备用内容检查通过"
						m.last[r.ID+"/throughput"] = time.Time{}
					} else {
						s.ValidUntil = now
						s.Reason = "节点健康检查失败，恢复上游解析"
					}
				}
			}
			next[k] = s
		}
	} else {
		for _, family := range []string{"A", "AAAA"} {
			rr := []Result{}
			for _, v := range results {
				if v.Family == family {
					rr = append(rr, v)
				}
			}
			if len(rr) == 0 {
				continue
			}
			k := key(r.Domain, family)
			old := next[k]
			if j.IP != "" {
				old = Selection{RuleID: r.ID, Domain: r.Domain, Family: family, IP: j.IP, Fixed: true, ChangedAt: now}
			}
			next[k] = Choose(old, rr, c, now)
		}
	}
	version := m.version + 1
	if e := m.store.SaveState(c, next, version); e != nil {
		m.mu.Unlock()
		m.taskError(j, e)
		return
	}
	m.selections = next
	m.version = version
	m.mu.Unlock()
	for _, s := range next {
		if s.RuleID == r.ID {
			m.store.Event(fmt.Sprintf("%s %s：%s", s.Domain, s.Family, s.Reason))
		}
	}
	m.store.Prune()
}
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
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
		m.applied = p.Version
		m.lastSync = time.Now()
		m.mu.Unlock()
	}
}
func (m *Manager) Unfix(id string) error {
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
			s.Reason = "已恢复自动选优"
		}
		next[k] = s
	}
	version := m.version + 1
	if e := m.store.SaveState(m.config, next, version); e != nil {
		return e
	}
	m.selections = next
	m.version = version
	return nil
}

func rankedHistory(current []Result, history []Result, now time.Time) []Result {
	out := append([]Result{}, current...)
	for i, r := range out {
		if !r.OK || r.Kind != "throughput" {
			continue
		}
		recent := []float64{}
		total := 0
		for _, h := range history {
			if h.Domain != r.Domain || h.IP != r.IP || h.Family != r.Family || h.Profile != r.Profile || h.Kind != "throughput" || now.Sub(h.Time) > 2*time.Hour {
				continue
			}
			total++
			if h.OK {
				recent = append(recent, h.Speed)
			}
			if total >= 3 {
				break
			}
		}
		if total > 0 && len(recent)*3 < total*2 {
			out[i].OK = false
			out[i].Error = "近期成功率不足"
			continue
		}
		if len(recent) > 0 {
			sort.Float64s(recent)
			out[i].Speed = recent[len(recent)/2]
		}
	}
	return out
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
