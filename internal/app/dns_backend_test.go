package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testDNSBackend struct {
	DNSBackend
	apply func(context.Context, Policy) error
}

func TestShorterPolicyLifetimeKeepsRetainedNodesPublishable(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "fixed"}[fixed], func(t *testing.T) {
			m := testManager(t)
			c := m.Config()
			rule := c.Rules[0]
			originalExpiry := time.Now().Add(time.Duration(c.PolicySeconds) * time.Second)
			m.selections[key(rule.Domain, "A")] = Selection{RuleID: rule.ID, Domain: rule.Domain, Family: "A", IP: "8.8.8.8", Fixed: fixed, ValidUntil: originalExpiry}
			c.PolicySeconds = 6000
			before := time.Now()
			if err := m.SetConfig(c); err != nil {
				t.Fatal(err)
			}
			retained := m.selections[key(rule.Domain, "A")]
			if retained.IP != "8.8.8.8" || retained.Fixed != fixed || !retained.ValidUntil.After(before) || retained.ValidUntil.After(time.Now().Add(6000*time.Second)) {
				t.Fatalf("retained node has invalid lifetime: %+v", retained)
			}
			var persisted map[string]Selection
			if err := m.store.Get("selections", &persisted); err != nil {
				t.Fatal(err)
			}
			if !persisted[key(rule.Domain, "A")].ValidUntil.Equal(retained.ValidUntil) {
				t.Fatal("clamped expiry was not persisted")
			}
			d := NewDNS("127.0.0.1:5353", t.TempDir(), nil)
			m.dns = &testDNSBackend{apply: func(_ context.Context, p Policy) error { return d.validatePolicy(p) }}
			m.Publish(context.Background())
			if m.applied != m.version || m.lastSync.IsZero() {
				t.Fatal("shortened policy was not applied")
			}
			c.PolicySeconds = 7200
			if err := m.SetConfig(c); err != nil {
				t.Fatal(err)
			}
			if !m.selections[key(rule.Domain, "A")].ValidUntil.Equal(retained.ValidUntil) {
				t.Fatal("increasing lifetime extended an existing selection without a health check")
			}
		})
	}
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
