package app

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

type Rule struct {
	ID             string   `json:"id"`
	Platform       string   `json:"platform"`
	Domain         string   `json:"domain"`
	URL            string   `json:"url"`
	Enabled        bool     `json:"enabled"`
	Candidates     []string `json:"candidates"`
	MinBytes       int64    `json:"min_bytes"`
	ExpectedPrefix string   `json:"expected_prefix,omitempty"`
}
type Config struct {
	Enabled         bool     `json:"enabled"`
	ProxyEnabled    bool     `json:"proxy_enabled"`
	Upstreams       []string `json:"upstreams"`
	AllowedCIDRs    []string `json:"allowed_cidrs"`
	ProbeSeconds    int      `json:"probe_seconds"`
	IntervalSeconds int      `json:"interval_seconds"`
	HealthSeconds   int      `json:"health_seconds"`
	PolicySeconds   int      `json:"policy_seconds"`
	TTL             uint32   `json:"ttl"`
	Improvement     float64  `json:"improvement"`
	CooldownSeconds int      `json:"cooldown_seconds"`
	Timezone        string   `json:"timezone"`
	Rules           []Rule   `json:"rules"`
	RuleVersion     string   `json:"rule_version"`
	RuleSource      string   `json:"rule_source"`
	FeedURL         string   `json:"feed_url"`
	FeedSeconds     int      `json:"feed_seconds"`
}

//go:embed download-rules.json
var builtinRules []byte

func DefaultConfig() Config {
	c := Config{Enabled: true, Upstreams: []string{"223.5.5.5:53", "119.29.29.29:53"}, AllowedCIDRs: []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}, ProbeSeconds: 5, IntervalSeconds: 86400, HealthSeconds: 1800, PolicySeconds: 7200, FeedSeconds: 21600, TTL: 60, Improvement: 0.05, CooldownSeconds: 300, Timezone: "Asia/Shanghai", RuleSource: "builtin"}
	feed, err := parseRuleFeed(builtinRules)
	if err != nil {
		panic("内置下载规则无效: " + err.Error())
	}
	c.RuleVersion = feed.Version
	c.Rules = feed.Rules
	return c
}
func PublicIP(s string) bool {
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	if !ip.Is4() {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4"} {
		if netip.MustParsePrefix(s).Contains(ip) {
			return false
		}
	}
	return true
}
func DomainValid(s string) bool {
	if len(s) > 253 || s != strings.ToLower(s) || !strings.Contains(s, ".") || strings.HasSuffix(s, ".") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func (c Config) Validate() error {
	if len(c.Upstreams) < 1 || len(c.Upstreams) > 5 {
		return errors.New("上游 DNS 数量须为 1–5")
	}
	for _, s := range c.Upstreams {
		if strings.HasPrefix(s, "https://") {
			u, e := url.Parse(s)
			if e != nil || u.User != nil || u.Fragment != "" || u.Hostname() == "" {
				return errors.New("DoH 上游地址无效")
			}
			if ip, e := netip.ParseAddr(u.Hostname()); e == nil && !ip.Is4() {
				return errors.New("DoH 上游不能使用 IPv6 地址")
			}
			continue
		}
		host, port, e := net.SplitHostPort(s)
		if e != nil || port == "" {
			return fmt.Errorf("上游须为 IP:端口 或 https:// 地址: %s", s)
		}
		ip, e := netip.ParseAddr(host)
		if e != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsLoopback() || host == deploymentIP() {
			return errors.New("上游必须是明确的非环回 IP，不能指向服务自身")
		}
		if port != "53" && port != "5353" {
			return errors.New("普通上游仅支持 53 或 5353")
		}
	}
	if len(c.AllowedCIDRs) == 0 || len(c.AllowedCIDRs) > 32 {
		return errors.New("请设置允许访问的局域网网段")
	}
	for _, s := range c.AllowedCIDRs {
		p, e := netip.ParsePrefix(s)
		if e != nil || !p.Addr().Is4() || p.Bits() == 0 {
			return errors.New("访问网段无效，不能开放所有地址")
		}
	}
	if c.ProbeSeconds < 1 || c.ProbeSeconds > 15 {
		return errors.New("测速时间须为 1–15 秒")
	}
	if c.IntervalSeconds < 3600 || c.IntervalSeconds > 604800 || c.HealthSeconds < 900 || c.HealthSeconds > 86400 || c.PolicySeconds < c.HealthSeconds*2+600 || c.PolicySeconds > 604800 || c.TTL < 5 || c.TTL > 300 || c.Improvement < 0.01 || c.Improvement > 1 || c.CooldownSeconds < 0 || c.CooldownSeconds > 86400 {
		return errors.New("调度或切换参数超出范围")
	}
	if c.FeedSeconds < 3600 || c.FeedSeconds > 604800 {
		return errors.New("规则同步间隔须为 1 小时至 7 天")
	}
	if c.FeedURL != "" {
		if err := validateFeedURL(c.FeedURL); err != nil {
			return err
		}
	}
	if _, e := time.LoadLocation(c.Timezone); e != nil {
		return errors.New("时区无效")
	}
	if len(c.Rules) > 100 || len(c.RuleSource) == 0 || len(c.RuleVersion) == 0 || len(c.RuleSource) > 2000 || len(c.RuleVersion) > 100 {
		return errors.New("规则数量或来源过长")
	}
	seen := map[string]bool{}
	domains := map[string]bool{}
	for _, r := range c.Rules {
		if len(r.ID) < 1 || len(r.ID) > 64 || !DomainValid(r.Domain) || seen[r.ID] || domains[r.Domain] {
			return errors.New("规则 ID／域名无效或重复")
		}
		for _, ch := range r.ID {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return errors.New("规则 ID 只能包含小写字母、数字和连字符")
			}
		}
		seen[r.ID] = true
		domains[r.Domain] = true
		if r.Platform != "Xbox" && r.Platform != "PlayStation" && r.Platform != "Switch" {
			return errors.New("平台须为 Xbox、PlayStation 或 Switch")
		}
		u, e := url.Parse(r.URL)
		if e != nil || !(u.Scheme == "http" || u.Scheme == "https") || u.Hostname() != r.Domain || u.User != nil || u.Fragment != "" || u.Port() != "" && u.Port() != "80" && u.Port() != "443" {
			return errors.New("测试 URL 须使用规则本身的域名及标准 HTTP(S) 端口")
		}
		// Restrict probes to download hosts, avoiding login, payment and arbitrary destinations.
		allowed := false
		for _, suffix := range []string{".xboxlive.com", ".xboxlive.cn", ".dl.playstation.net", ".cdn.nintendo.net", ".d4c.nintendo.net", ".nemo.srv.nintendo.net"} {
			if strings.HasSuffix(r.Domain, suffix) {
				allowed = true
			}
		}
		if !allowed {
			return errors.New("规则域名不在支持的三平台下载域名范围内")
		}
		if r.MinBytes < 1024 || r.MinBytes > 64<<20 || len(r.ExpectedPrefix) > 1024 {
			return errors.New("内容检查参数无效")
		}
		if len(r.Candidates) > 64 {
			return errors.New("每条规则最多 64 个手动候选")
		}
		for _, ip := range r.Candidates {
			if !PublicIP(ip) {
				return errors.New("候选必须为公网 IPv4，不能使用内网或特殊地址")
			}
		}
	}
	return nil
}

