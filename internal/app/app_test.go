package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/miekg/dns"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigRejectsServerAsUpstream(t *testing.T) {
	// Private addresses in these tests are synthetic fixtures.
	t.Setenv("SERVER_IP", "192.168.50.10")
	c := DefaultConfig()
	c.Upstreams = []string{"192.168.50.10:53"}
	if err := c.Validate(); err == nil {
		t.Fatal("server address accepted as its own upstream")
	}
	c.Upstreams = []string{"192.168.50.1:53"}
	if err := c.Validate(); err != nil {
		t.Fatalf("other upstream rejected: %v", err)
	}
}

func TestConfigTrustBoundaries(t *testing.T) {
	c := DefaultConfig()
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "100.64.1.1", "169.254.169.254", "::1", "::ffff:192.168.1.1", "2001:db8::1", "224.0.0.1", "2606:4700:4700::1111"} {
		if PublicIP(ip) {
			t.Errorf("unsafe IP allowed: %s", ip)
		}
	}
	for _, ip := range []string{"1.1.1.1", "223.5.5.5"} {
		if !PublicIP(ip) {
			t.Errorf("public IP rejected: %s", ip)
		}
	}
	cases := []func(*Config){func(c *Config) { c.AllowedCIDRs = []string{"0.0.0.0/0"} }, func(c *Config) { c.Upstreams = []string{"127.0.0.1:53"} }, func(c *Config) { c.Rules[0].URL = "http://10.0.0.1/private" }, func(c *Config) {
		c.Rules[0].Domain = "downloads.example"
		c.Rules[0].URL = "https://login.microsoft.com/"
	}, func(c *Config) { c.Rules[0].Candidates = []string{"192.168.1.1"} }, func(c *Config) { c.ProbeSeconds = 0 }, func(c *Config) { c.Rules = append(c.Rules, c.Rules[0]) }}
	for i, change := range cases {
		c := DefaultConfig()
		change(&c)
		if c.Validate() == nil {
			t.Errorf("unsafe config %d accepted", i)
		}
	}
	if !AllowSource("192.168.1.5:5000", DefaultConfig()) || AllowSource("8.8.8.8:1234", DefaultConfig()) {
		t.Fatal("ACL mismatch")
	}
}
func result(ip string, speed float64, ok bool) Result {
	return Result{RuleID: "xbox-global", Domain: "xvcf1.xboxlive.com", Family: "A", IP: ip, Kind: "throughput", Speed: speed, OK: ok}
}
func TestSelectionSpeedAndFallback(t *testing.T) {
	c := DefaultConfig()
	now := time.Now()
	old := Choose(Selection{}, []Result{result("1.1.1.1", 20, true), result("8.8.8.8", 10, true)}, c, now)
	if old.IP != "1.1.1.1" {
		t.Fatal(old)
	}
	old.ChangedAt = now.Add(-time.Hour)
	close := Choose(old, []Result{result("1.1.1.1", 20, true), result("8.8.8.8", 20.5, true)}, c, now)
	if close.IP != old.IP {
		t.Fatal("near rates caused churn", close)
	}
	rr := []Result{result("1.1.1.1", 20, true), result("8.8.8.8", 40, true)}
	if got := Choose(old, rr, c, now); got.IP != "8.8.8.8" {
		t.Fatal("faster candidate needs extra rounds", got)
	}
	cooling := old
	cooling.ChangedAt = now
	if Choose(cooling, rr, c, now).IP != old.IP {
		t.Fatal("cooldown ignored")
	}
	if got := Choose(old, []Result{result("1.1.1.1", 0, false), result("8.8.8.8", 5, true)}, c, now); got.IP != "8.8.8.8" {
		t.Fatal(got)
	}
	if got := Choose(old, []Result{result("1.1.1.1", 0, false)}, c, now); got.IP != "" || got.ValidUntil.After(now) {
		t.Fatal(got)
	}
	old.Fixed = true
	if Choose(old, rr, c, now).IP != old.IP {
		t.Fatal("fixed moved")
	}
}
func TestStorePersistenceAndTransactions(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c := DefaultConfig()
	if e = s.SaveState(c, map[string]Selection{}, 1); e != nil {
		t.Fatal(e)
	}
	_, e = s.db.Exec("CREATE TRIGGER fail_version BEFORE UPDATE ON metadata WHEN NEW.key='version' BEGIN SELECT RAISE(ABORT,'readonly test'); END")
	if e != nil {
		t.Fatal(e)
	}
	c.Enabled = false
	if e = s.SaveState(c, map[string]Selection{}, 2); e == nil {
		t.Fatal("expected rollback")
	}
	var restored Config
	s.Get("config", &restored)
	if !restored.Enabled {
		t.Fatal("partial config commit")
	}
}
func probeFixture(t *testing.T, h http.HandlerFunc) (Prober, string) {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	dial := func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, strings.TrimPrefix(server.URL, "http://"))
	}
	return Prober{dial: dial}, server.URL
}
func TestProbeHTTPBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    bool
		prefix  string
	}{
		{"valid range", func(w http.ResponseWriter, r *http.Request) {
			if r.Host != "downloads.example" || r.Header.Get("Range") == "" {
				t.Error("host or range missing")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Range", "bytes 0-65535/131072")
			w.WriteHeader(206)
			w.Write(bytes.Repeat([]byte("a"), 65536))
		}, true, "aaa"},
		{"ignored range bounded", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(bytes.Repeat([]byte("z"), 200000))
		}, true, ""},
		{"html", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write(bytes.Repeat([]byte("a"), 65536))
		}, false, ""},
		{"forbidden", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }, false, ""},
		{"bad range", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", "bytes 1-65536/999999")
			w.WriteHeader(206)
		}, false, ""},
		{"wrong prefix", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(bytes.Repeat([]byte("b"), 65536))
		}, false, "aaa"},
		{"redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://127.0.0.1/private", 302) }, false, ""},
		{"redirect host", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://other.example/sample", 302) }, false, ""},
		{"redirect port", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://downloads.example:8080/sample", 302)
		}, false, ""},
		{"redirect scheme", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://downloads.example/sample", 302)
		}, false, ""},
		{"redirect path", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/other", 302) }, false, ""},
		{"redirect loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/sample", 302) }, false, ""},
		{"redirect query", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery == "" {
				http.Redirect(w, r, "/sample?download=1", 302)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(bytes.Repeat([]byte("a"), 65536))
		}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := probeFixture(t, tc.handler)
			c := DefaultConfig()
			r := c.Rules[0]
			r.Domain = "downloads.example"
			r.URL = "http://downloads.example/sample"
			r.ExpectedPrefix = tc.prefix
			v := p.Probe(context.Background(), r, "1.1.1.1", c, "health")
			if math.IsNaN(v.Speed) || math.IsInf(v.Speed, 0) {
				t.Fatal("non-finite throughput", v)
			}
			if v.OK != tc.want {
				t.Fatalf("%+v", v)
			}
			if v.Bytes > 65536 {
				t.Fatal("limit exceeded")
			}
		})
	}
}
func TestProbeTLSAndCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(bytes.Repeat([]byte("z"), 65536)) }))
	defer server.Close()
	p := Prober{dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, strings.TrimPrefix(server.URL, "https://"))
	}}
	c := DefaultConfig()
	r := c.Rules[0]
	r.URL = "https://" + r.Domain + "/sample"
	v := p.Probe(context.Background(), r, "1.1.1.1", c, "throughput")
	if v.OK {
		t.Fatal("untrusted TLS certificate accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v = p.Probe(ctx, r, "1.1.1.1", c, "throughput")
	if v.OK {
		t.Fatal("cancel ignored")
	}
	v = p.Probe(context.Background(), r, "127.0.0.1", c, "throughput")
	if v.OK || !strings.Contains(v.Error, "公网") {
		t.Fatal(v)
	}
}
func makeUpstream(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	count := &atomic.Int64{}
	handler := dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		count.Add(1)
		m := new(dns.Msg)
		m.SetReply(q)
		m.RecursionAvailable = true
		name := q.Question[0].Name
		switch {
		case name == "negative.example.":
			m.Rcode = dns.RcodeNameError
			rr, _ := dns.NewRR("negative.example. 30 IN SOA ns.example. hostmaster.example. 1 60 60 60 30")
			m.Ns = []dns.RR{rr}
		case name == "empty.example.":
			m.Answer = nil
		case name == "large.example.":
			for i := 0; i < 15; i++ {
				m.Answer = append(m.Answer, &dns.TXT{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 30}, Txt: []string{strings.Repeat("x", 180)}})
			}
		case name == "alias.example.":
			rr, _ := dns.NewRR("alias.example. 30 IN CNAME target.example.")
			a, _ := dns.NewRR("target.example. 30 IN A 9.9.9.9")
			m.Answer = []dns.RR{rr, a}
		default:
			if q.Question[0].Qtype == dns.TypeA {
				rr, _ := dns.NewRR(name + " 30 IN A 9.9.9.9")
				m.Answer = []dns.RR{rr}
			} else if q.Question[0].Qtype == dns.TypeAAAA {
				rr, _ := dns.NewRR(name + " 30 IN AAAA 2606:4700:4700::1111")
				m.Answer = []dns.RR{rr}
			}
		}
		if strings.HasPrefix(w.RemoteAddr().Network(), "udp") {
			size := 512
			if opt := q.IsEdns0(); opt != nil {
				size = max(512, int(opt.UDPSize()))
			}
			m.Truncate(size)
		}
		w.WriteMsg(m)
	})
	ln, pc := listenPair(t)
	addr := ln.Addr().String()
	udp := &dns.Server{PacketConn: pc, Handler: handler}
	tcp := &dns.Server{Listener: ln, Handler: handler}
	go udp.ActivateAndServe()
	go tcp.ActivateAndServe()
	t.Cleanup(func() { udp.Shutdown(); tcp.Shutdown() })
	return addr, count
}

