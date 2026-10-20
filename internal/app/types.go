package app

import (
	"crypto/sha256"
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
	Upstreams       []string `json:"upstreams"`
	AllowedCIDRs    []string `json:"allowed_cidrs"`
	ProbeSeconds    int      `json:"probe_seconds"`
	ProbeBytes      int64    `json:"probe_bytes"`
	DailyBytes      int64    `json:"daily_bytes"`
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
}

func DefaultConfig() Config {
	c := Config{Enabled: true, Upstreams: []string{"223.5.5.5:53", "119.29.29.29:53"}, AllowedCIDRs: []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128", "fc00::/7"}, ProbeSeconds: 3, ProbeBytes: 8 << 20, DailyBytes: 512 << 20, IntervalSeconds: 1800, HealthSeconds: 120, PolicySeconds: 600, TTL: 60, Improvement: 0.15, CooldownSeconds: 600, Timezone: "Asia/Shanghai", RuleVersion: "2026-10-20", RuleSource: "builtin", Rules: []Rule{
		{ID: "xbox-global", Platform: "Xbox", Domain: "xvcf1.xboxlive.com", URL: "http://xvcf1.xboxlive.com/Z/routing/extraextralarge.txt", Enabled: true, Candidates: []string{}, MinBytes: 65536},
		{ID: "playstation", Platform: "PlayStation", Domain: "gst.prod.dl.playstation.net", URL: "http://gst.prod.dl.playstation.net/networktest/get_192m", Enabled: true, Candidates: []string{}, MinBytes: 65536},
		{ID: "switch", Platform: "Switch", Domain: "ctest-dl-lp1.cdn.nintendo.net", URL: "http://ctest-dl-lp1.cdn.nintendo.net/30m", Enabled: true, Candidates: []string{}, MinBytes: 65536},
	}}
	for i, domain := range []string{"assets1.xboxlive.com", "assets2.xboxlive.com", "xvcf2.xboxlive.com", "d1.xboxlive.com", "d2.xboxlive.com", "assets1.xboxlive.cn", "assets2.xboxlive.cn", "d1.xboxlive.cn", "d2.xboxlive.cn"} {
		c.Rules = append(c.Rules, Rule{ID: fmt.Sprintf("xbox-content-%d", i+1), Platform: "Xbox", Domain: domain, URL: "http://" + domain + "/Z/routing/extraextralarge.txt", Enabled: true, Candidates: []string{}, MinBytes: 65536})
	}
	c.Rules = append(c.Rules, Rule{ID: "ps-games", Platform: "PlayStation", Domain: "gs2.ww.prod.dl.playstation.net", URL: "http://gs2.ww.prod.dl.playstation.net/gs2/appkgo/prod/CUSA03962_00/4/f_526a2fab32d369a8ca6298b59686bf823fa9edfe95acb85bc140c27f810842ce/f/UP0102-CUSA03962_00-BH70000000000001_0.pkg", Enabled: true, Candidates: []string{}, MinBytes: 65536})
	return c
}
func PublicIP(s string) bool {
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	if ip.Is4In6() {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96"} {
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
			continue
		}
		host, port, e := net.SplitHostPort(s)
		if e != nil || port == "" {
			return fmt.Errorf("上游须为 IP:端口 或 https:// 地址: %s", s)
		}
		ip, e := netip.ParseAddr(host)
		if e != nil || ip.IsUnspecified() || ip.IsLoopback() || host == os.Getenv("NAS_IP") {
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
		if e != nil || p.Bits() == 0 {
			return errors.New("访问网段无效，不能开放所有地址")
		}
	}
	if c.ProbeSeconds < 1 || c.ProbeSeconds > 15 || c.ProbeBytes < 65536 || c.ProbeBytes > 64<<20 || c.DailyBytes < 1<<20 || c.DailyBytes > 100<<30 {
		return errors.New("测速时间或流量预算超出范围")
	}
	if c.IntervalSeconds < 30 || c.IntervalSeconds > 86400 || c.HealthSeconds < 10 || c.HealthSeconds > 3600 || c.PolicySeconds < c.HealthSeconds*2 || c.PolicySeconds > 86400 || c.TTL < 5 || c.TTL > 300 || c.Improvement < 0.05 || c.Improvement > 1 || c.CooldownSeconds < 0 || c.CooldownSeconds > 86400 {
		return errors.New("调度或切换参数超出范围")
	}
	if _, e := time.LoadLocation(c.Timezone); e != nil {
		return errors.New("时区无效")
	}
	if len(c.Rules) > 100 || len(c.RuleSource) == 0 || len(c.RuleVersion) == 0 || len(c.RuleSource) > 500 || len(c.RuleVersion) > 100 {
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
		if r.MinBytes < 1024 || r.MinBytes > c.ProbeBytes || len(r.ExpectedPrefix) > 1024 {
			return errors.New("内容检查参数无效")
		}
		if len(r.Candidates) > 64 {
			return errors.New("每条规则最多 64 个手动候选")
		}
		for _, ip := range r.Candidates {
			if !PublicIP(ip) {
				return errors.New("候选必须为公网 IP，不能使用内网或特殊地址")
			}
		}
	}
	return nil
}

type Result struct {
	ID         int64     `json:"id"`
	RuleID     string    `json:"rule_id"`
	Domain     string    `json:"domain"`
	SampleURL  string    `json:"sample_url"`
	Profile    string    `json:"profile"`
	IP         string    `json:"ip"`
	Family     string    `json:"family"`
	Kind       string    `json:"kind"`
	Time       time.Time `json:"time"`
	Bytes      int64     `json:"bytes"`
	DurationMS int64     `json:"duration_ms"`
	Speed      float64   `json:"speed_mbps"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
}

func Family(ip string) string {
	a, e := netip.ParseAddr(ip)
	if e == nil && a.Is4() {
		return "A"
	}
	return "AAAA"
}

type Selection struct {
	RuleID     string    `json:"rule_id"`
	Domain     string    `json:"domain"`
	Family     string    `json:"family"`
	IP         string    `json:"ip"`
	Backup     string    `json:"backup,omitempty"`
	Speed      float64   `json:"speed_mbps"`
	ValidUntil time.Time `json:"valid_until"`
	ChangedAt  time.Time `json:"changed_at"`
	Reason     string    `json:"reason"`
	Fixed      bool      `json:"fixed"`
	Challenger string    `json:"challenger,omitempty"`
	Streak     int       `json:"streak,omitempty"`
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
		Min, Max    int64
		Seconds     int
		Upstreams   []string
	}{r.URL, r.ExpectedPrefix, r.MinBytes, c.ProbeBytes, c.ProbeSeconds, c.Upstreams})
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
