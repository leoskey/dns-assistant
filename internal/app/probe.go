package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

type Prober struct {
	// Dial is overridden only by controlled local tests; production always validates public IPs.
	dial        func(context.Context, string, string) (net.Conn, error)
	allowTestIP bool
}

func (p Prober) Probe(ctx context.Context, r Rule, ip string, c Config, kind string) Result {
	out := Result{RuleID: r.ID, Domain: r.Domain, IP: ip, Family: Family(ip), Kind: kind, Time: time.Now().UTC(), SampleURL: r.URL, Profile: probeProfile(r, c)}
	if !PublicIP(ip) && !p.allowTestIP {
		out.Error = "拒绝非公网候选地址"
		return out
	}
	limit := c.ProbeBytes
	if kind == "health" {
		limit = 65536
	}
	timeout := time.Duration(c.ProbeSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dial := p.dial
	if dial == nil {
		d := &net.Dialer{Timeout: timeout}
		dial = d.DialContext
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ResponseHeaderTimeout: timeout, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil || host != r.Domain {
			return nil, errors.New("探测目标发生变化")
		}
		return dial(ctx, network, net.JoinHostPort(ip, port))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 2 || req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme || req.URL.Path != via[0].URL.Path {
			return errors.New("测试地址跳转超出允许范围")
		}
		return nil
	}}
	req, e := http.NewRequestWithContext(ctx, "GET", r.URL, nil)
	if e != nil {
		out.Error = e.Error()
		return out
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", limit-1))
	req.Header.Set("Accept-Encoding", "identity")
	agent := "DNSAssistant/1.0"
	if r.Platform == "Switch" {
		agent = "DNSAssistant/Nintendo NX"
	}
	req.Header.Set("User-Agent", agent)
	started := time.Now()
	resp, e := client.Do(req)
	if e != nil {
		out.Error = e.Error()
		return out
	}
	defer resp.Body.Close()
	fail := func(msg string) Result {
		out.DurationMS = time.Since(started).Milliseconds()
		out.Error = msg
		return out
	}
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return fail(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	if resp.StatusCode == 206 {
		var start, end, total int64
		if _, e = fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); e != nil || start != 0 || end < start || end >= total || end >= limit {
			return fail("Content-Range 无效")
		}
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "html") || strings.Contains(ct, "json") || strings.Contains(ct, "xml") {
		return fail("返回了页面或错误内容")
	}
	buf := make([]byte, 32768)
	prefix := []byte{}
	var readErr error
	for out.Bytes < limit {
		n, e := resp.Body.Read(buf[:min(int64(len(buf)), limit-out.Bytes)])
		out.Bytes += int64(n)
		if len(prefix) < 1024 {
			prefix = append(prefix, buf[:min(n, 1024-len(prefix))]...)
		}
		if e != nil {
			readErr = e
			break
		}
		if n == 0 {
			select {
			case <-ctx.Done():
				readErr = ctx.Err()
			default:
			}
			if readErr != nil {
				break
			}
		}
	}
	out.DurationMS = max(1, time.Since(started).Milliseconds())
	minimum := min(r.MinBytes, limit)
	windowEnded := errors.Is(ctx.Err(), context.DeadlineExceeded) && out.Bytes >= minimum
	if errors.Is(ctx.Err(), context.Canceled) {
		return fail("探测已取消")
	}
	if ctx.Err() != nil && out.Bytes < minimum {
		return fail("探测超时或取消")
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) && out.Bytes < limit && !windowEnded {
		return fail("读取失败: " + readErr.Error())
	}
	if out.Bytes < minimum {
		return fail("有效内容不足 " + strconv.FormatInt(r.MinBytes, 10) + " 字节")
	}
	text := strings.ToLower(strings.TrimSpace(string(prefix)))
	if strings.HasPrefix(text, "<!doctype") || strings.HasPrefix(text, "<html") || strings.HasPrefix(text, "<?xml") {
		return fail("内容看起来是错误页面")
	}
	if r.ExpectedPrefix != "" && !strings.HasPrefix(string(prefix), r.ExpectedPrefix) {
		return fail("内容前缀校验失败")
	}
	if resp.StatusCode == 206 && resp.ContentLength >= 0 && out.Bytes < min(resp.ContentLength, limit) && !windowEnded {
		return fail("分段内容未完整读取")
	}
	out.OK = true
	out.Speed = float64(out.Bytes) * 8 / (float64(out.DurationMS) / 1000 * 1e6)
	return out
}
func AllowSource(addr string, c Config) bool {
	h, _, e := net.SplitHostPort(addr)
	if e != nil {
		h = addr
	}
	ip, e := netip.ParseAddr(h)
	if e != nil {
		return false
	}
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return true
	}
	for _, cidr := range c.AllowedCIDRs {
		p, e := netip.ParsePrefix(cidr)
		if e == nil && p.Contains(ip) {
			return true
		}
	}
	return false
}