var fixturePort atomic.Int64

func listenPair(t *testing.T) (net.Listener, net.PacketConn) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", 20000+fixturePort.Add(1))
		l, e := net.Listen("tcp", addr)
		if e != nil {
			continue
		}
		u, e := net.ListenPacket("udp", addr)
		if e == nil {
			return l, u
		}
		l.Close()
	}
	t.Fatal("no free TCP/UDP pair")
	return nil, nil
}
func unusedAddr(t *testing.T) string {
	l, u := listenPair(t)
	addr := l.Addr().String()
	l.Close()
	u.Close()
	return addr
}
func startDNS(t *testing.T) (*DNS, *atomic.Int64) {
	t.Helper()
	up, count := makeUpstream(t)
	d := NewDNS(unusedAddr(t), t.TempDir(), strings.Repeat("s", 40), "")
	d.testMode = true
	c := DefaultConfig()
	c.Upstreams = []string{up}
	d.policy.Store(&Policy{Config: c, Selections: []Selection{}})
	ctx, cancel := context.WithCancel(context.Background())
	if e := d.Start(ctx); e != nil {
		cancel()
		t.Fatal(e)
	}
	t.Cleanup(func() { cancel(); d.Close(context.Background()) })
	return d, count
}
func query(t *testing.T, d *DNS, name string, typ uint16, tcp, do, cd bool) *dns.Msg {
	t.Helper()
	q := new(dns.Msg)
	q.SetQuestion(dns.Fqdn(name), typ)
	q.CheckingDisabled = cd
	if do {
		q.SetEdns0(1232, true)
	}
	network := "udp"
	if tcp {
		network = "tcp"
	}
	r, _, e := (&dns.Client{Net: network, Timeout: 3 * time.Second}).Exchange(q, d.addr)
	if e != nil {
		t.Fatal(e)
	}
	if r.Id != q.Id {
		t.Fatal("ID mismatch")
	}
	return r
}
func TestDNSProtocolsCachePolicyAndExpiry(t *testing.T) {
	d, count := startDNS(t)
	domain := "xvcf1.xboxlive.com"
	a := query(t, d, domain, dns.TypeA, false, false, false)
	if !strings.Contains(a.String(), "9.9.9.9") {
		t.Fatal(a)
	}
	before := count.Load()
	query(t, d, domain, dns.TypeA, false, false, false)
	if count.Load() != before {
		t.Fatal("cache missed")
	}
	p := *d.policy.Load()
	p.Version = 1
	p.CreatedAt = time.Now()
	p.Selections = []Selection{{RuleID: "xbox-global", Domain: domain, Family: "A", IP: "1.1.1.1", ValidUntil: time.Now().Add(time.Minute)}}
	if e := d.Apply(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	for _, tcp := range []bool{false, true} {
		r := query(t, d, domain, dns.TypeA, tcp, false, false)
		if !strings.Contains(r.String(), "1.1.1.1") || r.AuthenticatedData || r.Authoritative {
			t.Fatal(r)
		}
	}
	r := query(t, d, domain, dns.TypeAAAA, false, false, false)
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 0 {
		t.Fatal("target AAAA must be NODATA", r)
	}
	for _, flags := range [][2]bool{{true, false}, {false, true}} {
		r = query(t, d, domain, dns.TypeA, false, flags[0], flags[1])
		if strings.Contains(r.String(), "1.1.1.1") {
			t.Fatal("DNSSEC policy bypass failed")
		}
	}
	p.Version = 2
	p.Selections[0].ValidUntil = time.Now().Add(-time.Second)
	if e := d.Apply(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(query(t, d, domain, dns.TypeA, false, false, false).String(), "1.1.1.1") {
		t.Fatal("expired policy")
	}
	if e := d.Apply(context.Background(), Policy{Version: 1, Config: p.Config}); e == nil {
		t.Fatal("version regression")
	}
	bad := p
	bad.Version = 3
	bad.Selections = append([]Selection{}, p.Selections...)
	bad.Selections[0].IP = "127.0.0.1"
	if e := d.Apply(context.Background(), bad); e == nil {
		t.Fatal("unsafe policy")
	}
}
func TestDNSCNAMEAndLargeResponses(t *testing.T) {
	d, _ := startDNS(t)
	if len(query(t, d, "alias.example", dns.TypeA, false, false, false).Answer) != 2 {
		t.Fatal("CNAME lost")
	}
	if query(t, d, "negative.example", dns.TypeA, false, false, false).Rcode != dns.RcodeNameError {
		t.Fatal("NXDOMAIN lost")
	}
	if len(query(t, d, "empty.example", dns.TypeAAAA, false, false, false).Answer) != 0 {
		t.Fatal("NODATA lost")
	}
	q := query(t, d, "large.example", dns.TypeTXT, false, false, false)
	if !q.Truncated {
		t.Fatal("UDP truncation missing")
	}
	q = query(t, d, "large.example", dns.TypeTXT, true, false, false)
	if len(q.Answer) != 15 || q.Truncated {
		t.Fatal("TCP incomplete", q)
	}
}
func TestPolicyPersistenceAndConcurrentUpdates(t *testing.T) {
	d, _ := startDNS(t)
	p := *d.policy.Load()
	p.Version = 1
	p.Selections = []Selection{{RuleID: "xbox-global", Domain: p.Config.Rules[0].Domain, Family: "A", IP: "1.1.1.1", ValidUntil: time.Now().Add(time.Minute)}}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				d.policy.Load()
			}
		}()
	}
	if e := d.Apply(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	wg.Wait()
	var saved Policy
	b, e := os.ReadFile(filepath.Join(d.dir, "policy.json"))
	if e != nil {
		t.Fatal(e)
	}
	if json.Unmarshal(b, &saved) != nil || saved.Version != 1 {
		t.Fatal("snapshot missing")
	}
	if e := d.Apply(context.Background(), p); e != nil {
		t.Fatal("idempotence", e)
	}
	p.Selections[0].IP = "8.8.8.8"
	if e := d.Apply(context.Background(), p); e == nil {
		t.Fatal("same version altered")
	}
}

