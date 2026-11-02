package main

import (
	"context"
	"dns-assistant/internal/app"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var shell32 = windows.NewLazySystemDLL("shell32.dll")
var kernel32 = windows.NewLazySystemDLL("kernel32.dll")

const (
	wmTray          = 0x8001
	wmServiceDone   = 0x8002
	wmServiceReady  = 0x8003
	wmUpdateChecked = 0x8004
	menuOpen        = 1
	menuUpdate      = 2
	menuStartup     = 3
	menuData        = 4
	menuExit        = 5
)

type winPoint struct{ X, Y int32 }
type winMessage struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          winPoint
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Procedure                          uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type notifyIcon struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

type updateResult struct {
	version string
	err     error
}
type desktopTray struct {
	window             uintptr
	largeIcon          uintptr
	icon               notifyIcon
	options            desktopOptions
	startup            startupStore
	ctx                context.Context
	cancel             context.CancelFunc
	ready              chan string
	done               chan error
	updateResult       chan updateResult
	address            string
	checking, stopping bool
	taskbarCreated     uint32
	manager            *managementWindow
	openPending        bool
}

func wide(s string) *uint16 { value, _ := windows.UTF16PtrFromString(s); return value }
func messageBox(window uintptr, message, title string, flags uintptr) uintptr {
	result, _, _ := user32.NewProc("MessageBoxW").Call(window, uintptr(unsafe.Pointer(wide(message))), uintptr(unsafe.Pointer(wide(title))), flags)
	return result
}
func desktopError(message string, err error) {
	messageBox(0, message+"\n\n"+err.Error(), "DNS Assistant", 0x10)
}

// Resource 1 is generated from web/public/logo.svg. Loading at the system's
// requested size keeps the tray, title bar, taskbar and Explorer icon identical.
func loadApplicationIcon(widthMetric, heightMetric uintptr) uintptr {
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	width, _, _ := user32.NewProc("GetSystemMetrics").Call(widthMetric)
	height, _, _ := user32.NewProc("GetSystemMetrics").Call(heightMetric)
	handle, _, _ := user32.NewProc("LoadImageW").Call(instance, 1, 1, width, height, 0)
	return handle
}

func createTrayIcon() uintptr { return loadApplicationIcon(49, 50) } // SM_CXSMICON, SM_CYSMICON

func (t *desktopTray) notify(operation uintptr, tip string) error {
	if tip != "" {
		clear(t.icon.Tip[:])
		copy(t.icon.Tip[:127], utf16.Encode([]rune(tip)))
	}
	result, _, err := shell32.NewProc("Shell_NotifyIconW").Call(operation, uintptr(unsafe.Pointer(&t.icon)))
	if result == 0 {
		return fmt.Errorf("无法显示托盘图标: %v", err)
	}
	return nil
}

func (t *desktopTray) post(message uint32) {
	user32.NewProc("PostMessageW").Call(t.window, uintptr(message), 0, 0)
}

func (t *desktopTray) stop() {
	if !t.stopping {
		t.stopping = true
		t.notify(1, "DNS Assistant · 正在停止")
		t.cancel()
	}
}

func (t *desktopTray) action(id uintptr) {
	if t.stopping {
		return
	}
	switch id {
	case menuOpen:
		if t.address != "" {
			t.showManagement()
		} else {
			t.openPending = true
		}
	case menuData:
		command := exec.Command("explorer.exe", t.options.Data)
		if err := command.Start(); err != nil {
			desktopError("无法打开数据目录", err)
		} else {
			go command.Wait()
		}
	case menuStartup:
		enabled, err := t.startup.enabled()
		if err == nil {
			err = t.startup.set(!enabled)
		}
		if err != nil {
			desktopError("无法修改开机启动", err)
		}
	case menuExit:
		t.stop()
	case menuUpdate:
		if t.checking {
			return
		}
		t.checking = true
		t.notify(1, "DNS Assistant · 正在检查更新")
		go func() {
			ctx, cancel := context.WithTimeout(t.ctx, 20*time.Second)
			defer cancel()
			version, err := checkDesktopUpdate(ctx, &http.Client{Timeout: 20 * time.Second}, latestReleaseURL, app.Version())
			t.updateResult <- updateResult{version, err}
			t.post(wmUpdateChecked)
		}()
	}
}

func (t *desktopTray) buildMenu() uintptr {
	menu, _, _ := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return 0
	}
	appendItem := func(id uintptr, text string, flags uintptr) {
		user32.NewProc("AppendMenuW").Call(menu, flags, id, uintptr(unsafe.Pointer(wide(text))))
	}
	appendItem(0, "DNS Assistant v"+app.Version(), 1)
	openFlags := uintptr(0)
	if t.address == "" || t.stopping {
		openFlags = 1
	}
	appendItem(menuOpen, "打开管理窗口", openFlags)
	updateFlags := uintptr(0)
	updateText := "检查更新…"
	if t.checking {
		updateText = "正在检查更新…"
		updateFlags = 1
	}
	if t.stopping {
		updateFlags = 1
	}
	appendItem(menuUpdate, updateText, updateFlags)
	startupFlags := uintptr(0)
	if enabled, _ := t.startup.enabled(); enabled {
		startupFlags = 8
	}
	if t.stopping {
		startupFlags |= 1
	}
	appendItem(menuStartup, "开机启动（登录后）", startupFlags)
	appendItem(menuData, "打开数据目录", 0)
	appendItem(0, "", 0x800)
	exitText := "退出"
	if t.stopping {
		exitText = "正在停止…"
	}
	exitFlags := uintptr(0)
	if t.stopping {
		exitFlags = 1
	}
	appendItem(menuExit, exitText, exitFlags)
	return menu
}

