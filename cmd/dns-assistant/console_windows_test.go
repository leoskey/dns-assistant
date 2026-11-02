package main

import (
	"debug/pe"
	"dns-assistant/internal/app"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestPackagedWindowsGUI(t *testing.T) {
	executable := os.Getenv("DNS_ASSISTANT_DESKTOP_BINARY")
	if executable == "" {
		t.Skip("requires packaged executable")
	}
	file, err := pe.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	header, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || header.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
		t.Fatal("Windows release must use the GUI subsystem; hiding a console after startup still flashes")
	}
	if header.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].Size == 0 {
		t.Fatal("Windows release is missing the embedded application icon")
	}
	version, err := exec.Command(executable, "version").Output()
	if err != nil || !strings.HasPrefix(string(version), "DNS Assistant v") {
		t.Fatalf("version output lost: %q %v", version, err)
	}
	config, err := exec.Command(executable, "config").Output()
	if err != nil || !json.Valid(config) {
		t.Fatalf("config output lost: %q %v", config, err)
	}
}

func assertNoConsole(t *testing.T, pid int) {
	t.Helper()
	helper := exec.Command(os.Args[0], "-test.run=^TestWindowsConsoleHelper$", "-test.v")
	helper.Env = append(os.Environ(), "DNS_ASSISTANT_CONSOLE_PROBE="+strconv.Itoa(pid))
	helper.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
	if output, err := helper.CombinedOutput(); err != nil {
		t.Fatalf("console check: %s %v", output, err)
	}
}

func TestWindowsConsoleHelper(t *testing.T) {
	if text := os.Getenv("DNS_ASSISTANT_CONSOLE_PROBE"); text != "" {
		pid, err := strconv.Atoi(text)
		if err != nil {
			t.Fatal(err)
		}
		result, _, err := kernel32.NewProc("AttachConsole").Call(uintptr(pid))
		if result != 0 {
			kernel32.NewProc("FreeConsole").Call()
			t.Fatal("tray process has a console")
		}
		if err != windows.ERROR_INVALID_HANDLE {
			t.Fatalf("unexpected console probe failure: %v", err)
		}
		return
	}
	if os.Getenv("DNS_ASSISTANT_CONSOLE_SERVICE") != "1" {
		t.Skip("subprocess helper")
	}
	// This helper owns a hidden console. The GUI executable must attach to it,
	// preserve redirected output, and still drain the service after Ctrl+C.
	handler := windows.NewCallback(func(uint32) uintptr { return 1 })
	kernel32.NewProc("SetConsoleCtrlHandler").Call(handler, 1)
	defer kernel32.NewProc("SetConsoleCtrlHandler").Call(handler, 0)
	dir := t.TempDir()
	store, err := app.OpenStore(filepath.Join(dir, "controller"))
	if err != nil {
		t.Fatal(err)
	}
	config := app.DefaultConfig()
	config.Enabled = false
	if err := store.Set("config", config); err != nil {
		t.Fatal(err)
	}
	store.Close()
	web := freeAddress(t)
	command := exec.Command(os.Getenv("DNS_ASSISTANT_DESKTOP_BINARY"), "standalone", "-data", dir, "-listen", freeAddress(t), "-web", web, "-proxy", freeAddress(t), "-tray=false", "-open-browser=false")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	client := &http.Client{Timeout: time.Second}
	ready := false
	for i := 0; i < 100; i++ {
		response, err := client.Get("http://" + web + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("console service did not start")
	}
	result, _, err := kernel32.NewProc("GenerateConsoleCtrlEvent").Call(0, 0)
	if result == 0 {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("Ctrl+C was not graceful", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Ctrl+C did not stop service")
	}
}

func TestPackagedConsoleMode(t *testing.T) {
	if os.Getenv("DNS_ASSISTANT_DESKTOP_BINARY") == "" {
		t.Skip("requires packaged executable")
	}
	helper := exec.Command(os.Args[0], "-test.run=^TestWindowsConsoleHelper$", "-test.v")
	helper.Env = append(os.Environ(), "DNS_ASSISTANT_CONSOLE_SERVICE=1")
	helper.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
	if output, err := helper.CombinedOutput(); err != nil {
		t.Fatal(fmt.Sprintf("console mode: %s %v", output, err))
	}
}
