package app

import (
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

func TestConnectionRecordsCaptureFullHTTPAddress(t *testing.T) {
	m := testManager(t)
	address := "http://files.xboxlive.com/a%2Fb/file.bin?ticket=a%2Bb&empty=&n=1&n=2"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/second" {
			page, err := m.store.ConnectionPage(PageRequest{Page: 1, Size: 10})
			if err != nil || page.Total != 1 || page.Items[0].Address != address {
				t.Error("request must be recorded before waiting for the origin", page, err)
			}
		}
		io.WriteString(w, "download-sample")
	}))
	defer origin.Close()
	m.resolve = func(context.Context, string, Config) []string { return []string{"8.8.8.8"} }
	m.DownloadProxy.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(origin.URL, "http://"))
	}
	proxy := httptest.NewServer(m.DownloadProxy)
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || string(data) != "download-sample" {
		t.Fatal(resp.StatusCode, string(data), err)
	}
	h := NewAPI(m).Handler()
	r := call(h, "GET", "/api/v1/connections", "", "", "")
	var page Page[Connection]
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
		t.Fatal(r.Body.String())
	}
	c := page.Items[0]
	if c.Address != address || c.Domain != "files.xboxlive.com" || c.Method != "GET" || c.Client != "127.0.0.1" || c.Time.IsZero() || c.ID < 1 {
		t.Fatal(c)
	}
	for _, tc := range []struct{ address, client string }{{"http://127.0.0.1/private", "192.168.1.5:1234"}, {address, "8.8.8.8:1234"}} {
		req := httptest.NewRequest("GET", tc.address, nil)
		req.RemoteAddr = tc.client
		m.DownloadProxy.ServeHTTP(httptest.NewRecorder(), req)
	}
	if page, err := m.store.ConnectionPage(PageRequest{Page: 1, Size: 10}); err != nil || page.Total != 1 {
		t.Fatal("rejected requests entered connection records", page, err)
	}
	if r := call(h, "GET", "/api/v1/connections?page_size=1000", "", "", ""); r.Code != 400 {
		t.Fatal("unbounded page accepted", r.Code)
	}
	if _, err := m.store.db.Exec("DROP TABLE connections"); err != nil {
		t.Fatal(err)
	}
	resp, err = client.Get("http://files.xboxlive.com/second")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("record failure interrupted the transfer", resp.StatusCode)
	}
	if r := call(h, "GET", "/api/v1/connections", "", "", ""); r.Code != 500 {
		t.Fatal("storage error hidden", r.Code)
	}
}

func TestConnectionSchemaThreeMigration(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveState(DefaultConfig(), map[string]Selection{}, 42); err != nil {
		t.Fatal(err)
	}
	if err = s.AddQuery(Query{Domain: "example.com", Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DROP TABLE connections; PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version uint64
	if err = s.Get("version", &version); err != nil || version != 42 {
		t.Fatal("migration changed the active policy", version, err)
	}
	if p, err := s.QueryPage(PageRequest{Page: 1, Size: 10}); err != nil || p.Total != 1 {
		t.Fatal("migration lost existing records", p, err)
	}
	if err := s.AddConnection(Connection{Time: time.Now(), Method: "CONNECT", Domain: "example.com", Address: "example.com:443"}); err != nil {
		t.Fatal(err)
	}
	if p, err := s.ConnectionPage(PageRequest{Page: 1, Size: 10}); err != nil || p.Total != 1 {
		t.Fatal("new records unavailable after migration", p, err)
	}
}
