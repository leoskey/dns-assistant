package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDesktopUpdateCheck(t *testing.T) {
	for _, tc := range []struct {
		name, body, current, want string
		status                    int
		fail                      bool
	}{
		{"newer", `{"tag_name":"v0.10.0"}`, "0.9.0", "v0.10.0", 200, false},
		{"same", `{"tag_name":"v0.4.0"}`, "0.4.0", "", 200, false},
		{"older", `{"tag_name":"v0.3.0"}`, "0.4.0", "", 200, false},
		{"preview", `{"tag_name":"v0.5.0","prerelease":true}`, "0.4.0", "", 200, true},
		{"invalid", `{"tag_name":"vnext"}`, "0.4.0", "", 200, true},
		{"offline", `{}`, "0.4.0", "", 503, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer server.Close()
			got, err := checkDesktopUpdate(context.Background(), server.Client(), server.URL, tc.current)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestDesktopPaths(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "dns-assistant.exe")
	if got := standaloneDataDir(executable); got != filepath.Join(dir, "data") {
		t.Fatal(got)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", t.TempDir())
		if err := os.WriteFile(filepath.Join(dir, "installed.flag"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if got := standaloneDataDir(executable); got != filepath.Join(os.Getenv("LOCALAPPDATA"), "DNS Assistant", "data") {
			t.Fatal(got)
		}
	}
	for address, want := range map[string]string{
		"0.0.0.0:8080":      "http://127.0.0.1:8080",
		"127.0.0.1:18080":   "http://127.0.0.1:18080",
		"192.168.1.10:8080": "http://192.168.1.10:8080",
		"bad":               "",
	} {
		if got := managementURL(address); got != want {
			t.Fatalf("%s: %s != %s", address, got, want)
		}
	}
}

func TestDesktopExclusiveLock(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows desktop lock")
	}
	dir := t.TempDir()
	unlock, err := lockStandalone(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if unlock != nil {
			unlock()
		}
	})
	if other, err := lockStandalone(dir); err == nil {
		other()
		t.Fatal("second instance acquired lock")
	}
	unlock()
	unlock = nil
	again, err := lockStandalone(dir)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	again()
}
