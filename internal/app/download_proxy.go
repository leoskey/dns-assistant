package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type DownloadProxy struct {
	m            *Manager
	forward      *httputil.ReverseProxy
	slots        chan struct{}
	active       atomic.Int64
	downloaded   atomic.Int64
	transport    *proxyTransport
	transfersMu  sync.Mutex
	transfers    map[uint64]*proxyTransfer
	nextTransfer uint64
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
	p := &DownloadProxy{m: m, slots: make(chan struct{}, 64), dial: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, transfers: map[uint64]*proxyTransfer{}}
	p.transport = &proxyTransport{p: p, pools: map[string]proxyPool{}}
	p.forward = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.Host = r.In.URL.Host
			r.Out.Header.Del("Proxy-Authorization")
			r.Out.Header.Del("Proxy-Connection")
		},
		Transport:     p.transport,
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
	p.recordRequest(r)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	client, _, _ := net.SplitHostPort(r.RemoteAddr)
	domain := strings.TrimSuffix(strings.ToLower(r.URL.Hostname()), ".")
	p.transfersMu.Lock()
	p.nextTransfer++
	transfer := &proxyTransfer{ProxyConnectionStatus: ProxyConnectionStatus{ID: p.nextTransfer, Domain: domain, Client: client, State: "connecting", StartedAt: time.Now().UTC()}, cancel: cancel}
	p.transfers[transfer.ID] = transfer
	p.transfersMu.Unlock()
	defer func() { p.transfersMu.Lock(); delete(p.transfers, transfer.ID); p.transfersMu.Unlock() }()
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		if conn, ok := info.Conn.(*proxyConnection); ok {
			p.connected(transfer, conn)
		}
	}}
	r = r.WithContext(httptrace.WithClientTrace(ctx, trace))
	p.active.Add(1)
	defer p.active.Add(-1)
	if r.Method == http.MethodConnect {
		p.tunnel(w, r, transfer)
		return
	}
	p.forward.ServeHTTP(w, r)
}

func (p *DownloadProxy) recordRequest(r *http.Request) {
	client, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		client = r.RemoteAddr
	}
	domain, address := r.URL.Hostname(), r.URL.String()
	if r.Method == http.MethodConnect {
		domain, _, _ = net.SplitHostPort(r.Host)
		address = r.Host
	}
	record := Connection{Time: time.Now().UTC(), Client: client, Method: r.Method, Domain: strings.TrimSuffix(strings.ToLower(domain), "."), Address: address}
	if err := p.m.store.AddConnection(record); err != nil {
		slog.Warn("连接记录写入失败", "error", err)
	}
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
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	return p.connectRoute(ctx, addr, p.route(host))
}

func (p *DownloadProxy) connectRoute(ctx context.Context, addr string, route proxyRoute) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	host = route.host
	c, selected := route.config, route.selected
	wrap := func(conn net.Conn, ip, state string) net.Conn {
		return &proxyConnection{Conn: conn, downloaded: &p.downloaded, ip: ip, state: state, routeKey: route.key()}
	}
	if selected != "" {
		if conn, err := p.dial(ctx, "tcp4", net.JoinHostPort(selected, port)); err == nil {
			return wrap(conn, selected, "selected"), nil
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
			state := route.state
			if selected != "" {
				state = "fallback"
			}
			return wrap(conn, ip, state), nil
		}
	}
	return nil, errors.New("目标没有可连接的公网 IPv4")
}

func (p *DownloadProxy) connected(transfer *proxyTransfer, conn *proxyConnection) {
	p.transfersMu.Lock()
	defer p.transfersMu.Unlock()
	transfer.IP, transfer.State, transfer.routeKey = conn.ip, conn.state, conn.routeKey
}

func (p *DownloadProxy) tunnel(w http.ResponseWriter, r *http.Request, transfer *proxyTransfer) {
	upstream, err := p.connect(r.Context(), "tcp4", r.Host)
	if err != nil {
		http.Error(w, "下载目标连接失败", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	p.connected(transfer, upstream.(*proxyConnection))
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
	downloaded          *atomic.Int64
	ip, state, routeKey string
}

func (c *proxyConnection) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.downloaded.Add(int64(n))
	return n, err
}

func ServeDownloadProxy(ctx context.Context, addr string, p *DownloadProxy) error {
	ctx, cancelServer := context.WithCancel(ctx)
	defer cancelServer()
	s := &http.Server{Handler: p, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384, BaseContext: func(net.Listener) context.Context { return ctx }}
	listener, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.Shutdown(shutdown) != nil {
			s.Close()
		}
		p.transport.CloseIdleConnections()
	}()
	err = s.Serve(listener)
	cancelServer()
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func deploymentIP() string {
	value := strings.TrimSpace(os.Getenv("SERVER_IP"))
	if ip := net.ParseIP(value); ip != nil && ip.To4() != nil && ip.IsGlobalUnicast() && !ip.IsLoopback() {
		return ip.String()
	}
	return ""
}

func serviceAddresses() []string {
	if strings.TrimSpace(os.Getenv("SERVER_IP")) != "" {
		if ip := net.ParseIP(deploymentIP()); ip != nil && ip.IsPrivate() {
			return []string{ip.String()}
		}
		return []string{}
	}
	// Bridge interfaces belong to the container, not the host accepting console connections.
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return []string{}
		}
	}
	addrs, _ := net.InterfaceAddrs()
	return lanIPv4s(addrs)
}

func lanIPv4s(addrs []net.Addr) []string {
	ips := []string{}
	seen := map[string]bool{}
	for _, addr := range addrs {
		ip, subnet, err := net.ParseCIDR(addr.String())
		if err != nil || len(subnet.Mask) != net.IPv4len || !ip.IsPrivate() {
			continue
		}
		value := ip.String()
		if !seen[value] {
			ips = append(ips, value)
			seen[value] = true
		}
	}
	return ips
}