func call(h http.Handler, method, path, body, header, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1"
	req.RemoteAddr = "192.168.1.50:12345"
	if header != "" {
		req.Header.Set("X-DNS-Request", header)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}
func testAPI(t *testing.T) (*API, http.Handler) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	m, e := NewManager(s, "http://127.0.0.1:1", strings.Repeat("s", 40))
	if e != nil {
		t.Fatal(e)
	}
	a := NewAPI(m)
	return a, a.Handler()
}

func TestStatusAdvertisesConfiguredDNSAddress(t *testing.T) {
	_, h := testAPI(t)
	for _, tc := range []struct{ configured, want string }{
		{"192.168.50.10", "192.168.50.10"},
		{" 192.168.50.10 ", "192.168.50.10"},
		{"fd12::100", ""},
		{"", ""}, {"localhost", ""}, {"192.168.50.10:8080", ""},
		{"127.0.0.1", ""}, {"::1", ""}, {"0.0.0.0", ""},
		{"169.254.1.1", ""}, {"224.0.0.1", ""}, {"2606:4700:4700::1111", ""},
	} {
		t.Run(tc.configured, func(t *testing.T) {
			t.Setenv("SERVER_IP", tc.configured)
			r := call(h, "GET", "/api/v1/status", "", "", "")
			var status struct {
				DNSAddress string `json:"dns_address"`
			}
			if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &status) != nil || status.DNSAddress != tc.want {
				t.Fatalf("advertised address mismatch: %s", r.Body.String())
			}
		})
	}
}

