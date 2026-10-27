package app

import (
	"context"
	"crypto/rand"
	"dns-assistant/internal/ui"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonOut(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if e := dec.Decode(v); e != nil {
		jsonError(w, 400, "请求格式无效: "+e.Error())
		return false
	}
	var extra any
	if e := dec.Decode(&extra); e != io.EOF {
		jsonError(w, 400, "请求只能包含一个 JSON 值")
		return false
	}
	return true
}
func randomToken() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

type API struct {
	m       *Manager
	started time.Time
}

func NewAPI(m *Manager) *API { return &API{m: m, started: time.Now()} }
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, e := url.Parse(origin)
	return e == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if _, e := a.m.store.db.Exec("SELECT 1"); e != nil {
			jsonError(w, 503, "数据库不可用")
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/v1/status", a.status)
	protected.HandleFunc("GET /api/v1/config", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, a.m.Config()) })
	protected.HandleFunc("POST /api/v1/probe-batch", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Platform string `json:"platform"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.Platform != "" && input.Platform != "Xbox" && input.Platform != "PlayStation" && input.Platform != "Switch" {
			jsonError(w, 400, "平台无效")
			return
		}
		config := a.m.Config()
		if !config.Enabled {
			jsonError(w, 409, "自动选优已暂停，请先恢复")
			return
		}
		queued, skipped := []string{}, []string{}
		for _, rule := range config.Rules {
			if !rule.Enabled || (input.Platform != "" && rule.Platform != input.Platform) {
				continue
			}
			if a.m.Enqueue(Job{RuleID: rule.ID, Kind: "throughput"}) {
				queued = append(queued, rule.ID)
			} else {
				skipped = append(skipped, rule.ID)
			}
		}
		jsonOut(w, 202, map[string]any{"queued": queued, "skipped": skipped})
	})
	protected.HandleFunc("PUT /api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		var c Config
		if !decode(w, r, &c) {
			return
		}
		if e := a.m.SetConfig(c); e != nil {
			jsonError(w, 400, e.Error())
			return
		}
		jsonOut(w, 200, c)
	})
	protected.HandleFunc("GET /api/v1/queries", func(w http.ResponseWriter, r *http.Request) {
		p, err := parsePage(r)
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		page, err := a.m.store.QueryPage(p)
		if err != nil {
			jsonError(w, 500, "查询记录读取失败")
			return
		}
		jsonOut(w, 200, page)
	})
	protected.HandleFunc("GET /api/v1/history", func(w http.ResponseWriter, r *http.Request) {
		p, err := parsePage(r)
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		filter := r.URL.Query().Get("filter")
		if filter != "" && filter != "all" && filter != "throughput" && filter != "health" && filter != "failed" {
			jsonError(w, 400, "记录筛选无效")
			return
		}
		page, err := a.m.store.ResultPage(p, filter)
		if err != nil {
			jsonError(w, 500, "测速记录读取失败")
			return
		}
		jsonOut(w, 200, page)
	})
	protected.HandleFunc("GET /api/v1/history/events", func(w http.ResponseWriter, r *http.Request) {
		p, err := parsePage(r)
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		page, err := a.m.store.EventPage(p)
		if err != nil {
			jsonError(w, 500, "运行记录读取失败")
			return
		}
		jsonOut(w, 200, page)
	})
	protected.HandleFunc("GET /api/v1/history/daily", func(w http.ResponseWriter, r *http.Request) {
		p, err := parsePage(r)
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		page, err := a.m.store.DailyPage(p)
		if err != nil {
			jsonError(w, 500, "每日汇总读取失败")
			return
		}
		jsonOut(w, 200, page)
	})
	protected.HandleFunc("POST /api/v1/rules/sync", func(w http.ResponseWriter, r *http.Request) {
		if err := a.m.SyncFeed(r.Context()); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		a.m.Publish(r.Context())
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	protected.HandleFunc("POST /api/v1/probe", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			RuleID string `json:"rule_id"`
			IP     string `json:"ip"`
		}
		if !decode(w, r, &v) {
			return
		}
		if v.IP != "" && !PublicIP(v.IP) {
			jsonError(w, 400, "固定节点必须为公网 IPv4")
			return
		}
		if !a.m.Config().Enabled && v.IP == "" {
			jsonError(w, 409, "自动选优已暂停，请先恢复")
			return
		}
		if !a.m.Enqueue(Job{RuleID: v.RuleID, Kind: "throughput", IP: v.IP}) {
			jsonError(w, 409, "任务已排队、规则未启用或任务队列已满")
			return
		}
		jsonOut(w, 202, map[string]string{"state": "queued"})
	})
	protected.HandleFunc("POST /api/v1/default", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			RuleID string `json:"rule_id"`
		}
		if !decode(w, r, &v) {
			return
		}
		if e := a.m.RestoreDefault(v.RuleID); e != nil {
			jsonError(w, 500, e.Error())
			return
		}
		a.m.Publish(r.Context())
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	protected.HandleFunc("GET /api/v1/backup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="dns-assistant-config.json"`)
		jsonOut(w, 200, struct {
			Format int    `json:"format"`
			Config Config `json:"config"`
		}{2, a.m.Config()})
	})
	protected.HandleFunc("POST /api/v1/restore", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Format int             `json:"format"`
			Config json.RawMessage `json:"config"`
		}
		if !decode(w, r, &v) {
			return
		}
		if v.Format != 1 && v.Format != 2 {
			jsonError(w, 400, "备份格式不支持")
			return
		}
		c, e := upgradeConfig(v.Config)
		if e != nil {
			jsonError(w, 400, e.Error())
			return
		}
		if e := a.m.SetConfig(c); e != nil {
			jsonError(w, 400, e.Error())
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	protected.HandleFunc("POST /api/v1/check", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Domain string `json:"domain"`
			Type   string `json:"type"`
		}
		if !decode(w, r, &v) {
			return
		}
		if v.Type != "" && v.Type != "A" {
			jsonError(w, 400, "接入检查只支持 IPv4 A 记录")
			return
		}
		if !DomainValid(v.Domain) {
			jsonError(w, 400, "请输入有效域名")
			return
		}
		b, _ := json.Marshal(v)
		req, e := http.NewRequestWithContext(r.Context(), "POST", a.m.dnsURL+"/internal/check", strings.NewReader(string(b)))
		if e != nil {
			jsonError(w, 500, e.Error())
			return
		}
		req.Header.Set("Authorization", "Bearer "+a.m.token)
		resp, e := a.m.client.Do(req)
		if e != nil {
			jsonError(w, 502, "DNS 核心尚未连接")
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, io.LimitReader(resp.Body, 1<<20))
	})
	protected.HandleFunc("GET /api/v1/events", a.events)
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			if !sameOrigin(r) || r.Header.Get("X-DNS-Request") != "1" {
				jsonError(w, 403, "请求来源或安全请求头无效")
				return
			}
		}
		protected.ServeHTTP(w, r)
	}))
	assets, _ := fs.Sub(ui.Files, "dist")
	server := http.FileServer(http.FS(assets))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/assets/") && r.URL.Path != "/favicon.svg" {
			r.URL.Path = "/"
		}
		server.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !AllowSource(r.RemoteAddr, a.m.Config()) || !adminHostAllowed(r, a.m.Config()) {
			jsonError(w, 403, "管理界面仅允许配置的局域网来源与主机名")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}
