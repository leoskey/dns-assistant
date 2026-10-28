package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadProxyTargetAndAccessBoundaries(t *testing.T) {
	m := testManager(t)
	p := m.DownloadProxy
	p.dial = func(context.Context, string, string) (net.Conn, error) {
		t.Error("rejected request reached dial")
		return nil, errors.New("unexpected dial")
	}
	r := httptest.NewRequest("GET", "http://example.com/file", nil)
	r.RemoteAddr = "192.168.1.2:1234"
	var w *httptest.ResponseRecorder
	for _, target := range []struct{ method, target string }{
		{"GET", "/relative"}, {"GET", "https://example.com/file"}, {"GET", "http://example.com:8080/file"},
		{"GET", "http://user:secret@example.com/file"}, {"GET", "http://localhost/file"},
		{"GET", "http://127.0.0.1/file"}, {"GET", "http://192.168.1.1/file"}, {"GET", "http://169.254.169.254/file"},
		{"GET", "http://100.64.0.1/file"}, {"GET", "http://[::1]/file"}, {"GET", "http://[::ffff:127.0.0.1]/file"},
		{"CONNECT", "example.com:80"}, {"CONNECT", "example.com:22"}, {"CONNECT", "127.0.0.1:443"}, {"CONNECT", "example.com"},
	} {
		t.Run(target.method+target.target, func(t *testing.T) {
			r := httptest.NewRequest(target.method, target.target, nil)
			r.RemoteAddr = "192.168.1.2:1234"
			w := httptest.NewRecorder()
			p.ServeHTTP(w, r)
			if w.Code != 400 {
				t.Fatal("unsafe target accepted", w.Code, w.Body.String())
			}
		})
	}
	r.RemoteAddr = "8.8.8.8:1234"
	w = httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("public source accepted", w.Code)
	}
	r.RemoteAddr = "192.168.1.2:1234"
	for i := 0; i < cap(p.slots); i++ {
		p.slots <- struct{}{}
	}
	w = httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 503 || p.active.Load() != 0 {
		t.Fatal("concurrency limit ignored")
	}
}

func TestDownloadProxyUsesCurrentSelectionAndPublicFallback(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		paused, fixed, expired, disabledRule, failed bool
		host, want                                   string
	}{
		{name: "selected", want: "1.1.1.1:80"},
		{name: "expired", expired: true, want: "8.8.8.8:80"},
		{name: "paused", paused: true, want: "8.8.8.8:80"},
		{name: "paused fixed", paused: true, fixed: true, want: "1.1.1.1:80"},
		{name: "disabled rule", disabledRule: true, want: "8.8.8.8:80"},
		{name: "failed selected dial", failed: true, want: "8.8.8.8:80"},
		{name: "ordinary domain", host: "ordinary.example", want: "8.8.8.8:80"},
		{name: "public IP", host: "9.9.9.9", want: "9.9.9.9:80"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testManager(t)
			c := m.Config()
			c.Enabled = !tc.paused
			c.Rules[0].Enabled = !tc.disabledRule
			if err := m.SetConfig(c); err != nil {
				t.Fatal(err)
			}
			rule := c.Rules[0]
			until := time.Now().Add(time.Hour)
			if tc.expired {
				until = time.Now().Add(-time.Second)
			}
			m.selections[key(rule.Domain, "A")] = Selection{RuleID: rule.ID, Domain: rule.Domain, Family: "A", IP: "1.1.1.1", ValidUntil: until, Fixed: tc.fixed}
			m.resolve = func(context.Context, string, Config) []string {
				return []string{"127.0.0.1", "192.168.1.1", "::1", "8.8.8.8"}
			}
			var dialed []string
			p := m.DownloadProxy
			p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
				dialed = append(dialed, addr)
				if tc.failed && addr == "1.1.1.1:80" {
					return nil, errors.New("selected unavailable")
				}
				a, b := net.Pipe()
				b.Close()
				return a, nil
			}
			host := tc.host
			if host == "" {
				host = strings.ToUpper(rule.Domain) + "."
			}
			conn, err := p.connect(context.Background(), "tcp", host+":80")
			if err != nil {
				t.Fatal(err)
			}
			conn.Close()
			if dialed[len(dialed)-1] != tc.want {
				t.Fatal(dialed, "want", tc.want)
			}
			for _, addr := range dialed {
				if strings.HasPrefix(addr, "127.") || strings.HasPrefix(addr, "192.168.") {
					t.Fatal("private resolution dialed", dialed)
				}
			}
		})
	}
	m := testManager(t)
	m.resolve = func(context.Context, string, Config) []string { return []string{"127.0.0.1"} }
	if _, err := m.DownloadProxy.connect(context.Background(), "tcp", "rebound.example:80"); err == nil {
		t.Fatal("private-only DNS result accepted")
	}
}

