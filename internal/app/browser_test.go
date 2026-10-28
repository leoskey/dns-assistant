package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in browser fixture: real controller, SQLite, DNS and HTTP measurement;
// all remote services are controlled local fixtures, never production endpoints.
func TestBrowserFixture(t *testing.T) {
	if os.Getenv("DNS_ASSISTANT_BROWSER_FIXTURE") != "1" {
		t.Skip("started explicitly by scripts/browser-check.cjs")
	}
	m := testManager(t)
	internal := httptest.NewServer(NewAPI(m).Internal())
	defer internal.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	up, _ := makeUpstream(t)
	d := NewDNS(unusedAddr(t), t.TempDir(), m.token, internal.URL)
	d.testMode = true
	dnsConfig := DefaultConfig()
	dnsConfig.Upstreams = []string{up}
	d.policy.Store(&Policy{Config: dnsConfig, Selections: []Selection{}})
	if err := d.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	var syncBlocked atomic.Bool
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/policy" && syncBlocked.Load() {
			w.WriteHeader(503)
			return
		}
		d.Handler().ServeHTTP(w, r)
	}))
	defer control.Close()
	m.dnsURL = control.URL
	c := m.Config()
	c.Rules = DefaultConfig().Rules[:2]
	c.FeedURL = "https://rules.example.com/table.json"
	if err := m.SetConfig(c); err != nil {
		t.Fatal(err)
	}
	var broken atomic.Bool
	m.prober, _ = probeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if broken.Load() {
			w.WriteHeader(503)
			return
		}
		streamSample(w, r)
	})
	m.resolve = func(context.Context, string, Config) []string { return []string{"1.1.1.1"} }
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream-speed" {
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			chunk := make([]byte, 64*1024)
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					if _, err := w.Write(chunk); err != nil {
						return
					}
					w.(http.Flusher).Flush()
				}
			}
		}
		w.Write([]byte("proxy-browser-fixture"))
	}))
	defer origin.Close()
	m.DownloadProxy.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(origin.URL, "http://"))
	}
	proxy := httptest.NewServer(m.DownloadProxy)
	defer proxy.Close()
	m.feedClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "bad") {
			return feedResponse(`{"version":"bad","rules":[]}`), nil
		}
		return feedResponse(`{"version":"browser-v2","rules":[{"platform":"Xbox","url":"http://xvcf1.xboxlive.com/large.bin","candidates":["1.1.1.1"]},{"platform":"PlayStation","url":"http://gst.prod.dl.playstation.net/sample","candidates":[]}]}`), nil
	})}
	for i := 0; i < 43; i++ {
		m.Observe(Query{Domain: "ordinary.example", Client: "192.168.1.20", Type: "A"})
	}
	// The core test policy uses a controlled loopback upstream. The real manager's
	// policy validation is exercised in other tests; DNS test mode permits this fixture.
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	stop := make(chan struct{}, 1)
	api := NewAPI(m).Handler()
	mux := http.NewServeMux()
	mux.Handle("/", api)
	mux.HandleFunc("POST /fixture/fail-health", func(w http.ResponseWriter, r *http.Request) {
		broken.Store(true)
		id := m.Config().Rules[0].ID
		if !m.Enqueue(Job{RuleID: id, Kind: "health"}) {
			jsonError(w, 409, "任务尚未完成")
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /fixture/recover", func(w http.ResponseWriter, r *http.Request) {
		broken.Store(false)
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /fixture/sync-off", func(w http.ResponseWriter, r *http.Request) {
		syncBlocked.Store(true)
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /fixture/sync-on", func(w http.ResponseWriter, r *http.Request) {
		syncBlocked.Store(false)
		m.Publish(r.Context())
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /fixture/stop", func(w http.ResponseWriter, r *http.Request) {
		select {
		case stop <- struct{}{}:
		default:
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	fmt.Println("BROWSER_PROXY_URL=" + proxy.URL)
	fmt.Println("BROWSER_FIXTURE_URL=" + server.URL)
	select {
	case <-stop:
	case <-time.After(180 * time.Second):
		t.Error("browser did not stop fixture")
	}
	cancel()
	<-done
}
