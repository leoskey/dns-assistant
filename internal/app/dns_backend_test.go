package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type testDNSBackend struct {
	DNSBackend
	apply func(context.Context, Policy) error
}

func (d *testDNSBackend) Apply(ctx context.Context, policy Policy) error {
	if d.apply != nil {
		return d.apply(ctx, policy)
	}
	if d.DNSBackend != nil {
		return d.DNSBackend.Apply(ctx, policy)
	}
	return errors.New("DNS unavailable")
}

func (d *testDNSBackend) Check(ctx context.Context, domain string) (DNSCheck, error) {
	if d.DNSBackend != nil {
		return d.DNSBackend.Check(ctx, domain)
	}
	return DNSCheck{}, errors.New("DNS unavailable")
}

func TestDirectDNSHealthAndQuery(t *testing.T) {
	d, _ := startDNS(t)
	m := testManager(t)
	m.dns = d
	handler := NewAPI(m).Handler()
	for _, route := range []string{"/healthz", "/api/v1/check"} {
		r := httptest.NewRequest("GET", route, nil)
		if route != "/healthz" {
			r = httptest.NewRequest("POST", route, strings.NewReader(`{"domain":"health.dns-assistant.invalid","type":"A"}`))
			r.Header.Set("X-DNS-Request", "1")
		}
		r.RemoteAddr, r.Host = "127.0.0.1:1234", "localhost"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
		if route != "/healthz" && !strings.Contains(w.Body.String(), "127.0.0.1") {
			t.Fatal("check did not query the DNS listener")
		}
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/healthz", nil)
	r.RemoteAddr, r.Host = "127.0.0.1:1234", "localhost"
	handler.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("stopped DNS reported healthy: %d", w.Code)
	}
}

func TestDirectPublishFailureAndRecovery(t *testing.T) {
	d, _ := startDNS(t)
	m := testManager(t)
	m.config = d.policy.Load().Config
	m.version = 5
	m.dns = &testDNSBackend{apply: func(context.Context, Policy) error { return errors.New("disk unavailable") }}
	m.Publish(context.Background())
	if m.applied != 0 || !m.lastSync.IsZero() {
		t.Fatal("failed publication marked as applied")
	}
	m.dns = d
	m.Publish(context.Background())
	if m.applied != 5 || m.lastSync.IsZero() || d.policy.Load().Version != 5 {
		t.Fatal("publication did not recover")
	}
}