type Result struct {
	ID            int64     `json:"id"`
	RuleID        string    `json:"rule_id"`
	Domain        string    `json:"domain"`
	SampleURL     string    `json:"sample_url"`
	Profile       string    `json:"profile"`
	IP            string    `json:"ip"`
	Family        string    `json:"family"`
	Kind          string    `json:"kind"`
	Time          time.Time `json:"time"`
	Bytes         int64     `json:"bytes"`
	DurationMS    int64     `json:"duration_ms"`
	Speed         float64   `json:"speed_mbps"`
	MeasuredBytes int64     `json:"measured_bytes"`
	MeasuredMS    int64     `json:"measured_ms"`
	OK            bool      `json:"ok"`
	Error         string    `json:"error,omitempty"`
}

func Family(ip string) string {
	a, e := netip.ParseAddr(ip)
	if e == nil && a.Is4() {
		return "A"
	}
	return ""
}

type Selection struct {
	RuleID     string    `json:"rule_id"`
	Domain     string    `json:"domain"`
	Family     string    `json:"family"`
	IP         string    `json:"ip"`
	Speed      float64   `json:"speed_mbps"`
	ValidUntil time.Time `json:"valid_until"`
	ChangedAt  time.Time `json:"changed_at"`
	Reason     string    `json:"reason"`
	Fixed      bool      `json:"fixed"`
	CheckedAt  time.Time `json:"checked_at"`
}

func key(domain, family string) string { return domain + "/" + family }

type Policy struct {
	Version    uint64      `json:"version"`
	CreatedAt  time.Time   `json:"created_at"`
	Config     Config      `json:"config"`
	Selections []Selection `json:"selections"`
}

func AtomicJSON(path string, v any) error {
	data, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	tmp := path + ".tmp"
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(data); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(tmp)
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		os.Remove(tmp)
	}
	return e
}

func probeProfile(r Rule, c Config) string {
	data, _ := json.Marshal(struct {
		URL, Prefix string
		Min         int64
		Seconds     int
		Upstreams   []string
	}{r.URL, r.ExpectedPrefix, r.MinBytes, c.ProbeSeconds, c.Upstreams})
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
