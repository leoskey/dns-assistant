package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type DownloadProxy struct {
	m          *Manager
	forward    *httputil.ReverseProxy
	slots      chan struct{}
	active     atomic.Int64
	downloaded atomic.Int64
	// Only controlled tests replace the final dial; target validation still runs.
	dial func(context.Context, string, string) (net.Conn, error)
}

type ProxyStatus struct {
	Enabled         bool   `json:"enabled"`
	Address         string `json:"address"`
	Port            int    `json:"port"`
	ActiveTransfers int64  `json:"active_transfers"`
	DownloadedBytes int64  `json:"downloaded_bytes"`
}

func newDownloadProxy(m *Manager) *DownloadProxy {
	p := &DownloadProxy{m: m, slots: make(chan struct{}, 64), dial: (&net.Dialer{Timeout: 5 * time.Second}).DialContext}
	p.forward = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.Host = r.In.URL.Host
			r.Out.Header.Del("Proxy-Authorization")
			r.Out.Header.Del("Proxy-Connection")
		},
		Transport:     &http.Transport{Proxy: nil, DialContext: p.connect, DisableCompression: true, MaxIdleConns: 64, IdleConnTimeout: 60 * time.Second, ResponseHeaderTimeout: 15 * time.Second},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "下载目标连接失败", http.StatusBadGateway)
		},
	}
	return p
}

func (p *DownloadProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := p.m.Config()
	if !AllowSource(r.RemoteAddr, c) {
		http.Error(w, "代理仅允许配置的局域网来源", http.StatusForbidden)
		return
	}
	if !c.ProxyEnabled {
		http.Error(w, "下载代理未启用", http.StatusServiceUnavailable)
		return
	}
	if err := validateProxyTarget(r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		http.Error(w, "代理并发已满，请稍后重试", http.StatusServiceUnavailable)
		return
	}
	p.active.Add(1)
	defer p.active.Add(-1)
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}
	p.forward.ServeHTTP(w, r)
}

func validateProxyTarget(r *http.Request) error {
	host, port := "", ""
	if r.Method == http.MethodConnect {
		var err error
		host, port, err = net.SplitHostPort(r.Host)
		if err != nil || port != "443" || r.URL.Host != r.Host || r.URL.Path != "" || r.URL.RawQuery != "" {
			return errors.New("HTTPS 隧道仅支持目标主机:443")
		}
	} else {
		if r.URL.Scheme != "http" || r.URL.Host == "" || r.URL.User != nil || r.URL.Fragment != "" || r.URL.Opaque != "" {
			return errors.New("请使用标准 HTTP 代理请求")
		}
		host, port = r.URL.Hostname(), r.URL.Port()
		if port != "" && port != "80" {
			return errors.New("HTTP 转发仅支持目标端口 80")
		}
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if PublicIP(ip.String()) {
			return nil
		}
	} else if DomainValid(host) {
		return nil
	}
	return errors.New("代理目标必须是公网 IPv4 或有效域名")
}

func (p *DownloadProxy) connect(ctx context.Context, network, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	p.m.mu.RLock()
	c := p.m.config
	selected := ""
	for _, rule := range c.Rules {
		if rule.Enabled && rule.Domain == host {
			s := p.m.selections[key(host, "A")]
			if s.RuleID == rule.ID && s.ValidUntil.After(time.Now()) && (c.Enabled || s.Fixed) {
				selected = s.IP
			}
			break
		}
	}
	p.m.mu.RUnlock()
	if selected != "" {
		if conn, err := p.dial(ctx, "tcp4", net.JoinHostPort(selected, port)); err == nil {
			return &proxyConnection{Conn: conn, downloaded: &p.downloaded}, nil
		}
	}
	ips := []string{host}
	if net.ParseIP(host) == nil {
		ips = p.m.resolve(ctx, host, c)
	}
	for _, ip := range ips {
		if !PublicIP(ip) || ip == selected {
			continue
		}
		if conn, err := p.dial(ctx, "tcp4", net.JoinHostPort(ip, port)); err == nil {
			return &proxyConnection{Conn: conn, downloaded: &p.downloaded}, nil
		}
	}
	return nil, errors.New("目标没有可连接的公网 IPv4")
}

func (p *DownloadProxy) tunnel(w http.ResponseWriter, r *http.Request) {
	upstream, err := p.connect(r.Context(), "tcp4", r.Host)
	if err != nil {
		http.Error(w, "下载目标连接失败", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	client, buffered, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	stop := context.AfterFunc(r.Context(), func() { client.Close(); upstream.Close() })
	defer stop()
	client.SetDeadline(time.Time{})
	if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffered.Flush(); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_, err := io.Copy(upstream, buffered)
		if err != nil {
			upstream.Close()
		} else {
			if tcp, ok := upstream.(*proxyConnection).Conn.(interface{ CloseWrite() error }); ok {
				tcp.CloseWrite()
			} else {
				upstream.Close()
			}
		}
		close(done)
	}()
	io.Copy(client, upstream)
	client.Close()
	upstream.Close()
	<-done
}

type proxyConnection struct {
	net.Conn
	downloaded *atomic.Int64
}

func (c *proxyConnection) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.downloaded.Add(int64(n))
	return n, err
}

func ServeDownloadProxy(ctx context.Context, addr string, p *DownloadProxy) error {
	s := &http.Server{Handler: p, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384, BaseContext: func(net.Listener) context.Context { return ctx }}
	listener, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.Shutdown(shutdown)
		p.forward.Transport.(*http.Transport).CloseIdleConnections()
	}()
	err = s.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func deploymentIP() string {
	value := strings.TrimSpace(os.Getenv("SERVER_IP"))
	if value == "" {
		value = strings.TrimSpace(os.Getenv("NAS_IP"))
	}
	if ip := net.ParseIP(value); ip != nil && ip.To4() != nil && ip.IsGlobalUnicast() && !ip.IsLoopback() {
		return ip.String()
	}
	return ""
}
