package main

import (
	"crypto/sha256"
	"dns-assistant/internal/app"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

func TestPackagedManagementWindow(t *testing.T) {
	executable := os.Getenv("DNS_ASSISTANT_DESKTOP_BINARY")
	if executable == "" {
		t.Skip("requires packaged EXE and an interactive Windows desktop")
	}
	for _, autoOpen := range []bool{true, false} {
		t.Run(fmt.Sprintf("autoOpen=%v", autoOpen), func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "dns-assistant.exe")
			content, err := os.ReadFile(executable)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, content, 0700); err != nil {
				t.Fatal(err)
			}
			data := filepath.Join(root, "data")
			store, err := app.OpenStore(filepath.Join(data, "controller"))
			if err != nil {
				t.Fatal(err)
			}
			config := app.DefaultConfig()
			config.Enabled = false
			if err := store.Set("config", config); err != nil {
				t.Fatal(err)
			}
			store.Close()
			dns, web, proxy := freeAddress(t), freeAddress(t), freeAddress(t)
			command := exec.Command(binary, "standalone", "-data", data, "-listen", dns, "-web", web, "-proxy", proxy, fmt.Sprintf("-open-window=%v", autoOpen))
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait(); close(done) }()
			defer func() {
				command.Process.Kill()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
				}
				cleanupManagementCache(t, data)
			}()
			tray := waitForTrayService(t, uint32(command.Process.Pid), web)
			if !autoOpen {
				if managerWindow(uint32(command.Process.Pid)) != 0 {
					t.Fatal("silent startup displayed management")
				}
				user32.NewProc("PostMessageW").Call(tray, 0x111, menuOpen, 0)
			}
			manager := waitForManagementWindow(t, uint32(command.Process.Pid))
			// Optional bounded fixture for inspecting the actual packaged window.
			// Creating the marker ends inspection and resumes the lifecycle checks.
			if marker := os.Getenv("DNS_ASSISTANT_WINDOW_INSPECTION_DONE"); autoOpen && marker != "" {
				t.Logf("management inspection ready: pid=%d", command.Process.Pid)
				deadline := time.Now().Add(5 * time.Minute)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(marker); err == nil {
						break
					}
					time.Sleep(250 * time.Millisecond)
				}
			}
			assertNoConsole(t, command.Process.Pid)
			user32.NewProc("SendMessageW").Call(manager, 0x10, 0, 0)
			if visible, _, _ := user32.NewProc("IsWindowVisible").Call(manager); visible != 0 {
				t.Fatal("close did not hide window")
			}
			if err := app.QueryHealth(dns); err != nil {
				t.Fatalf("closing management stopped DNS: %v", err)
			}
			// A second EXE launch activates the existing service/window and exits.
			second := exec.Command(binary)
			if err := second.Start(); err != nil {
				t.Fatal(err)
			}
			secondDone := make(chan error, 1)
			go func() { secondDone <- second.Wait() }()
			select {
			case err := <-secondDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				second.Process.Kill()
				t.Fatal("second launch did not activate existing instance")
			}
			assertManagerVisible(t, manager)
			user32.NewProc("SendMessageW").Call(manager, 0x10, 0, 0)
			user32.NewProc("PostMessageW").Call(tray, wmTray, 1, 0x203)
			assertManagerVisible(t, manager)
			if managerWindow(uint32(command.Process.Pid)) != manager {
				t.Fatal("reopening created another management window")
			}
			user32.NewProc("PostMessageW").Call(tray, 0x111, menuExit, 0)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("window prevented graceful exit")
			}
			if result, _, _ := user32.NewProc("IsWindow").Call(manager); result != 0 {
				t.Fatal("management window survived exit")
			}
			t.Log("real WebView2 navigation, close-to-tray, activation, tray double-click, no console and graceful exit passed")
		})
	}
}

func managerWindow(pid uint32) uintptr {
	var window uintptr
	for {
		window, _, _ = user32.NewProc("FindWindowExW").Call(0, window, uintptr(unsafe.Pointer(wide(managementClass))), 0)
		if window == 0 {
			return 0
		}
		var owner uint32
		user32.NewProc("GetWindowThreadProcessId").Call(window, uintptr(unsafe.Pointer(&owner)))
		if owner == pid {
			return window
		}
	}
}

func waitForManagementWindow(t *testing.T, pid uint32) uintptr {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		window := managerWindow(pid)
		ready, _, _ := user32.NewProc("GetPropW").Call(window, uintptr(unsafe.Pointer(wide("DNSAssistant.WebViewReady"))))
		if window != 0 && ready != 0 {
			return window
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("embedded management did not finish navigation")
	return 0
}

func cleanupManagementCache(t *testing.T, data string) {
	t.Helper()
	cacheRoot, _ := os.UserCacheDir()
	cache := filepath.Join(cacheRoot, "DNS Assistant", "WebView2", fmt.Sprintf("%x", sha256.Sum256([]byte(data))))
	for range 50 {
		if os.RemoveAll(cache) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("test WebView2 cache did not release: %s", cache)
}

func assertManagerVisible(t *testing.T, window uintptr) {
	t.Helper()
	for range 50 {
		if visible, _, _ := user32.NewProc("IsWindowVisible").Call(window); visible != 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("management window did not reopen")
}