func TestLANManagementOriginBackupAndRestore(t *testing.T) {
	_, h := testAPI(t)
	if r := call(h, "GET", "/api/v1/status", "", "", ""); r.Code != 200 {
		t.Fatal("direct access failed", r.Body.String())
	}
	c := DefaultConfig()
	data, _ := json.Marshal(c)
	if call(h, "PUT", "/api/v1/config", string(data), "", "").Code != 403 {
		t.Fatal("missing request header accepted")
	}
	if call(h, "PUT", "/api/v1/config", string(data), "1", "https://evil.example").Code != 403 {
		t.Fatal("cross origin accepted")
	}
	if r := call(h, "PUT", "/api/v1/config", string(data), "1", ""); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	backup := call(h, "GET", "/api/v1/backup", "", "", "")
	if strings.Contains(backup.Body.String(), "password") || strings.Contains(backup.Body.String(), "hash") || strings.Contains(backup.Body.String(), "service_token") {
		t.Fatal("backup leaked secrets")
	}
	if call(h, "POST", "/api/v1/restore", backup.Body.String(), "1", "").Code != 200 {
		t.Fatal("restore failed")
	}
	if call(h, "POST", "/api/v1/probe", `{"rule_id":"xbox-global","ip":"127.0.0.1"}`, "1", "").Code != 400 {
		t.Fatal("private probe allowed")
	}
	for _, tc := range []struct{ host, remote string }{{"evil.example", "192.168.1.2:1234"}, {"127.0.0.1", "8.8.8.8:1234"}} {
		r := httptest.NewRequest("GET", "/api/v1/status", nil)
		r.Host = tc.host
		r.RemoteAddr = tc.remote
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Code != 403 {
			t.Fatal("untrusted host/source accepted")
		}
	}
	if call(h, "POST", "/api/v1/setup", `{}`, "1", "").Code != 404 {
		t.Fatal("obsolete account endpoint")
	}
}
func TestJSONAndServiceAuthentication(t *testing.T) {
	_, h := testAPI(t)
	for _, body := range []string{`{"unknown":true}`, `{} {}`, strings.Repeat("x", 2<<20)} {
		if call(h, "PUT", "/api/v1/config", body, "1", "").Code != 400 {
			t.Fatal("bad input allowed")
		}
	}
	d, _ := startDNS(t)
	if call(d.Handler(), "PUT", "/internal/policy", "{}", "", "").Code != 401 {
		t.Fatal("internal no auth")
	}
}
func TestAutomaticManagerPublishAndFailedPin(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var publications atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 40) {
			t.Error("missing auth")
		}
		publications.Add(1)
		jsonOut(w, 200, map[string]bool{"ok": true})
	}))
	defer server.Close()
	m, e := NewManager(s, server.URL, strings.Repeat("s", 40))
	if e != nil {
		t.Fatal(e)
	}
	m.prober, _ = probeFixture(t, streamSample)
	m.resolve = func(context.Context, string, Config) []string { return []string{"1.1.1.1"} }
	c := DefaultConfig()
	c.ProbeSeconds = 1
	c.Rules = c.Rules[:1]
	c.Rules[0].Candidates = []string{"1.1.1.1"}
	if e = m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.RLock()
		selected := m.selections[key(c.Rules[0].Domain, "A")]
		m.mu.RUnlock()
		if selected.IP != "" && publications.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.mu.RLock()
	selected := m.selections[key(c.Rules[0].Domain, "A")]
	m.mu.RUnlock()
	if selected.IP != "1.1.1.1" || publications.Load() == 0 {
		h, _ := s.History(5)
		m.mu.RLock()
		task := m.tasks["xbox-global"]
		m.mu.RUnlock()
		t.Fatal("automatic worker did not apply", selected, task, h)
	}

	if m.Enqueue(Job{RuleID: "missing", Kind: "throughput"}) {
		t.Fatal("unknown queued")
	}
	m.mu.RLock()
	old := m.version
	m.mu.RUnlock()
	if e = m.SetConfig(Config{}); e == nil {
		t.Fatal("invalid config saved")
	}
	m.mu.RLock()
	if old != m.version {
		t.Fatal("failed edit changed state")
	}
	m.mu.RUnlock()
}

