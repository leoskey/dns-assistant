package main

import (
	"dns-assistant/internal/app"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestStartupToggleAndMenu(t *testing.T) {
	dir := t.TempDir()
	options := desktopOptions{Executable: filepath.Join(dir, "dns assistant.exe"), Data: filepath.Join(dir, "data with spaces"), DNS: "127.0.0.1:1053", Web: "127.0.0.1:18080", Proxy: "127.0.0.1:18082", IP: "192.168.1.2"}
	keyPath := fmt.Sprintf(`Software\DNSAssistant\Tests\%d-%d`, os.Getpid(), time.Now().UnixNano())
	defer registry.DeleteKey(registry.CURRENT_USER, keyPath)
	store := startupStore{keyPath, options}
	if on, err := store.enabled(); err != nil || on {
		t.Fatalf("initial state %v, %v", on, err)
	}
	if err := store.set(true); err != nil {
		t.Fatal(err)
	}
	if on, err := store.enabled(); err != nil || !on {
		t.Fatalf("enabled %v, %v", on, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".startup.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved desktopOptions
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.arguments(), options.arguments()) {
		t.Fatal("startup parameters lost")
	}
	value, _ := store.value()
	args, err := windows.DecomposeCommandLine(value)
	if err != nil || !reflect.DeepEqual(args, []string{options.Executable, "autostart"}) {
		t.Fatalf("bad startup command %q: %v", value, err)
	}
	tray := &desktopTray{startup: store, address: options.Web, checking: true}
	menu := tray.buildMenu()
	if menu == 0 {
		t.Fatal("menu creation failed")
	}
	defer user32.NewProc("DestroyMenu").Call(menu)
	state, _, _ := user32.NewProc("GetMenuState").Call(menu, menuStartup, 0)
	if state&8 == 0 {
		t.Fatal("enabled startup is not checked")
	}
	state, _, _ = user32.NewProc("GetMenuState").Call(menu, menuExit, 0)
	if state&3 != 0 {
		t.Fatal("update check must not prevent exit")
	}
	for id, want := range map[uintptr]string{menuOpen: "打开管理窗口", menuUpdate: "正在检查更新…", menuStartup: "开机启动（登录后）", menuData: "打开数据目录", menuExit: "退出"} {
		var text [128]uint16
		user32.NewProc("GetMenuStringW").Call(menu, id, uintptr(unsafe.Pointer(&text[0])), 128, 0)
		if windows.UTF16ToString(text[:]) != want {
			t.Fatalf("menu item %d missing", id)
		}
	}
	tray.action(menuStartup)
	if on, _ := store.enabled(); on {
		t.Fatal("startup still enabled")
	}
	tray.action(menuStartup)
	if on, err := store.enabled(); err != nil || !on {
		t.Fatalf("startup menu did not enable startup: %v", err)
	}
	tray.action(menuStartup)
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	k.SetStringValue(startupValueName, `"C:\Other\dns-assistant.exe" autostart`)
	k.Close()
	if err := store.set(false); err == nil {
		t.Fatal("removed another installation's startup")
	}
}

func TestPackagedTrayLifecycle(t *testing.T) {
	executable := os.Getenv("DNS_ASSISTANT_DESKTOP_BINARY")
	if executable == "" {
		t.Skip("set DNS_ASSISTANT_DESKTOP_BINARY for interactive Windows integration")
	}
	executable, err := filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
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
	dnsAddr, webAddr, proxyAddr := freeAddress(t), freeAddress(t), freeAddress(t)
	for attempt := range 2 {
		command := exec.Command(executable, "standalone", "-data", dir, "-listen", dnsAddr, "-web", webAddr, "-proxy", proxyAddr, "-open-browser=false")
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		defer func() { command.Process.Kill() }()
		client := &http.Client{Timeout: time.Second}
		deadline := time.Now().Add(15 * time.Second)
		var window uintptr
		for time.Now().Before(deadline) {
			window = trayWindowForPID(uint32(command.Process.Pid))
			response, err := client.Get("http://" + webAddr + "/api/v1/status")
			if err == nil {
				var status struct {
					Connected bool `json:"dns_connected"`
				}
				json.NewDecoder(response.Body).Decode(&status)
				response.Body.Close()
				if status.Connected && window != 0 {
					break
				}
			}
			select {
			case err := <-done:
				t.Fatalf("tray process exited early: %v", err)
			default:
			}
			time.Sleep(100 * time.Millisecond)
		}
		if window == 0 {
			t.Fatal("tray window was not created")
		}
		if err := app.QueryHealth(dnsAddr); err != nil {
			t.Fatal(err)
		}
		assertNoConsole(t, command.Process.Pid)
		var rect [4]int32
		identifier := struct {
			Size   uint32
			Window uintptr
			ID     uint32
			GUID   windows.GUID
		}{Window: window, ID: 1}
		identifier.Size = uint32(unsafe.Sizeof(identifier))
		result, _, _ := shell32.NewProc("Shell_NotifyIconGetRect").Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&rect[0])))
		if int32(result) < 0 {
			t.Fatalf("tray icon was not registered: 0x%x", result)
		}
		// Simulate Explorer recreating its notification area without restarting Explorer.
		icon := notifyIcon{Window: window, ID: 1}
		icon.Size = uint32(unsafe.Sizeof(icon))
		shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&icon)))
		taskbarCreated, _, _ := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(wide("TaskbarCreated"))))
		user32.NewProc("SendMessageW").Call(window, taskbarCreated, 0, 0)
		result, _, _ = shell32.NewProc("Shell_NotifyIconGetRect").Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&rect[0])))
		if int32(result) < 0 {
			t.Fatal("tray icon was not restored")
		}
		user32.NewProc("PostMessageW").Call(window, 0x111, menuExit, 0)
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("tray exit did not drain services")
		}
		for _, address := range []string{dnsAddr, webAddr, proxyAddr} {
			conn, err := net.DialTimeout("tcp", address, time.Second)
			if err == nil {
				conn.Close()
				t.Fatalf("listener still running: %s", address)
			}
		}
		t.Logf("tray run %d: icon, policy, restart recovery and graceful shutdown passed", attempt+1)
	}
	store, err = app.OpenStore(filepath.Join(dir, "controller"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var saved app.Config
	if err := store.Get("config", &saved); err != nil || saved.Enabled {
		t.Fatal("persisted config changed", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "desktop.log")); err != nil || !strings.Contains(string(data), "单机服务已启动") {
		t.Fatal("desktop log unavailable", err)
	}
}

func trayWindowForPID(pid uint32) uintptr {
	var found uintptr
	callback := windows.NewCallback(func(window, param uintptr) uintptr {
		var owner uint32
		user32.NewProc("GetWindowThreadProcessId").Call(window, uintptr(unsafe.Pointer(&owner)))
		if owner == pid {
			var title [128]uint16
			user32.NewProc("GetWindowTextW").Call(window, uintptr(unsafe.Pointer(&title[0])), 128)
			if windows.UTF16ToString(title[:]) == "DNS Assistant Tray" {
				found = window
				return 0
			}
		}
		return 1
	})
	user32.NewProc("EnumWindows").Call(callback, 0)
	return found
}

func TestDesktopLogRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := &desktopLog{path: path, file: file}
	defer func() { writer.file.Close() }()
	if _, err := writer.Write([]byte(strings.Repeat("x", 2<<20))); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("new log")); err != nil {
		t.Fatal(err)
	}
	old, err := os.Stat(path + ".1")
	if err != nil || old.Size() != 2<<20 {
		t.Fatal("log was not rotated", err)
	}
}
