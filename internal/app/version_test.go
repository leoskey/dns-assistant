package app

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestStatusExposesApplicationVersionWithoutChangingPolicyVersion(t *testing.T) {
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(Version()) {
		t.Fatalf("invalid application version: %q", Version())
	}
	m := testManager(t)
	req := httptest.NewRequest("GET", "http://127.0.0.1/api/v1/status", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	NewAPI(m).Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status request failed: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		AppVersion  string `json:"app_version"`
		AppRevision string `json:"app_revision"`
		Policy      uint64 `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AppVersion != Version() || got.AppRevision != Revision || got.Policy != m.version {
		t.Fatalf("application and policy versions do not match their sources: %+v", got)
	}
}
