package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/miekg/dns"
	"net"

	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type DNS struct {
	mu            sync.Mutex
	policy        atomic.Pointer[Policy]
	proxy         *proxy.Proxy
	addr, dir     string
	observer      func(context.Context, Query)
	observeCancel context.CancelFunc
	observeDone   chan struct{}
	observed      chan Query
	rateMu        sync.Mutex
	counts        map[string]int
	minute        int64
	testMode      bool
}

func NewDNS(addr, dir string, observer func(context.Context, Query)) *DNS {
	d := &DNS{addr: addr, dir: dir, observer: observer, observed: make(chan Query, 128), counts: map[string]int{}}
	d.policy.Store(&Policy{Config: DefaultConfig(), Selections: []Selection{}})
	return d
}
func (d *DNS) Start(ctx context.Context) error {
	if e := os.MkdirAll(d.dir, 0700); e != nil {
		return e
	}
	var p Policy
	if b, e := os.ReadFile(filepath.Join(d.dir, "policy.json")); e == nil {
		var saved struct {
			Config json.RawMessage `json:"config"`
		}
		json.Unmarshal(b, &saved)
		config, configErr := upgradeConfig(saved.Config)
		decodeErr := json.Unmarshal(b, &p)
		if d.testMode && decodeErr == nil {
			config = p.Config
			configErr = nil
		}
		if decodeErr == nil && configErr == nil {
			p.Config = config
			filtered := []Selection{}
			for _, v := range p.Selections {
				if v.Family == "A" && (v.IP == "" || PublicIP(v.IP)) {
					filtered = append(filtered, v)
				}
			}
			p.Selections = filtered
		}
		if decodeErr == nil && configErr == nil && d.validatePolicy(p) == nil {
			normalizePolicy(&p)
			d.policy.Store(&p)
		}
	}
	engine, e := d.makeProxy(d.policy.Load().Config)
	if e != nil {
		return e
	}
	if e = engine.Start(ctx); e != nil {
		return e
	}
	d.proxy = engine
	observeCtx, cancel := context.WithCancel(ctx)
	d.observeCancel = cancel
	d.observeDone = make(chan struct{})
	go func() { defer close(d.observeDone); d.observeLoop(observeCtx) }()
	return nil
}
func (d *DNS) makeProxy(c Config) (*proxy.Proxy, error) {
	up, e := proxy.ParseUpstreamsConfig(c.Upstreams, &upstream.Options{Timeout: 2 * time.Second})
	if e != nil {
		return nil, e
	}
	ua, e := net.ResolveUDPAddr("udp4", d.addr)
	if e != nil {
		return nil, e
	}
	ta, e := net.ResolveTCPAddr("tcp4", d.addr)
	if e != nil {
		return nil, e
	}
	if ua.IP == nil {
		ua.IP = net.IPv4zero
	}
	if ta.IP == nil {
		ta.IP = net.IPv4zero
	}
	return proxy.New(&proxy.Config{UDPListenAddr: []*net.UDPAddr{ua}, TCPListenAddr: []*net.TCPAddr{ta}, UpstreamConfig: up, UpstreamMode: proxy.UpstreamModeParallel, CacheEnabled: true, CacheSizeBytes: 16 << 20, MaxGoroutines: 128, RefuseAny: true, RequestHandler: proxy.HandlerFunc(d.handle)})
}
func (d *DNS) Close(ctx context.Context) error {
	d.mu.Lock()
	if d.observeCancel != nil {
		d.observeCancel()
	}
	engine, done := d.proxy, d.observeDone
	d.proxy = nil
	d.mu.Unlock()
	var err error
	if engine != nil {
		err = engine.Shutdown(ctx)
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			if err == nil {
				err = ctx.Err()
			}
		}
	}
	return err
}
func (d *DNS) handle(ctx context.Context, p *proxy.Proxy, q *proxy.DNSContext) error {
	state := d.policy.Load()
	name := strings.TrimSuffix(strings.ToLower(q.Req.Question[0].Name), ".")
	family := dns.TypeToString[q.Req.Question[0].Qtype]
	refuse := func() { q.Res = new(dns.Msg); q.Res.SetRcode(q.Req, dns.RcodeRefused) }
	if !AllowSource(q.Addr.String(), state.Config) {
		refuse()
		return nil
	}
	now := time.Now()
	d.rateMu.Lock()
	minute := now.Unix() / 60
	if minute != d.minute {
		d.counts = map[string]int{}
		d.minute = minute
	}
	source := q.Addr.Addr().String()
	if len(d.counts) > 2048 {
		d.rateMu.Unlock()
		refuse()
		return nil
	}
	d.counts[source]++
	limited := d.counts[source] > 3000
	d.rateMu.Unlock()
	if limited {
		refuse()
		return nil
	}
	if name == "health.dns-assistant.invalid" {
		q.Res = new(dns.Msg)
		q.Res.SetReply(q.Req)
		q.Res.RecursionAvailable = true
		if q.Req.Question[0].Qtype == dns.TypeA {
			q.Res.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0}, A: net.IPv4(127, 0, 0, 1)}}
		}
		return nil
	}
	target := false
	for _, r := range state.Config.Rules {
		if r.Enabled && r.Domain == name {
			target = true
			break
		}
	}
	if target && family == "AAAA" && q.Req.Question[0].Qclass == dns.ClassINET {
		q.Res = new(dns.Msg)
		q.Res.SetReply(q.Req)
		q.Res.RecursionAvailable = true
		select {
		case d.observed <- Query{Domain: name, Type: family, Client: source, Time: now.UTC()}:
		default:
		}
		return nil
	}
	optimized := false
	if q.Req.Question[0].Qclass == dns.ClassINET && !q.Req.CheckingDisabled && !(q.Req.IsEdns0() != nil && q.Req.IsEdns0().Do()) && family == "A" {
		for _, s := range state.Selections {
			if s.Domain != name || s.Family != family || s.IP == "" || !s.ValidUntil.After(now) {
				continue
			}
			enabled := false
			for _, r := range state.Config.Rules {
				if r.Enabled && r.ID == s.RuleID && r.Domain == name {
					enabled = true
				}
			}
			if !enabled || !state.Config.Enabled && !s.Fixed {
				break
			}
			q.Res = new(dns.Msg)
			q.Res.SetReply(q.Req)
			q.Res.RecursionAvailable = true
			q.Res.Authoritative = false
			q.Res.AuthenticatedData = false
			ttl := min(state.Config.TTL, uint32(max(1, int(time.Until(s.ValidUntil).Seconds()))))
			hdr := dns.RR_Header{Name: q.Req.Question[0].Name, Rrtype: q.Req.Question[0].Qtype, Class: dns.ClassINET, Ttl: ttl}
			q.Res.Answer = []dns.RR{&dns.A{Hdr: hdr, A: net.ParseIP(s.IP).To4()}}
			size := dns.MaxMsgSize
			if q.Proto == proxy.ProtoUDP {
				size = dns.MinMsgSize
			}
			if edns := q.Req.IsEdns0(); edns != nil {
				advertised := max(uint16(dns.MinMsgSize), edns.UDPSize())
				q.Res.SetEdns0(advertised, false)
				if q.Proto == proxy.ProtoUDP {
					size = int(advertised)
				}
			}
			q.Res.Compress = true
			q.Res.Truncate(size)
			optimized = true
			break
		}
	}
	select {
	case d.observed <- Query{Domain: name, Type: family, Client: source, Time: now.UTC(), Optimized: optimized}:
	default:
	}
	if optimized {
		return nil
	}
	return (proxy.DefaultHandler{}).ServeDNS(ctx, p, q)
}
func (d *DNS) validatePolicy(p Policy) error {
	if !d.testMode {
		if e := p.Config.Validate(); e != nil {
			return e
		}
	}
	if len(p.Selections) > 200 {
		return errors.New("策略节点过多")
	}
	seen := map[string]bool{}
	for _, s := range p.Selections {
		ruleOK := false
		for _, r := range p.Config.Rules {
			if r.ID == s.RuleID && r.Domain == s.Domain {
				ruleOK = true
			}
		}
		if !ruleOK || s.Family != "A" || s.IP != "" && (!PublicIP(s.IP) || Family(s.IP) != s.Family) {
			return errors.New("策略域名或节点无效")
		}
		k := key(s.Domain, s.Family)
		if seen[k] {
			return errors.New("重复策略")
		}
		seen[k] = true
		if s.ValidUntil.After(time.Now().Add(time.Duration(p.Config.PolicySeconds+5) * time.Second)) {
			return errors.New("策略有效期超出配置")
		}
	}
	// A local interface as upstream can create a DNS forwarding loop.
	host, port, _ := net.SplitHostPort(d.addr)
	for _, up := range p.Config.Upstreams {
		h, pt, e := net.SplitHostPort(up)
		if e != nil {
			continue
		}
		if h == host && pt == port {
			return errors.New("DNS 上游不能指向自身")
		}
		if pt == port {
			ips, _ := net.InterfaceAddrs()
			for _, a := range ips {
				v, _, _ := net.ParseCIDR(a.String())
				if v != nil && v.String() == h {
					return errors.New("DNS 上游不能指向自身监听地址")
				}
			}
		}
	}
	return nil
}
func (d *DNS) Apply(ctx context.Context, p Policy) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	var copyPolicy Policy
	if err = json.Unmarshal(data, &copyPolicy); err != nil {
		return err
	}
	p = copyPolicy
	normalizePolicy(&p)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.proxy == nil {
		return errors.New("DNS 未运行")
	}
	if e := d.validatePolicy(p); e != nil {
		return e
	}
	old := d.policy.Load()
	if p.Version < old.Version {
		return errors.New("策略版本回退被拒绝")
	}
	if p.Version == old.Version {
		before, _ := json.Marshal(struct {
			C Config
			S []Selection
		}{old.Config, old.Selections})
		after, _ := json.Marshal(struct {
			C Config
			S []Selection
		}{p.Config, p.Selections})
		if bytes.Equal(before, after) {
			return nil
		}
		if old.Version != 0 {
			return errors.New("相同版本内容不一致")
		}
	}
	a, _ := json.Marshal(old.Config.Upstreams)
	b, _ := json.Marshal(p.Config.Upstreams)
	changed := !bytes.Equal(a, b)
	var next *proxy.Proxy
	var e error
	if changed {
		next, e = d.makeProxy(p.Config)
		if e != nil {
			return e
		}
	}
	if e = AtomicJSON(filepath.Join(d.dir, "policy.json"), p); e != nil {
		return e
	}
	if changed {
		d.proxy.Shutdown(ctx)
		if e = next.Start(ctx); e != nil {
			d.proxy.Start(ctx)
			AtomicJSON(filepath.Join(d.dir, "policy.json"), old)
			return e
		}
		d.proxy = next
	}
	d.policy.Store(&p)
	return nil
}
func (d *DNS) Check(ctx context.Context, domain string) (DNSCheck, error) {
	if !DomainValid(domain) {
		return DNSCheck{}, fmt.Errorf("域名格式无效")
	}
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	host, port, err := net.SplitHostPort(d.addr)
	if err != nil {
		return DNSCheck{}, err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	response, duration, err := (&dns.Client{Timeout: 3 * time.Second}).ExchangeContext(ctx, msg, net.JoinHostPort(host, port))
	if err != nil {
		return DNSCheck{}, err
	}
	result := DNSCheck{Domain: domain, RCode: dns.RcodeToString[response.Rcode], Answers: []string{}, DurationMS: duration.Milliseconds()}
	for _, rr := range response.Answer {
		result.Answers = append(result.Answers, rr.String())
	}
	return result, nil
}

func (d *DNS) observeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case q := <-d.observed:
			if d.observer != nil {
				d.observer(ctx, q)
			}
		}
	}
}
func QueryHealth(addr string) error {
	q := new(dns.Msg)
	q.SetQuestion("health.dns-assistant.invalid.", dns.TypeA)
	r, _, e := (&dns.Client{Timeout: 2 * time.Second}).Exchange(q, addr)
	if e != nil {
		return e
	}
	if r.Rcode != 0 || len(r.Answer) == 0 {
		return fmt.Errorf("DNS 尚未就绪")
	}
	return nil
}

func normalizePolicy(p *Policy) {
	sort.Slice(p.Selections, func(i, j int) bool {
		return key(p.Selections[i].Domain, p.Selections[i].Family) < key(p.Selections[j].Domain, p.Selections[j].Family)
	})
	for i := range p.Selections {
		p.Selections[i].ValidUntil = p.Selections[i].ValidUntil.UTC()
		p.Selections[i].ChangedAt = p.Selections[i].ChangedAt.UTC()
	}
}
