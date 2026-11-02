package main

import (
	"dns-assistant/internal/app"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Explicitly opt in: this test toggles the installed application's startup
// setting, closes it through its tray command, exercises autostart, then restores
// its settings and leaves it running. Never run as part of unattended CI.
func TestInstalledTrayActions(t *testing.T) {
	executable := os.Getenv("DNS_ASSISTANT_INSTALLED_BINARY")
	pid, err := strconv.ParseUint(os.Getenv("DNS_ASSISTANT_INSTALLED_PID"), 10, 32)
	if executable == "" || err != nil {
		t.Skip("set DNS_ASSISTANT_INSTALLED_BINARY and DNS_ASSISTANT_INSTALLED_PID for installed-app verification")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	var path [32768]uint16
	size := uint32(len(path))
	if err := windows.QueryFullProcessImageName(handle, 0, &path[0], &size); err != nil || !strings.EqualFold(windows.UTF16ToString(path[:size]), executable) {
		t.Fatal("selected process does not belong to the requested installation", err)
	}
	window := trayWindowForPID(uint32(pid))
	if window == 0 {
		t.Fatal("installed tray window missing")
	}
	store := startupStore{startupRegistryPath, desktopOptions{Executable: executable}}
	previous, err := store.value()
	if err != nil || previous != "" && !strings.EqualFold(previous, store.command()) {
		t.Fatal("cannot test startup while another installation owns the setting", err)
	}
	startupPath := filepath.Join(filepath.Dir(executable), ".startup.json")
	saved, readErr := os.ReadFile(startupPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	var options desktopOptions
	stopped := false
	t.Cleanup(func() {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, startupRegistryPath, registry.SET_VALUE)
		if err != nil {
			t.Error("restore startup setting:", err)
		} else {
			if previous == "" {
				err = key.DeleteValue(startupValueName)
				if err == registry.ErrNotExist {
					err = nil
				}
			} else {
				err = key.SetStringValue(startupValueName, previous)
			}
			key.Close()
			if err != nil {
				t.Error("restore startup setting:", err)
			}
		}
		if readErr == nil {
			err = os.WriteFile(startupPath, saved, 0600)
		} else {
			err = os.Remove(startupPath)
			if os.IsNotExist(err) {
				err = nil
			}
		}
		if err != nil {
			t.Error("restore startup configuration:", err)
		}
		if stopped {
			args := options.arguments()
			args = append(args, "-open-window=true")
			restart := exec.Command(executable, args...)
			if err := restart.Start(); err != nil {
				t.Error("restore application:", err)
			} else {
				waitForTrayService(t, uint32(restart.Process.Pid), options.Web)
				restart.Process.Release()
			}
		}
	})
	post := func(id uintptr) { user32.NewProc("PostMessageW").Call(window, 0x111, id, 0) }
	setStartup := func(want bool) {
		on, err := store.enabled()
		if err != nil {
			t.Fatal(err)
		}
		if on != want {
			post(menuStartup)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			on, err = store.enabled()
			if err == nil && on == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("startup menu did not reach requested state", want, err)
	}
	setStartup(true)
	content, err := os.ReadFile(startupPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &options); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(options.Executable, executable) || options.Data == "" || options.Web == "" || options.DNS == "" || options.Proxy == "" {
		t.Fatal("startup menu saved incomplete settings")
	}
	setStartup(false)
	setStartup(true)
	t.Log("installed startup menu: enable, disable, re-enable and saved launch parameters passed")

	post(menuUpdate)
	deadline := time.Now().Add(25 * time.Second)
	var dialog uintptr
	for time.Now().Before(deadline) {
		dialog = installedUpdateDialog(uint32(pid))
		if dialog != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if dialog == 0 {
		t.Fatal("update check did not report a result")
	}
	var message strings.Builder
	callback := windows.NewCallback(func(child, param uintptr) uintptr {
		var class [64]uint16
		user32.NewProc("GetClassNameW").Call(child, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
		if windows.UTF16ToString(class[:]) == "Static" {
			var text [2048]uint16
			user32.NewProc("GetWindowTextW").Call(child, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
			message.WriteString(windows.UTF16ToString(text[:]))
		}
		return 1
	})
	user32.NewProc("EnumChildWindows").Call(dialog, callback, 0)
	// A check must never install a release or downgrade this local build.
	button := uintptr(1) // IDOK
	if no, _, _ := user32.NewProc("GetDlgItem").Call(dialog, 7); no != 0 {
		button = 7
	} // IDNO
	user32.NewProc("PostMessageW").Call(dialog, 0x111, button, 0)
	if text := message.String(); strings.Contains(text, "检查更新失败") {
		t.Errorf("live update check: %s", text)
	} else if !strings.Contains(text, "最新正式版本") && !strings.Contains(text, "发现新版本") {
		t.Errorf("unexpected update result: %s", text)
	} else {
		t.Logf("installed update menu: %s", text)
	}
	if err := app.QueryHealth(options.DNS); err != nil {
		t.Fatal("DNS stopped during update check", err)
	}

	post(menuExit)
	state, err := windows.WaitForSingleObject(handle, 15000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("installed tray exit did not stop the process", state, err)
	}
	stopped = true
	assertInstalledListenersClosed(t, options)
	t.Log("installed exit menu: process and DNS/management/proxy listeners stopped")

	command := exec.Command(executable, "autostart")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		if w := trayWindowForPID(uint32(command.Process.Pid)); w != 0 {
			user32.NewProc("PostMessageW").Call(w, 0x111, menuExit, 0)
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("autostart process did not stop")
			}
		}
	}()
	window = waitForTrayService(t, uint32(command.Process.Pid), options.Web)
	if w := managerWindow(uint32(command.Process.Pid)); w != 0 {
		if visible, _, _ := user32.NewProc("IsWindowVisible").Call(w); visible != 0 {
			t.Error("autostart opened a management window")
		}
	}
	if err := app.QueryHealth(options.DNS); err != nil {
		t.Fatal(err)
	}
	t.Log("installed autostart command: saved configuration, healthy DNS, tray-only startup and no console passed")
	post(menuExit)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("autostart process did not exit")
	}
	assertInstalledListenersClosed(t, options)
}

func installedUpdateDialog(pid uint32) uintptr {
	var found uintptr
	callback := windows.NewCallback(func(window, param uintptr) uintptr {
		var owner uint32
		user32.NewProc("GetWindowThreadProcessId").Call(window, uintptr(unsafe.Pointer(&owner)))
		if owner == pid {
			var class [64]uint16
			user32.NewProc("GetClassNameW").Call(window, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
			if windows.UTF16ToString(class[:]) == "#32770" {
				found = window
				return 0
			}
		}
		return 1
	})
	user32.NewProc("EnumWindows").Call(callback, 0)
	return found
}

func assertInstalledListenersClosed(t *testing.T, options desktopOptions) {
	t.Helper()
	for _, address := range []string{options.DNS, options.Web, options.Proxy} {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatal(err)
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		connection, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), time.Second)
		if err == nil {
			connection.Close()
			t.Errorf("listener still running: %s", address)
		}
	}
	if err := app.QueryHealth(options.DNS); err == nil {
		t.Error("DNS still answers after exit")
	}
}
