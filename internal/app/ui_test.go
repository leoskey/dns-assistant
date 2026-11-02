package app

import (
	"bytes"
	"dns-assistant/internal/ui"
	"strings"
	"testing"
)

func TestManagementLogoResource(t *testing.T) {
	_, handler := testAPI(t)
	want, err := ui.Files.ReadFile("dist/logo.svg")
	if err != nil {
		t.Fatal(err)
	}
	response := call(handler, "GET", "/logo.svg", "", "", "")
	if response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Type"), "image/svg+xml") || !bytes.Equal(response.Body.Bytes(), want) {
		t.Fatalf("logo must return the embedded SVG, got status %d, type %q", response.Code, response.Header().Get("Content-Type"))
	}
	page := call(handler, "GET", "/settings", "", "", "")
	if page.Code != 200 || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") {
		t.Fatal("management routes must still return the application page")
	}
}