func (t *desktopTray) menu() {
	menu := t.buildMenu()
	if menu == 0 {
		return
	}
	defer user32.NewProc("DestroyMenu").Call(menu)
	var point winPoint
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
	user32.NewProc("SetForegroundWindow").Call(t.window)
	id, _, _ := user32.NewProc("TrackPopupMenu").Call(menu, 0x100|0x2, uintptr(point.X), uintptr(point.Y), 0, t.window, 0)
	t.post(0)
	t.action(id)
}

func (t *desktopTray) procedure(window uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case 0x111:
		if lparam == 0 {
			t.action(wparam & 0xffff)
		}
		return 0
	case wmTray:
		switch uint32(lparam) & 0xffff {
		case 0x203:
			t.action(menuOpen)
		case 0x205, 0x7b:
			t.menu()
		}
		return 0
	case 0x10:
		t.stop()
		return 0 // WM_CLOSE: drain services before destroying the window.
	case 0x11:
		t.stop()
		return 1 // WM_QUERYENDSESSION
	case 0x16:
		if wparam != 0 {
			t.stop()
		}
		return 0
	case wmServiceReady:
		t.address = <-t.ready
		if !t.stopping {
			t.notify(1, "DNS Assistant · 运行中")
			if t.options.OpenBrowser || t.openPending {
				t.showManagement()
			}
		}
		return 0
	case wmServiceDone:
		if t.manager != nil {
			t.manager.close()
		}
		user32.NewProc("DestroyWindow").Call(window)
		return 0
	case 0x2:
		user32.NewProc("PostQuitMessage").Call(0)
		return 0
	case wmUpdateChecked:
		result := <-t.updateResult
		t.checking = false
		if t.stopping {
			return 0
		}
		t.notify(1, "DNS Assistant · 运行中")
		if result.err != nil {
			desktopError("检查更新失败，当前服务继续运行", result.err)
			return 0
		}
		if result.version == "" {
			messageBox(t.window, "当前已是最新正式版本 v"+app.Version(), "DNS Assistant", 0x40)
			return 0
		}
		if messageBox(t.window, "发现新版本 "+result.version+"。\n\n更新会暂时停止 DNS 和下载代理，备份数据后安装，完成后自动启动。是否继续？", "DNS Assistant 更新", 0x24) != 6 {
			return 0
		}
		if err := launchTrayUpdate(t.options); err != nil {
			desktopError("无法启动更新，当前服务继续运行", err)
			return 0
		}
		t.stop()
		return 0
	}
	if message == t.taskbarCreated {
		if err := t.notify(0, "DNS Assistant · 运行中"); err != nil {
			slog.Error("恢复托盘图标失败", "error", err)
			t.stop()
		}
		return 0
	}
	result, _, _ := user32.NewProc("DefWindowProcW").Call(window, uintptr(message), wparam, lparam)
	return result
}