func TestSSEReconnectionAndStaticApp(t *testing.T) {
	_, h := testAPI(t)
	server := httptest.NewServer(h)
	defer server.Close()
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/events", nil)
		resp, e := server.Client().Do(req)
		if e != nil {
			cancel()
			t.Fatal(e)
		}
		buf := make([]byte, 80)
		n, e := resp.Body.Read(buf)
		resp.Body.Close()
		cancel()
		if e != nil || !strings.Contains(string(buf[:n]), "event: update") {
			t.Fatal("no live events", e)
		}
	}
	rr := call(h, "GET", "/platforms", "", "", "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "DNS Assistant") {
		t.Fatal("SPA routing", rr.Code, rr.Body.String())
	}
}
func TestAtomicSnapshotFailureKeepsCurrent(t *testing.T) {
	d, _ := startDNS(t)
	p := *d.policy.Load()
	p.Version = 1
	d.dir = filepath.Join(t.TempDir(), "missing")
	if e := d.Apply(context.Background(), p); e == nil {
		t.Fatal("unwritable path accepted")
	}
	if d.policy.Load().Version != 0 {
		t.Fatal("uncommitted snapshot applied")
	}
}

var _ = fmt.Sprint
var _ = io.EOF

func streamSample(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	chunk := bytes.Repeat([]byte("z"), 65536)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
		if _, err := w.Write(chunk); err != nil {
			return
		}
		w.(http.Flusher).Flush()
	}
}
func TestProbeTimeWindowAndHealthMinimum(t *testing.T) {
	p, _ := probeFixture(t, streamSample)
	c := DefaultConfig()
	c.ProbeSeconds = 1
	r := c.Rules[0]
	v := p.Probe(context.Background(), r, "1.1.1.1", c, "throughput")
	if !v.OK || v.Bytes < 1<<20 || v.MeasuredMS < 700 || v.DurationMS < 900 {
		t.Fatal("sustained window rejected", v)
	}
	r.MinBytes = 1 << 20
	v = p.Probe(context.Background(), r, "1.1.1.1", c, "health")
	if !v.OK || v.Bytes != 65536 {
		t.Fatal("health cap/minimum", v)
	}
}
func TestHealthFailureRestoresDefaultBeforeRetest(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	publishedCh := make(chan Policy, 1)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p Policy
		json.NewDecoder(r.Body).Decode(&p)
		publishedCh <- p
		jsonOut(w, 200, map[string]bool{"ok": true})
	}))
	defer control.Close()
	m, e := NewManager(s, control.URL, strings.Repeat("s", 40))
	if e != nil {
		t.Fatal(e)
	}
	c := DefaultConfig()
	c.Rules = c.Rules[:1]
	r := c.Rules[0]
	m.config = c
	m.selections[key(r.Domain, "A")] = Selection{RuleID: r.ID, Domain: r.Domain, Family: "A", IP: "1.1.1.1", ValidUntil: time.Now().Add(time.Hour)}
	m.prober, _ = probeFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	m.execute(context.Background(), Job{RuleID: r.ID, Kind: "health"}, r, c)
	selected := m.selections[key(r.Domain, "A")]
	if selected.ValidUntil.After(time.Now()) || selected.Fixed {
		t.Fatal("bad node still active", selected)
	}
	published := <-publishedCh
	if len(published.Selections) != 1 || published.Selections[0].ValidUntil.After(time.Now()) {
		t.Fatal("default was not published before retest", published)
	}
	if m.nextTestLocked(r.ID, c).After(time.Now()) {
		t.Fatal("recovery test not due")
	}
}
func TestCoreReloadsSnapshotWithoutController(t *testing.T) {
	d, _ := startDNS(t)
	p := *d.policy.Load()
	p.Version = 1
	p.Selections = []Selection{{RuleID: "xbox-global", Domain: p.Config.Rules[0].Domain, Family: "A", IP: "1.1.1.1", ValidUntil: time.Now().Add(time.Minute)}}
	if e := d.Apply(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	dir := d.dir
	d.Close(context.Background())
	restarted := NewDNS(d.addr, dir, d.token, "")
	restarted.testMode = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if e := restarted.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer restarted.Close(context.Background())
	if !strings.Contains(query(t, restarted, p.Config.Rules[0].Domain, dns.TypeA, false, false, false).String(), "1.1.1.1") {
		t.Fatal("snapshot lost after restart")
	}
}

func TestPolicyOrderAndTimezoneAreIdempotent(t *testing.T) {
	d, _ := startDNS(t)
	p := *d.policy.Load()
	now := time.Now().UTC()
	p.Version = 1
	p.Selections = []Selection{{RuleID: "xbox-global", Domain: p.Config.Rules[0].Domain, Family: "A", IP: "1.1.1.1", ValidUntil: now.Add(time.Minute)}, {RuleID: "xbox-global", Domain: p.Config.Rules[0].Domain, Family: "A", IP: "8.8.8.8", ValidUntil: now.Add(time.Minute)}}
	p.Selections[1].RuleID = p.Config.Rules[1].ID
	p.Selections[1].Domain = p.Config.Rules[1].Domain
	if e := d.Apply(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	p.Selections[0], p.Selections[1] = p.Selections[1], p.Selections[0]
	p.Selections[0].ValidUntil = p.Selections[0].ValidUntil.In(time.FixedZone("local", 8*3600))
	if e := d.Apply(context.Background(), p); e != nil {
		t.Fatal("logical retry rejected", e)
	}
}

func TestDNSACLAndRateLimit(t *testing.T) {
	d := NewDNS("127.0.0.1:5353", t.TempDir(), strings.Repeat("s", 40), "")
	q := new(dns.Msg)
	q.SetQuestion("example.com.", dns.TypeA)
	request := &proxy.DNSContext{Req: q, Addr: netip.MustParseAddrPort("8.8.8.8:1234"), Proto: proxy.ProtoUDP}
	if e := d.handle(context.Background(), nil, request); e != nil || request.Res.Rcode != dns.RcodeRefused {
		t.Fatal("public source not refused")
	}
	d.minute = time.Now().Unix() / 60
	d.counts["192.168.1.2"] = 3000
	request = &proxy.DNSContext{Req: q, Addr: netip.MustParseAddrPort("192.168.1.2:1234"), Proto: proxy.ProtoUDP}
	if e := d.handle(context.Background(), nil, request); e != nil || request.Res.Rcode != dns.RcodeRefused {
		t.Fatal("rate not enforced")
	}
}
func TestFailedPinAndReadonlyConfigKeepPublishedState(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m, e := NewManager(s, "http://127.0.0.1:1", strings.Repeat("s", 40))
	if e != nil {
		t.Fatal(e)
	}
	c := DefaultConfig()
	c.Rules = c.Rules[:1]
	r := c.Rules[0]
	m.config = c
	old := Selection{RuleID: r.ID, Domain: r.Domain, Family: "A", IP: "1.1.1.1", ValidUntil: time.Now().Add(time.Minute)}
	m.selections[key(r.Domain, "A")] = old
	m.prober, _ = probeFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	m.execute(context.Background(), Job{RuleID: r.ID, Kind: "throughput", IP: "8.8.8.8"}, r, c)
	if got := m.selections[key(r.Domain, "A")]; got.IP != old.IP || got.Fixed {
		t.Fatal("failed fixed node replaced incumbent", got)
	}
	if e = s.SaveState(c, m.selections, m.version); e != nil {
		t.Fatal(e)
	}
	_, e = s.db.Exec("CREATE TRIGGER reject_version BEFORE UPDATE ON metadata WHEN NEW.key='version' BEGIN SELECT RAISE(ABORT,'write denied'); END")
	if e != nil {
		t.Fatal(e)
	}
	c.Enabled = false
	if e = m.SetConfig(c); e == nil {
		t.Fatal("failed storage accepted")
	}
	if !m.Config().Enabled {
		t.Fatal("uncommitted config became active")
	}
}

func TestProbeProfileSeparatesDifferentSamples(t *testing.T) {
	c := DefaultConfig()
	r := c.Rules[0]
	first := probeProfile(r, c)
	r.URL += "?sample=2"
	if probeProfile(r, c) == first {
		t.Fatal("sample change did not invalidate history")
	}
	r = c.Rules[0]
	c.ProbeSeconds++
	if probeProfile(r, c) == first {
		t.Fatal("window change did not invalidate history")
	}
}

func TestDNSUpstreamFailureAndNonblockingObservation(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	up, _ := makeUpstream(t)
	d := NewDNS(unusedAddr(t), t.TempDir(), strings.Repeat("s", 40), server.URL)
	d.testMode = true
	c := DefaultConfig()
	c.Upstreams = []string{unusedAddr(t), up}
	d.policy.Store(&Policy{Config: c})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if e := d.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer d.Close(context.Background())
	started := time.Now()
	if r := query(t, d, "fallback.example", dns.TypeA, false, false, false); r.Rcode != dns.RcodeSuccess || len(r.Answer) == 0 {
		t.Fatal("healthy upstream unavailable", r)
	}
	if time.Since(started) > time.Second {
		t.Fatal("healthy upstream waited for failed peer")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("query observation missing")
	}
	started = time.Now()
	query(t, d, "observe.example", dns.TypeA, false, false, false)
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("query waited for blocked observer")
	}
	p := *d.policy.Load()
	p.Version = 1
	p.Config.Upstreams = []string{unusedAddr(t)}
	if e := d.Apply(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	q := new(dns.Msg)
	q.SetQuestion("all-failed.example.", dns.TypeA)
	r, _, e := (&dns.Client{Timeout: 6 * time.Second}).Exchange(q, d.addr)
	if e != nil || r.Rcode != dns.RcodeServerFailure {
		t.Fatal("failed upstream response", r, e)
	}
}

func TestObservedQueriesDeduplicateAndPause(t *testing.T) {
	a, _ := testAPI(t)
	m := a.m
	c := m.Config()
	c.Rules = c.Rules[:1]
	if e := m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 20; i++ {
		m.Observe(Query{Domain: c.Rules[0].Domain, Client: "192.168.1.60", Type: "A"})
	}
	if len(m.queue) != 1 {
		t.Fatal("queries created duplicate jobs", len(m.queue))
	}
	<-m.queue
	m.mu.Lock()
	m.pending = map[string]bool{}
	m.mu.Unlock()
	c.Enabled = false
	if e := m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	m.Observe(Query{Domain: c.Rules[0].Domain, Client: "192.168.1.60"})
	if len(m.queue) != 0 {
		t.Fatal("paused observation started job")
	}
	c.Enabled = true
	if e := m.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	m.Observe(Query{Domain: c.Rules[0].Domain, Client: "192.168.1.60"})
	if len(m.queue) != 1 {
		t.Fatal("resume did not schedule")
	}
}
