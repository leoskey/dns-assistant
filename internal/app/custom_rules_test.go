package app

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCustomDownloadRuleConfigAPI(t *testing.T) {
	for _, tc := range []struct {
		name, domain, url, candidate string
		want                         int
	}{
		{"switch connection test", "ctest-dl.p01.lp1.ctest.srv.nintendo.net", "http://ctest-dl.p01.lp1.ctest.srv.nintendo.net/100m", "", 200},
		{"custom HTTP", "downloads.example", "http://downloads.example/sample", "", 200},
		{"custom HTTPS", "downloads.example", "https://downloads.example/sample", "1.1.1.1", 200},
		{"port 80", "downloads.example", "http://downloads.example:80/sample", "", 200},
		{"port 443", "downloads.example", "https://downloads.example:443/sample", "", 200},
		{"nonstandard port", "downloads.example", "http://downloads.example:8080/sample", "", 400},
		{"other protocol", "downloads.example", "ftp://downloads.example/sample", "", 400},
		{"host mismatch", "downloads.example", "http://other.example/sample", "", 400},
		{"credentials", "downloads.example", "http://user:password@downloads.example/sample", "", 400},
		{"fragment", "downloads.example", "http://downloads.example/sample#part", "", 400},
		{"private candidate", "downloads.example", "http://downloads.example/sample", "192.168.1.1", 400},
		{"loopback candidate", "downloads.example", "http://downloads.example/sample", "127.0.0.1", 400},
		{"special candidate", "downloads.example", "http://downloads.example/sample", "169.254.169.254", 400},
		{"IPv6 candidate", "downloads.example", "http://downloads.example/sample", "2606:4700:4700::1111", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, h := testAPI(t)
			c := a.m.Config()
			c.Rules = c.Rules[:1]
			c.Rules[0].Platform = "Switch"
			c.Rules[0].Domain = tc.domain
			c.Rules[0].URL = tc.url
			if tc.candidate != "" {
				c.Rules[0].Candidates = []string{tc.candidate}
			}
			data, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			r := call(h, http.MethodPut, "/api/v1/config", string(data), "1", "")
			if r.Code != tc.want {
				t.Fatalf("save status %d, want %d: %s", r.Code, tc.want, r.Body.String())
			}
			if tc.want != http.StatusOK {
				if a.m.Config().Rules[0].Domain == tc.domain {
					t.Fatal("rejected rule changed the configuration")
				}
				return
			}
			var stored Config
			if err := a.m.store.Get("config", &stored); err != nil {
				t.Fatal(err)
			}
			if len(stored.Rules) != 1 || stored.Rules[0].Domain != tc.domain || stored.Rules[0].URL != tc.url {
				t.Fatalf("custom rule not persisted: %+v", stored.Rules)
			}
			backup := call(h, http.MethodGet, "/api/v1/backup", "", "", "")
			if backup.Code != http.StatusOK {
				t.Fatal(backup.Body.String())
			}
			if r := call(h, http.MethodPost, "/api/v1/restore", backup.Body.String(), "1", ""); r.Code != http.StatusOK {
				t.Fatalf("restore rejected custom rule: %s", r.Body.String())
			}
		})
	}
}