func (a *API) status(w http.ResponseWriter, r *http.Request) {
	c := a.m.Config()
	a.m.mu.RLock()
	selections := []Selection{}
	for _, s := range a.m.selections {
		selections = append(selections, s)
	}
	nextTests := map[string]time.Time{}
	lastTests := map[string]time.Time{}
	for _, rule := range c.Rules {
		next := a.m.nextTestLocked(rule.ID, c)
		if next.Before(time.Now()) {
			next = time.Now()
		}
		nextTests[rule.ID] = next
		if last := a.m.last[rule.ID+"/throughput"]; !last.IsZero() {
			lastTests[rule.ID] = last
		}
	}
	tasks := []Task{}
	for _, t := range a.m.tasks {
		tasks = append(tasks, t)
	}
	version, applied := a.m.version, a.m.applied
	feed := a.m.feed
	dnsConnected := time.Since(a.m.lastSync) < 25*time.Second
	a.m.mu.RUnlock()
	dnsAddress := deploymentIP()
	proxy := ProxyStatus{Enabled: c.ProxyEnabled, Address: dnsAddress, Port: 8082, ActiveTransfers: a.m.DownloadProxy.active.Load(), DownloadedBytes: a.m.DownloadProxy.downloaded.Load()}
	jsonOut(w, 200, map[string]any{"app_version": Version(), "app_revision": Revision, "enabled": c.Enabled, "selections": selections, "tasks": tasks, "version": version, "applied_version": applied, "dns_connected": dnsConnected, "dns_address": dnsAddress, "proxy": proxy, "feed": feed, "uptime_seconds": int(time.Since(a.started).Seconds()), "rules": c.Rules, "next_tests": nextTests, "last_tests": lastTests, "interval_seconds": c.IntervalSeconds})
}

func parsePage(r *http.Request) (PageRequest, error) {
	p := PageRequest{Page: 1, Size: 10}
	for name, dest := range map[string]*int{"page": &p.Page, "page_size": &p.Size} {
		if values, ok := r.URL.Query()[name]; ok {
			if len(values) != 1 {
				return p, errors.New("分页参数重复")
			}
			n, err := strconv.Atoi(values[0])
			if err != nil || n < 1 || n > 100000 {
				return p, errors.New("分页参数无效")
			}
			*dest = n
		}
	}
	if p.Size != 10 && p.Size != 20 && p.Size != 50 && p.Size != 100 {
		return p, errors.New("每页条数须为 10、20、50 或 100")
	}
	if values, ok := r.URL.Query()["snapshot"]; ok {
		if len(values) != 1 {
			return p, errors.New("分页快照重复")
		}
		n, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || n < 0 {
			return p, errors.New("分页快照无效")
		}
		p.Snapshot = n
	}
	return p, nil
}
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	f, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, 500, "状态推送不支持")
		return
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {

		fmt.Fprint(w, "event: update\ndata: {}\n\n")
		f.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *API) Internal() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/observe", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, a.m.token) {
			jsonError(w, 401, "服务认证失败")
			return
		}
		var q Query
		if !decode(w, r, &q) {
			return
		}
		a.m.Observe(q)
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	return mux
}
func Serve(ctx context.Context, addr string, h http.Handler) error {
	s := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.Shutdown(c)
	}()
	listener, e := net.Listen("tcp4", addr)
	if e != nil {
		return e
	}
	e = s.Serve(listener)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}

func NewKey() string { return randomToken() }

func adminHostAllowed(r *http.Request, c Config) bool {
	host := r.Host
	if h, _, e := net.SplitHostPort(r.Host); e == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" || AllowSource(host, c) {
		return true
	}
	for _, allowed := range strings.Split(os.Getenv("ADMIN_HOSTS"), ",") {
		if allowed != "" && strings.EqualFold(strings.TrimSpace(allowed), host) {
			return true
		}
	}
	return false
}
