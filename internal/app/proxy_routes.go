package app

import (
	"context"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type proxyRoute struct {
	host, selected, state string
	config                Config
}

func (r proxyRoute) key() string {
	return r.state + "|" + r.selected + "|" + strings.Join(r.config.Upstreams, "|")
}

func (p *DownloadProxy) route(host string) proxyRoute {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	p.m.mu.RLock()
	defer p.m.mu.RUnlock()
	r := proxyRoute{host: host, config: p.m.config, state: "unconfigured"}
	for _, rule := range r.config.Rules {
		if rule.Domain != host {
			continue
		}
		r.state = "disabled"
		if !rule.Enabled {
			break
		}
		s := p.m.selections[key(host, "A")]
		r.state = "unavailable"
		if !r.config.Enabled && !s.Fixed {
			r.state = "paused"
			break
		}
		if s.RuleID == rule.ID && s.ValidUntil.After(time.Now()) && PublicIP(s.IP) {
			r.selected, r.state = s.IP, "selected"
		}
		break
	}
	return r
}

type proxyPool struct {
	key       string
	domain    string
	transport *http.Transport
}

// Each pool keeps the route it was created for, including in-flight dials.
type proxyTransport struct {
	p     *DownloadProxy
	mu    sync.Mutex
	pools map[string]proxyPool
}

func (t *proxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	route := t.p.route(req.URL.Hostname())
	host := req.URL.Host
	t.mu.Lock()
	pool, ok := t.pools[host]
	if !ok || pool.key != route.key() {
		if ok {
			pool.transport.CloseIdleConnections()
		}
		if len(t.pools) >= 64 {
			for _, old := range t.pools {
				old.transport.CloseIdleConnections()
			}
			clear(t.pools)
		}
		transport := &http.Transport{Proxy: nil, DisableCompression: true, MaxIdleConns: 8, MaxIdleConnsPerHost: 8, IdleConnTimeout: 60 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return t.p.connectRoute(ctx, addr, route)
		}
		pool = proxyPool{key: route.key(), domain: route.host, transport: transport}
		t.pools[host] = pool
	}
	t.mu.Unlock()
	return pool.transport.RoundTrip(req)
}

func (t *proxyTransport) CloseIdleConnections() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, pool := range t.pools {
		pool.transport.CloseIdleConnections()
	}
	clear(t.pools)
}

type ProxyConnectionStatus struct {
	ID             uint64    `json:"id"`
	Domain         string    `json:"domain"`
	Client         string    `json:"client"`
	IP             string    `json:"ip"`
	State          string    `json:"state"`
	LatestIP       string    `json:"latest_ip"`
	NeedsReconnect bool      `json:"needs_reconnect"`
	StartedAt      time.Time `json:"started_at"`
}

type proxyTransfer struct {
	ProxyConnectionStatus
	routeKey string
	cancel   context.CancelFunc
}

func (p *DownloadProxy) connectionStatuses() []ProxyConnectionStatus {
	p.transfersMu.Lock()
	transfers := make([]proxyTransfer, 0, len(p.transfers))
	for _, transfer := range p.transfers {
		transfers = append(transfers, *transfer)
	}
	p.transfersMu.Unlock()
	statuses := make([]ProxyConnectionStatus, 0, len(transfers))
	for _, transfer := range transfers {
		route := p.route(transfer.Domain)
		s := transfer.ProxyConnectionStatus
		s.LatestIP = route.selected
		s.NeedsReconnect = s.IP != "" && (transfer.routeKey != route.key() || (route.selected != "" && s.IP != route.selected))
		statuses = append(statuses, s)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	return statuses
}

func (p *DownloadProxy) reconnect(domain string) int {
	p.transport.mu.Lock()
	for host, pool := range p.transport.pools {
		if pool.domain == domain {
			pool.transport.CloseIdleConnections()
			delete(p.transport.pools, host)
		}
	}
	p.transport.mu.Unlock()
	p.transfersMu.Lock()
	defer p.transfersMu.Unlock()
	count := 0
	for _, transfer := range p.transfers {
		if transfer.Domain == domain {
			transfer.cancel()
			count++
		}
	}
	return count
}
