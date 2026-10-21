package app

import (
	"encoding/json"
	"testing"
)

func TestBatchProbeDeduplicatesAndHonorsPause(t *testing.T) {
	a, h := testAPI(t)
	if r := call(h, "POST", "/api/v1/probe-batch", `{"platform":"unknown"}`, "1", ""); r.Code != 400 {
		t.Fatal("unknown platform accepted")
	}
	var batch struct{ Queued, Skipped []string }
	r := call(h, "POST", "/api/v1/probe-batch", `{"platform":"PlayStation"}`, "1", "")
	if r.Code != 202 || json.Unmarshal(r.Body.Bytes(), &batch) != nil || len(batch.Queued) != 2 {
		t.Fatal(r.Body.String())
	}
	r = call(h, "POST", "/api/v1/probe-batch", `{"platform":"PlayStation"}`, "1", "")
	json.Unmarshal(r.Body.Bytes(), &batch)
	if len(batch.Queued) != 0 || len(batch.Skipped) != 2 {
		t.Fatal("batch did not deduplicate", r.Body.String())
	}
	c := a.m.Config()
	c.Enabled = false
	if err := a.m.SetConfig(c); err != nil {
		t.Fatal(err)
	}
	if r = call(h, "POST", "/api/v1/probe-batch", `{}`, "1", ""); r.Code != 409 {
		t.Fatal("paused batch accepted")
	}
	c.Enabled = true
	if err := a.m.SetConfig(c); err != nil {
		t.Fatal(err)
	}
	if r = call(h, "POST", "/api/v1/probe-batch", `{}`, "1", ""); r.Code != 202 {
		t.Fatal("batch rejected without budget", r.Body.String())
	}
}
