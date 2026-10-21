package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type FeedStatus struct {
	URL     string    `json:"url"`
	Attempt time.Time `json:"attempt"`
	Success time.Time `json:"success"`
	Error   string    `json:"error,omitempty"`
	Version string    `json:"version"`
}
type RuleFeed struct {
	Version string `json:"version"`
	Rules   []Rule `json:"rules"`
}

func validateFeedURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Port() != "" && u.Port() != "443" || len(raw) > 2000 {
		return errors.New("规则表须为标准 HTTPS 地址，不能包含账号或片段")
	}
	host := strings.ToLower(u.Hostname())
	if ip, e := netip.ParseAddr(host); e == nil {
		if !PublicIP(ip.String()) {
			return errors.New("规则表必须位于公网 IPv4 地址")
		}
	} else if host == "localhost" || !strings.Contains(host, ".") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") {
		return errors.New("规则表不能使用本地地址")
	}
	return nil
}
func newFeedClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("规则表不允许重定向，请填写最终 HTTPS 地址")
	}, Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DisableCompression: true, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		if e != nil {
			return nil, e
		}
		if len(ips) == 0 {
			return nil, errors.New("规则表没有可用 IPv4 地址")
		}
		for _, ip := range ips {
			if !PublicIP(ip.String()) {
				return nil, errors.New("规则表解析到非公网地址")
			}
		}
		var last error
		for _, ip := range ips {
			conn, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}}}
}
func (m *Manager) SyncFeed(ctx context.Context) error {
	if !m.feedMu.TryLock() {
		return errors.New("规则表正在同步")
	}
	defer m.feedMu.Unlock()
	c := m.Config()
	if c.FeedURL == "" {
		return errors.New("请先保存规则表 URL")
	}
	if err := validateFeedURL(c.FeedURL); err != nil {
		return err
	}
	m.mu.RLock()
	generation := m.generation
	state := m.feed
	m.mu.RUnlock()
	state.URL = c.FeedURL
	state.Attempt = time.Now().UTC()
	state.Error = ""
	fail := func(err error) error {
		state.Error = err.Error()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.generation == generation {
			if e := m.store.Set("feed", state); e != nil {
				state.Error += "；同步状态保存失败: " + e.Error()
			}
			m.feed = state
			m.signal()
		}
		return err
	}
	client := m.feedClient
	if client == nil {
		client = newFeedClient()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.FeedURL, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fail(fmt.Errorf("规则表 HTTP %d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return fail(err)
	}
	if len(data) > 1<<20 {
		return fail(errors.New("规则表超过 1 MiB"))
	}
	feed, err := parseRuleFeed(data)
	if err != nil {
		return fail(err)
	}
	c.Rules = feed.Rules
	c.RuleSource = c.FeedURL
	c.RuleVersion = feed.Version
	if err = c.Validate(); err != nil {
		return fail(err)
	}
	state.Success = time.Now().UTC()
	state.Version = feed.Version
	state.Error = ""
	if err = m.setConfig(c, &state, generation); err != nil {
		return fail(err)
	}
	m.store.Event("规则表已同步：" + feed.Version)
	return nil
}
func (m *Manager) feedLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		c := m.Config()
		m.mu.RLock()
		last := m.feed.Attempt
		m.mu.RUnlock()
		if c.FeedURL != "" && time.Since(last) >= time.Duration(c.FeedSeconds)*time.Second {
			m.SyncFeed(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func parseRuleFeed(data []byte) (RuleFeed, error) {
	var feed RuleFeed
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&feed); err != nil {
		return RuleFeed{}, fmt.Errorf("规则表格式无效: %w", err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return RuleFeed{}, errors.New("规则表只能包含一个 JSON 对象")
	}
	if feed.Version == "" || len(feed.Version) > 100 || len(feed.Rules) == 0 || len(feed.Rules) > 100 {
		return RuleFeed{}, errors.New("规则表需要版本号及 1–100 条下载规则")
	}
	var fields struct {
		Rules []map[string]json.RawMessage `json:"rules"`
	}
	json.Unmarshal(data, &fields)
	for i := range feed.Rules {
		r := &feed.Rules[i]
		if _, exists := fields.Rules[i]["enabled"]; !exists {
			r.Enabled = true
		}
		if r.Domain == "" {
			if u, e := url.Parse(r.URL); e == nil {
				r.Domain = strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
			}
		}
		if r.ID == "" {
			sum := sha256.Sum256([]byte(r.Domain))
			r.ID = fmt.Sprintf("download-%x", sum[:16])
		}
		if r.MinBytes == 0 {
			r.MinBytes = 65536
		}
		if r.Candidates == nil {
			r.Candidates = []string{}
		}
	}
	return feed, nil
}