func TestDownloadProxyHTTPRangeHeadersAndStreaming(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	m := testManager(t)
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Host != "download.example" || r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("X-Hop") != "" || r.Header.Get("Proxy-Connection") != "" {
			t.Error("host or proxy headers", r.Host, r.Header)
		}
		if r.Header.Get("Range") != "bytes=100-199" {
			t.Error("range lost")
		}
		if r.URL.RawQuery != "signed=example" {
			t.Error("query changed")
		}
		w.Header().Set("Content-Range", "bytes 100-199/1000")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Connection", "X-Origin-Hop")
		w.Header().Set("X-Origin-Hop", "remove")
		w.WriteHeader(206)
		w.(http.Flusher).Flush()
		if r.Method != "HEAD" {
			w.Write(bytes.Repeat([]byte("z"), 100))
		}
	}))
	defer origin.Close()
	m.resolve = func(context.Context, string, Config) []string { return []string{"8.8.8.8"} }
	m.DownloadProxy.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "8.8.8.8:80" {
			t.Error("dialed wrong target", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(origin.URL, "http://"))
	}
	proxy := httptest.NewServer(m.DownloadProxy)
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	for _, method := range []string{"GET", "HEAD"} {
		req, _ := http.NewRequest(method, "http://download.example/file?signed=example", nil)
		req.Header.Set("Range", "bytes=100-199")
		req.Header.Set("Proxy-Authorization", "Basic example-secret")
		req.Header.Set("Connection", "X-Hop")
		req.Header.Set("X-Hop", "remove")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 206 || resp.Header.Get("Content-Range") != "bytes 100-199/1000" || resp.Header.Get("X-Origin-Hop") != "" {
			t.Fatal(resp, err)
		}
		if method == "GET" && string(data) != strings.Repeat("z", 100) || method == "HEAD" && len(data) != 0 {
			t.Fatal("body changed", len(data))
		}
	}
	if requests.Load() != 2 || m.DownloadProxy.downloaded.Load() < 100 {
		t.Fatal("transfer counters missing")
	}
	m.DownloadProxy.forward.Transport.(*http.Transport).CloseIdleConnections()
	origin.Close()
	resp, err := client.Get("http://download.example/file")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 502 {
		t.Fatal("target failure did not return 502", resp.StatusCode)
	}
}

func TestDownloadProxyCONNECTPreservesTLS(t *testing.T) {
	m := testManager(t)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != "example.com" || r.Host != "example.com" {
			t.Error("TLS SNI/Host changed")
		}
		io.Copy(w, r.Body)
	}))
	defer origin.Close()
	m.resolve = func(context.Context, string, Config) []string { return []string{"8.8.8.8"} }
	m.DownloadProxy.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "8.8.8.8:443" {
			t.Error("wrong tunnel target", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(origin.URL, "https://"))
	}
	proxy := httptest.NewServer(m.DownloadProxy)
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	transport := origin.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Post("https://example.com/download", "application/octet-stream", strings.NewReader("encrypted-payload"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(data) != "encrypted-payload" || m.DownloadProxy.downloaded.Load() == 0 {
		t.Fatal(string(data), err)
	}
	page, err := m.store.ConnectionPage(PageRequest{Page: 1, Size: 10})
	if err != nil || page.Total != 1 || page.Items[0].Method != "CONNECT" || page.Items[0].Address != "example.com:443" || page.Items[0].Domain != "example.com" {
		t.Fatal("HTTPS records must contain the tunnel authority, never the encrypted path", page, err)
	}
	if _, err := client.Get("https://wrong.example/download"); err == nil {
		t.Fatal("invalid hostname certificate accepted")
	}
	untrusted := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	defer untrusted.CloseIdleConnections()
	if _, err := untrusted.Get("https://example.com/download"); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}

func TestDownloadProxyBufferedTunnelAndShutdown(t *testing.T) {
	m := testManager(t)
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		conn, err := echo.Accept()
		if err == nil {
			defer conn.Close()
			io.Copy(conn, conn)
		}
	}()
	m.DownloadProxy.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, echo.Addr().String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := unusedAddr(t)
	done := make(chan error, 1)
	go func() { done <- ServeDownloadProxy(ctx, addr, m.DownloadProxy) }()
	var conn net.Conn
	for i := 0; i < 100; i++ {
		conn, err = net.DialTimeout("tcp4", addr, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	io.WriteString(conn, "CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\n\r\nping")
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(resp, err)
	}
	var payload [4]byte
	if _, err := io.ReadFull(reader, payload[:]); err != nil || string(payload[:]) != "ping" {
		t.Fatal("buffered data lost", string(payload[:]), err)
	}
	if m.DownloadProxy.active.Load() != 1 {
		t.Fatal("active tunnel missing")
	}
	io.WriteString(conn, "pong")
	if _, err := io.ReadFull(reader, payload[:]); err != nil || string(payload[:]) != "pong" {
		t.Fatal("established tunnel lost data", err)
	}
	cancel()
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("shutdown left tunnel open")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("shutdown blocked")
	}
	for i := 0; i < 100 && m.DownloadProxy.active.Load() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if m.DownloadProxy.active.Load() != 0 {
		t.Fatal("tunnel leaked")
	}
}

