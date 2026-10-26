package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestNintendoDownloadAdmission(t *testing.T) {
	for _, domain := range []string{
		"ctest-dl-lp1.cdn.nintendo.net",
		"atum.hac.lp1.d4c.nintendo.net",
		"atum-4ff.hac.lp1.d4c.nintendo.net",
		"atum-eda.hac.lp1.d4c.nintendo.net",
		"atum-5a8.hac.lp1.d4c.nintendo.net",
		"atum-f03.hac.lp1.d4c.nintendo.net",
		"atumn.hac.lp1.d4c.nintendo.net",
		"atum.p01.lp1.d4c.srv.nintendo.net",
		"atumn.p01.lp1.d4c.srv.nintendo.net",
		"nemof.hac.lp1.nemo.srv.nintendo.net",
		"nemof.p01.lp1.nemo.srv.nintendo.net",
	} {
		t.Run(domain, func(t *testing.T) {
			c := DefaultConfig()
			c.Rules = []Rule{{ID: "sample", Platform: "Switch", Domain: domain, URL: "https://" + domain + "/sample", MinBytes: 65536}}
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			c.Rules[0].URL = "http://ctest-dl-lp1.cdn.nintendo.net/30m"
			if domain != "ctest-dl-lp1.cdn.nintendo.net" && c.Validate() == nil {
				t.Fatal("connection-test sample reused for another host")
			}
		})
	}
	for _, domain := range []string{
		"accounts.nintendo.com", "api.accounts.nintendo.com",
		"ecs-lp1.hac.shop.nintendo.net", "ias-lp1.hac.shop.nintendo.net",
		"sun.p01.lp1.d4c.srv.nintendo.net", "superfly.p01.lp1.d4c.srv.nintendo.net",
		"receive-lp1.dg.srv.nintendo.net", "aqua.p01.lp1.d4c.srv.nintendo.net",
		"unknown.p01.lp1.d4c.srv.nintendo.net", "sub.atum.p01.lp1.d4c.srv.nintendo.net",
		"atum.hac.lp1.d4c.nintendo.net.example.com",
	} {
		t.Run("reject/"+domain, func(t *testing.T) {
			c := DefaultConfig()
			c.Rules = []Rule{{ID: "sample", Platform: "Switch", Domain: domain, URL: "https://" + domain + "/sample", MinBytes: 65536}}
			if c.Validate() == nil {
				t.Fatal("non-download or unrelated host admitted")
			}
		})
	}
}

func TestNintendoCustomRuleCompatibility(t *testing.T) {
	m := testManager(t)
	for _, domain := range []string{
		"custom.cdn.nintendo.net", "custom.d4c.nintendo.net",
		"custom.nemo.srv.nintendo.net", "sub.atum.hac.lp1.d4c.nintendo.net",
	} {
		t.Run(domain, func(t *testing.T) {
			c := DefaultConfig()
			c.Rules = []Rule{{ID: "custom", Platform: "Switch", Domain: domain, URL: "https://" + domain + "/owned-sample", Enabled: true, MinBytes: 65536}}
			if err := c.Validate(); err != nil {
				t.Fatal("previously accepted custom rule rejected", err)
			}
			feedJSON, err := json.Marshal(RuleFeed{Version: "custom", Rules: c.Rules})
			if err != nil {
				t.Fatal(err)
			}
			feed, err := parseRuleFeed(feedJSON)
			if err != nil {
				t.Fatal("custom subscription incompatible", err)
			}
			c.Rules = feed.Rules
			if err := c.Validate(); err != nil {
				t.Fatal("custom subscription rule rejected", err)
			}
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := upgradeConfig(raw)
			if err != nil || restored.Rules[0].URL != c.Rules[0].URL {
				t.Fatal("persisted config/backup incompatible", err)
			}
			if err := m.SetConfig(restored); err != nil {
				t.Fatal(err)
			}
			restarted, err := NewManager(m.store, m.dnsURL, m.token)
			if err != nil || restarted.Config().Rules[0].Domain != domain {
				t.Fatal("custom rule prevents restart", err)
			}
		})
	}
}

func TestNintendoCandidatesAreNotBuiltinActiveRules(t *testing.T) {
	c := DefaultConfig()
	var count int
	for _, r := range c.Rules {
		if r.Platform != "Switch" {
			continue
		}
		count++
		if r.Domain != "ctest-dl-lp1.cdn.nintendo.net" || r.URL != "http://ctest-dl-lp1.cdn.nintendo.net/30m" {
			t.Fatalf("unverified Nintendo sample activated: %s", r.Domain)
		}
	}
	if count != 1 {
		t.Fatalf("expected original connection test, got %d", count)
	}
}

func TestSelectionRejectsDNSAndHealthMeasurements(t *testing.T) {
	c := DefaultConfig()
	fastDNS := result("1.1.1.1", 10000, true)
	fastDNS.Kind = "dns"
	health := result("8.8.8.8", 10000, true)
	health.Kind = "health"
	throughput := result("9.9.9.9", 10, true)
	selected := Choose(Selection{}, []Result{fastDNS, health, throughput}, c, time.Now())
	if selected.IP != throughput.IP {
		t.Fatal("DNS/health measurement outranked sustained download", selected)
	}
}

func TestNintendoContentSelectionsStayPerDomain(t *testing.T) {
	m := testManager(t)
	c := m.Config()
	c.Rules = []Rule{
		{ID: "switch-content", Platform: "Switch", Domain: "atum.p01.lp1.d4c.srv.nintendo.net", URL: "http://atum.p01.lp1.d4c.srv.nintendo.net/sample", Enabled: true, MinBytes: 65536},
		{ID: "switch-update", Platform: "Switch", Domain: "atumn.p01.lp1.d4c.srv.nintendo.net", URL: "http://atumn.p01.lp1.d4c.srv.nintendo.net/sample", Enabled: true, MinBytes: 65536},
	}
	if err := m.SetConfig(c); err != nil {
		t.Fatal(err)
	}
	m.resolve = func(context.Context, string, Config) []string { return []string{"9.9.9.9"} }
	m.prober, _ = probeFixture(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Host == c.Rules[0].Domain && req.URL.Path == "/sample" {
			streamSample(w, req)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	})
	first, second := c.Rules[0], c.Rules[1]
	m.execute(context.Background(), Job{RuleID: first.ID, Kind: "throughput"}, first, c)
	if got := m.selections[key(first.Domain, "A")]; got.IP != "9.9.9.9" || !got.ValidUntil.After(time.Now()) {
		t.Fatal("valid content sample did not select its own candidate", got)
	}
	if _, exists := m.selections[key(second.Domain, "A")]; exists {
		t.Fatal("untested domain inherited another domain's selection")
	}
	m.execute(context.Background(), Job{RuleID: second.ID, Kind: "throughput"}, second, c)
	if got := m.selections[key(second.Domain, "A")]; got.IP != "" || got.ValidUntil.After(time.Now()) {
		t.Fatal("HTTP failure inherited successful sample on same IP", got)
	}
	history, err := m.store.History(10)
	if err != nil || len(history) != 2 {
		t.Fatal("missing independent probe records", len(history), err)
	}
	for _, result := range history {
		if result.OK != (result.Domain == first.Domain) {
			t.Fatal("probe outcome associated with wrong domain", result)
		}
	}
}
