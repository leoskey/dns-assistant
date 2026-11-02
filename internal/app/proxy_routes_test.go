package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProxyReconnectClosesOnlyMatchingTunnels(t *testing.T) {
	m := testManager(t)
	p := m.DownloadProxy
	p.dial = func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() { defer b.Close(); io.Copy(b, b) }()
		return a, nil
	}
	server := httptest.NewServer(p)
	defer server.Close()
	open := func(ip string) (net.Conn, *bufio.Reader) {
		t.Helper()
		conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		io.WriteString(conn, "CONNECT "+ip+":443 HTTP/1.1\r\nHost: "+ip+":443\r\n\r\n")
		reader := bufio.NewReader(conn)
		resp, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
		if err != nil || resp.StatusCode != 200 {
			conn.Close()
			t.Fatal(resp, err)
		}
		return conn, reader
	}
	first, firstReader := open("1.1.1.1")
	defer first.Close()
	second, secondReader := open("8.8.8.8")
	defer second.Close()
	if p.reconnect("1.1.1.1") != 1 {
		t.Fatal("target not found")
	}
	if _, err := firstReader.ReadByte(); err == nil {
		t.Fatal("target tunnel remains open")
	}
	io.WriteString(second, "ping")
	var payload [4]byte
	if _, err := io.ReadFull(secondReader, payload[:]); err != nil || string(payload[:]) != "ping" {
		t.Fatal("other tunnel interrupted", err)
	}
}

func TestProxyHTTPPoolFollowsSelectionAndUpstreams(t *testing.T) {
	m := testManager(t)
	p := m.DownloadProxy
	t.Cleanup(p.transport.CloseIdleConnections)
	rule := m.Config().Rules[0]
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != rule.Domain {
			t.Error("Host changed", r.Host)
		}
		io.WriteString(w, "download")
	}))
	defer origin.Close()
	m.resolve = func(_ context.Context, _ string, c Config) []string {
		if c.Upstreams[0] == "8.8.8.8:53" {
			return []string{"8.8.4.4"}
		}
		return []string{"9.9.9.9"}
	}
	dials := make(chan string, 20)
	p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials <- addr
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(origin.URL, "http://"))
	}
	server := httptest.NewServer(p)
	defer server.Close()
	proxyURL, _ := url.Parse(server.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	get := func(wantDial string) {
		t.Helper()
		resp, err := client.Get("http://" + rule.Domain + "/file")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || string(body) != "download" {
			t.Fatal(string(body), err)
		}
		got := ""
		select {
		case got = <-dials:
		default:
		}
		if got != wantDial {
			t.Fatalf("dial = %q, want %q", got, wantDial)
		}
	}
	setSelection := func(ip string, until time.Time) {
		m.mu.Lock()
		m.selections[key(rule.Domain, "A")] = Selection{RuleID: rule.ID, IP: ip, ValidUntil: until}
		m.mu.Unlock()
	}
	get("9.9.9.9:80")
	get("") // Stable routes must still reuse the origin connection.
	setSelection("1.1.1.1", time.Now().Add(time.Hour))
	get("1.1.1.1:80")
	get("")
	setSelection("8.8.8.8", time.Now().Add(time.Hour))
	get("8.8.8.8:80")
	setSelection("8.8.8.8", time.Now().Add(-time.Second))
	get("9.9.9.9:80")
	c := m.Config()
	c.Upstreams = []string{"8.8.8.8:53"}
	if err := m.SetConfig(c); err != nil {
		t.Fatal(err)
	}
	get("8.8.4.4:80")
}

func TestProxyLiveRoutesAndTargetedReconnect(t *testing.T) {
	m := testManager(t)
	p := m.DownloadProxy
	t.Cleanup(p.transport.CloseIdleConnections)
	rule := m.Config().Rules[0]
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("a"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer origin.Close()
	m.resolve = func(context.Context, string, Config) []string { return []string{"9.9.9.9"} }
	p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(origin.URL, "http://"))
	}
	m.mu.Lock()
	m.selections[key(rule.Domain, "A")] = Selection{RuleID: rule.ID, IP: "1.1.1.1", ValidUntil: time.Now().Add(time.Hour)}
	m.mu.Unlock()
	server := httptest.NewServer(p)
	defer server.Close()
	proxyURL, _ := url.Parse(server.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	open := func(domain string) *http.Response {
		t.Helper()
		resp, err := client.Get("http://" + domain + "/file")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	first, second := open(rule.Domain), open("unconfigured.example")
	defer first.Body.Close()
	defer second.Body.Close()
	h := NewAPI(m).Handler()
	read := func() []ProxyConnectionStatus {
		t.Helper()
		w := call(h, "GET", "/api/v1/proxy/connections", "", "", "")
		var routes []ProxyConnectionStatus
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &routes) != nil {
			t.Fatal(w.Body.String())
		}
		return routes
	}
	routes := read()
	if len(routes) != 2 || routes[0].IP != "1.1.1.1" || routes[0].State != "selected" || routes[1].IP != "9.9.9.9" || routes[1].State != "unconfigured" {
		t.Fatal(routes)
	}
	m.mu.Lock()
	s := m.selections[key(rule.Domain, "A")]
	s.IP = "8.8.8.8"
	m.selections[key(rule.Domain, "A")] = s
	m.mu.Unlock()
	routes = read()
	if !routes[0].NeedsReconnect || routes[0].LatestIP != "8.8.8.8" || routes[0].IP != "1.1.1.1" || routes[1].NeedsReconnect {
		t.Fatal(routes)
	}
	if w := call(h, "POST", "/api/v1/proxy/reconnect", `{"domain":"`+rule.Domain+`"}`, "", ""); w.Code != 403 {
		t.Fatal("missing CSRF check", w.Code)
	}
	if w := call(h, "POST", "/api/v1/proxy/reconnect", `{"domain":"127.0.0.1"}`, "1", ""); w.Code != 400 {
		t.Fatal("invalid target accepted", w.Code)
	}
	w := call(h, "POST", "/api/v1/proxy/reconnect", `{"domain":"`+rule.Domain+`"}`, "1", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"closed":1`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := io.ReadAll(first.Body); err == nil {
		t.Fatal("target download not interrupted")
	}
	var b [1]byte
	if _, err := io.ReadFull(second.Body, b[:]); err != nil || b[0] != 'a' {
		t.Fatal("unrelated download interrupted", err)
	}
	third := open(rule.Domain)
	defer third.Body.Close()
	routes = read()
	if len(routes) != 2 || routes[1].IP != "8.8.8.8" || routes[1].NeedsReconnect {
		t.Fatal("new request did not adopt new route", routes)
	}
	if p.reconnect("missing.example") != 0 {
		t.Fatal("unrelated target closed")
	}
}