func TestDownloadProxyConfigPersistenceBackupAndStatus(t *testing.T) {
	a, h := testAPI(t)
	body, _ := json.Marshal(a.m.Config())
	if bytes.Contains(body, []byte("proxy_enabled")) {
		t.Fatal("retired switch exported")
	}
	var fields map[string]json.RawMessage
	json.Unmarshal(body, &fields)
	fields["proxy_enabled"] = json.RawMessage("false")
	legacy, _ := json.Marshal(fields)
	if err := a.m.store.Set("config", json.RawMessage(legacy)); err != nil {
		t.Fatal(err)
	}
	restored, err := NewManager(a.m.store, a.m.dnsURL, a.m.token)
	if err != nil {
		t.Fatal("legacy disabled setting blocked startup", err)
	}
	if !reflect.DeepEqual(restored.Config(), a.m.Config()) {
		t.Fatal("migration changed other settings")
	}
	var persisted json.RawMessage
	if err := a.m.store.Get("config", &persisted); err != nil || bytes.Contains(persisted, []byte("proxy_enabled")) {
		t.Fatal("retired switch remained in storage", err)
	}
	if r := call(h, "PUT", "/api/v1/config", string(legacy), "1", ""); r.Code != 400 {
		t.Fatal("normal PUT accepted retired switch", r.Code)
	}
	for _, format := range []int{1, 2} {
		backup, _ := json.Marshal(map[string]any{"format": format, "config": json.RawMessage(legacy)})
		if r := call(h, "POST", "/api/v1/restore", string(backup), "1", ""); r.Code != 200 {
			t.Fatal("legacy disabled backup failed", r.Body.String())
		}
	}
	backup := call(h, "GET", "/api/v1/backup", "", "", "")
	if backup.Code != 200 || bytes.Contains(backup.Body.Bytes(), []byte("proxy_enabled")) {
		t.Fatal("backup exported retired switch", backup.Body.String())
	}
	t.Setenv("SERVER_IP", "192.168.1.30")
	t.Setenv("NAS_IP", "192.168.1.20")
	var status struct {
		DNSAddress string      `json:"dns_address"`
		Proxy      ProxyStatus `json:"proxy"`
	}
	r := call(h, "GET", "/api/v1/status", "", "", "")
	if json.Unmarshal(r.Body.Bytes(), &status) != nil || status.DNSAddress != "192.168.1.30" || !status.Proxy.Enabled || status.Proxy.Port != 8082 || status.Proxy.Address != status.DNSAddress {
		t.Fatal(r.Body.String())
	}
	t.Setenv("SERVER_IP", "")
	if deploymentIP() != "" {
		t.Fatal("retired NAS_IP must not be used")
	}
	t.Setenv("SERVER_IP", "invalid")
	if deploymentIP() != "" {
		t.Fatal("invalid primary address must not silently use legacy value")
	}
}

func TestLANIPv4Discovery(t *testing.T) {
	addrs := []net.Addr{}
	for _, value := range []string{"127.0.0.1/8", "::1/128", "192.168.2.15/24", "8.8.8.8/32", "169.254.1.1/16", "10.0.0.8/8", "192.168.2.15/24", "172.20.0.4/16", "fd12::1/64", "100.64.0.1/10", "0.0.0.0/0"} {
		_, addr, err := net.ParseCIDR(value)
		if err != nil {
			t.Fatal(err)
		}
		// ParseCIDR's network masks the host bits; retain the actual interface address.
		addr.IP = net.ParseIP(strings.Split(value, "/")[0])
		addrs = append(addrs, addr)
	}
	if got := lanIPv4s(addrs); !reflect.DeepEqual(got, []string{"192.168.2.15", "10.0.0.8", "172.20.0.4"}) {
		t.Fatal("LAN addresses must retain order, omit non-LAN addresses and deduplicate", got)
	}
	if got := lanIPv4s(nil); got == nil || len(got) != 0 {
		t.Fatal("empty discovery must return an empty list", got)
	}
	t.Setenv("SERVER_IP", "127.0.0.1")
	t.Setenv("NAS_IP", "192.168.2.15")
	if got := serviceAddresses(); len(got) != 0 {
		t.Fatal("invalid explicit address must not fall back to loopback or container interfaces", got)
	}
	t.Setenv("SERVER_IP", "10.0.0.8")
	if got := serviceAddresses(); !reflect.DeepEqual(got, []string{"10.0.0.8"}) {
		t.Fatal("explicit host address must take precedence", got)
	}
}
