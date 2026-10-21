package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// Only persisted settings and format-1 backups accept the retired budget fields.
// Normal configuration writes continue to reject unknown fields.
func upgradeConfig(raw []byte) (Config, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Config{}, err
	}
	for _, key := range []string{"upstreams", "allowed_cidrs", "rules", "probe_seconds"} {
		if _, ok := fields[key]; !ok {
			return Config{}, errors.New("配置缺少必需字段: " + key)
		}
	}
	if fields == nil {
		return Config{}, errors.New("配置不能为空")
	}
	_, legacy := fields["probe_bytes"]
	if _, ok := fields["daily_bytes"]; ok {
		legacy = true
	}
	delete(fields, "probe_bytes")
	delete(fields, "daily_bytes")
	data, _ := json.Marshal(fields)
	c := DefaultConfig()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Config{}, errors.New("配置格式无效")
	}
	if legacy {
		upstreams := []string{}
		for _, v := range c.Upstreams {
			host, _, _ := net.SplitHostPort(v)
			if strings.HasPrefix(v, "https://") {
				if u, e := url.Parse(v); e == nil {
					host = u.Hostname()
				}
			}
			if ip, e := netip.ParseAddr(host); e == nil && !ip.Is4() {
				continue
			}
			upstreams = append(upstreams, v)
		}
		if len(upstreams) == 0 {
			upstreams = DefaultConfig().Upstreams
		}
		c.Upstreams = upstreams
		c.Improvement = 0.05
		c.CooldownSeconds = 300
		c.IntervalSeconds = max(c.IntervalSeconds, 86400)
		c.HealthSeconds = max(c.HealthSeconds, 1800)
		c.PolicySeconds = max(c.PolicySeconds, c.HealthSeconds*3+600)
		cidrs := []string{}
		for _, v := range c.AllowedCIDRs {
			if p, e := netip.ParsePrefix(v); e == nil && p.Addr().Is4() {
				cidrs = append(cidrs, v)
			}
		}
		c.AllowedCIDRs = cidrs
		for i := range c.Rules {
			ips := []string{}
			for _, v := range c.Rules[i].Candidates {
				if PublicIP(v) {
					ips = append(ips, v)
				}
			}
			c.Rules[i].Candidates = ips
		}
	}
	return c, c.Validate()
}
