package main

import (
	"context"
	"dns-assistant/internal/app"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestStandaloneLifecycle(t *testing.T) {
	dir := t.TempDir()
	store, err := app.OpenStore(filepath.Join(dir, "controller"))
	if err != nil {
		t.Fatal(err)
	}
	config := app.DefaultConfig()
	config.Enabled = false // 本地验收不执行公网测速。
	if err = store.Set("config", config); err != nil {
		t.Fatal(err)
	}
	store.Close()
	dnsAddr, webAddr, proxyAddr := freeAddress(t), freeAddress(t), freeAddress(t)
	client := &http.Client{Timeout: time.Second}
	for attempt := range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- runStandalone(ctx, dir, dnsAddr, webAddr, proxyAddr) }()
		t.Cleanup(cancel)
		deadline := time.Now().Add(10 * time.Second)
		for {
			var status struct {
				Connected bool `json:"dns_connected"`
				Enabled   bool `json:"enabled"`
			}
			response, getErr := client.Get("http://" + webAddr + "/api/v1/status")
			if getErr == nil {
				decodeErr := json.NewDecoder(response.Body).Decode(&status)
				response.Body.Close()
				if decodeErr == nil && status.Connected && !status.Enabled {
					break
				}
			}
			select {
			case err := <-done:
				t.Fatalf("attempt %d exited early: %v", attempt, err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatal("management and DNS policy did not become ready")
			}
			time.Sleep(50 * time.Millisecond)
		}
		response, err := client.Get("http://" + webAddr + "/")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "<html") {
			t.Fatalf("embedded UI unavailable: status=%d error=%v", response.StatusCode, err)
		}
		for _, transport := range []string{"udp", "tcp"} {
			query := new(dns.Msg)
			query.SetQuestion(dns.Fqdn(config.Rules[0].Domain), dns.TypeAAAA)
			answer, _, err := (&dns.Client{Net: transport, Timeout: time.Second}).Exchange(query, dnsAddr)
			if err != nil || answer.Rcode != dns.RcodeSuccess || len(answer.Answer) != 0 {
				t.Fatalf("%s local AAAA response: %v %v", transport, answer, err)
			}
		}
		proxyResponse, proxyErr := client.Get("http://" + proxyAddr + "/")
		if proxyErr != nil {
			t.Fatal(proxyErr)
		}
		proxyResponse.Body.Close()
		if proxyResponse.StatusCode != http.StatusBadRequest {
			t.Fatal("proxy listener did not reject a non-proxy request")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("standalone did not stop")
		}
	}
}

func TestStandalonePortConflict(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := runStandalone(context.Background(), t.TempDir(), "127.0.0.1:0", listener.Addr().String(), freeAddress(t)); err == nil {
		t.Fatal("occupied management port was accepted")
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	for range 100 {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := listener.Addr().String()
		packet, err := net.ListenPacket("udp4", addr)
		listener.Close()
		if err == nil {
			packet.Close()
			return addr
		}
	}
	t.Fatal("no free TCP/UDP address pair")
	return ""
}