func launchTrayUpdate(o desktopOptions) error {
	script := filepath.Join(filepath.Dir(o.Executable), "update.ps1")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("请使用完整便携包或安装包，缺少 update.ps1: %w", err)
	}
	// Compose the restart command once with Windows escaping, then transport it as
	// base64 so PowerShell never interprets paths or arguments as shell code.
	commandLine := windows.ComposeCommandLine(o.arguments())
	encoded := base64.StdEncoding.EncodeToString([]byte(commandLine))
	command := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script,
		"-InstallDir", filepath.Dir(o.Executable), "-WaitForProcessId", strconv.Itoa(os.Getpid()),
		"-RestartArguments", encoded, "-DataDir", o.Data, "-Notify")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

type desktopLog struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func (l *desktopLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size+int64(len(p)) > 2<<20 {
		l.file.Close()
		os.Remove(l.path + ".1")
		if err := os.Rename(l.path, l.path+".1"); err != nil {
			return 0, err
		}
		file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return 0, err
		}
		l.file, l.size = file, 0
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	return n, err
}

func runDesktop(parent context.Context, o desktopOptions, tray bool) error {
	if !tray {
		return runDesktopService(parent, o)
	}
	if err := os.MkdirAll(o.Data, 0700); err != nil {
		return err
	}
	path := filepath.Join(o.Data, "desktop.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	writer := &desktopLog{path: path, file: file, size: info.Size()}
	defer func() { writer.mu.Lock(); defer writer.mu.Unlock(); writer.file.Close() }()
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(writer, nil)))
	defer slog.SetDefault(previousLogger)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	t := &desktopTray{options: o, startup: startupStore{startupRegistryPath, o}, ctx: ctx, cancel: cancel,
		ready: make(chan string, 1), done: make(chan error, 1), updateResult: make(chan updateResult, 1)}
	if enabled, err := t.startup.enabled(); err != nil {
		return err
	} else if enabled {
		if err = t.startup.set(true); err != nil {
			return err
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		return fmt.Errorf("初始化管理窗口失败: %w", err)
	}
	defer windows.CoUninitialize()
	defer func() {
		if t.manager != nil {
			t.manager.close()
		}
	}()
	// Set DPI awareness before creating either window, so WebView2 and the frame
	// use the same physical coordinates on scaled and mixed-DPI displays.
	if proc := user32.NewProc("SetProcessDpiAwarenessContext"); proc.Find() == nil {
		proc.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	}
	className := wide(desktopClassName(o.Executable))
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	class := windowClass{Instance: instance, ClassName: className, Procedure: windows.NewCallback(t.procedure)}
	class.Size = uint32(unsafe.Sizeof(class))
	atom, _, callErr := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return fmt.Errorf("注册托盘窗口失败: %v", callErr)
	}
	defer user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(className)), instance)
	window, _, callErr := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wide("DNS Assistant Tray"))), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if window == 0 {
		return fmt.Errorf("创建托盘窗口失败: %v", callErr)
	}
	t.window = window
	defer user32.NewProc("DestroyWindow").Call(window)
	created, _, _ := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(wide("TaskbarCreated"))))
	t.taskbarCreated = uint32(created)
	icon := createTrayIcon()
	if icon == 0 {
		return fmt.Errorf("创建托盘图标失败")
	}
	defer user32.NewProc("DestroyIcon").Call(icon)
	t.largeIcon = loadApplicationIcon(11, 12) // SM_CXICON, SM_CYICON
	if t.largeIcon == 0 {
		return fmt.Errorf("加载应用图标失败")
	}
	defer user32.NewProc("DestroyIcon").Call(t.largeIcon)
	t.icon = notifyIcon{Window: window, ID: 1, Flags: 1 | 2 | 4, Callback: wmTray, Icon: icon}
	t.icon.Size = uint32(unsafe.Sizeof(t.icon))
	if err = t.notify(0, "DNS Assistant · 正在启动"); err != nil {
		return err
	}
	defer t.notify(2, "")
	go func() {
		err := runDesktopService(ctx, o, func(address string) { t.ready <- address; t.post(wmServiceReady) })
		t.done <- err
		t.post(wmServiceDone)
	}()
	var message winMessage
	for {
		result, _, callErr := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			cancel()
			<-t.done
			return fmt.Errorf("托盘消息处理失败: %v", callErr)
		}
		if result == 0 {
			break
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
	}
	cancel()
	runtime.KeepAlive(className)
	return <-t.done
}
