package main

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/combridge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

const (
	wmManagementError    = 0x8010
	wmManagementNavigate = 0x8011
	managementClass      = "DNSAssistant.Management"
)

func desktopClassName(executable string) string {
	path, _ := filepath.Abs(executable)
	return fmt.Sprintf("DNSAssistant.Tray.%x", sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path)))))
}

func activateDesktop(executable string, show bool) bool {
	window, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(wide(desktopClassName(executable)))), 0)
	if window == 0 {
		return false
	}
	if !show {
		return true
	}
	var pid uint32
	user32.NewProc("GetWindowThreadProcessId").Call(window, uintptr(unsafe.Pointer(&pid)))
	user32.NewProc("AllowSetForegroundWindow").Call(uintptr(pid))
	result, _, _ := user32.NewProc("PostMessageW").Call(window, 0x111, menuOpen, 0)
	return result != 0
}

// Only the small, stable base WebView2 COM interfaces are used here. The loader
// and reference-counted callback bridge come from go-webview2; no GUI framework,
// separate loader DLL, JavaScript/native RPC, or external browser is required.
// Slot numbers follow Microsoft's WebView2.idl (IUnknown occupies slots 0–2).
type webCOM struct{ vtable *[64]uintptr }

//go:uintptrescapes
func (c *webCOM) call(slot int, args ...uintptr) error {
	values := append([]uintptr{uintptr(unsafe.Pointer(c))}, args...)
	result, _, _ := syscall.SyscallN(c.vtable[slot], values...)
	if int32(result) < 0 {
		return fmt.Errorf("WebView2 HRESULT 0x%08x", uint32(result))
	}
	return nil
}

func (c *webCOM) text(slot int) string {
	var value *uint16
	if err := c.call(slot, uintptr(unsafe.Pointer(&value))); err != nil || value == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value)
}

type controllerHandler interface {
	controllerCompleted(uintptr, *webCOM) uintptr
}
type navigationHandler interface {
	navigationStarting(*webCOM, *webCOM) uintptr
}
type completedHandler interface {
	navigationCompleted(*webCOM, *webCOM) uintptr
}
type popupHandler interface {
	newWindowRequested(*webCOM, *webCOM) uintptr
}

func init() {
	combridge.RegisterVTable[combridge.IUnknown, controllerHandler]("{6C4819F3-C9B7-4260-8127-C9F5BDE7F68C}",
		func(this, result uintptr, controller *webCOM) uintptr {
			return combridge.Resolve[controllerHandler](this).controllerCompleted(result, controller)
		})
	combridge.RegisterVTable[combridge.IUnknown, navigationHandler]("{9ADBE429-F36D-432B-9DDC-F8881FBD76E3}",
		func(this uintptr, sender, args *webCOM) uintptr {
			return combridge.Resolve[navigationHandler](this).navigationStarting(sender, args)
		})
	combridge.RegisterVTable[combridge.IUnknown, completedHandler]("{D33A35BF-1C49-4F98-93AB-006E0533FE1C}",
		func(this uintptr, sender, args *webCOM) uintptr {
			return combridge.Resolve[completedHandler](this).navigationCompleted(sender, args)
		})
	combridge.RegisterVTable[combridge.IUnknown, popupHandler]("{D4C185FE-C81C-4989-97AF-2D3FA7AB5651}",
		func(this uintptr, sender, args *webCOM) uintptr {
			return combridge.Resolve[popupHandler](this).newWindowRequested(sender, args)
		})
}

type managementWindow struct {
	window                           uintptr
	tray                             *desktopTray
	controller, webview, environment *webCOM
	baseURL, pendingURL              string
	err                              error
	closed                           bool
}

func (t *desktopTray) showManagement() {
	if t.manager != nil && !t.manager.closed {
		t.manager.show()
		return
	}
	m := &managementWindow{tray: t, baseURL: managementURL(t.address)}
	t.manager = m
	if err := m.create(); err != nil {
		m.close()
		desktopError("无法打开管理窗口，DNS 和托盘继续运行", err)
	}
}

func (m *managementWindow) create() error {
	version, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err != nil || version == "" {
		return fmt.Errorf("未找到可用的 Microsoft Edge WebView2 Runtime。请安装 Evergreen Runtime 后从托盘重试。\nhttps://developer.microsoft.com/microsoft-edge/webview2/\n%v", err)
	}
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
	class := windowClass{Instance: instance, ClassName: wide(managementClass), Procedure: windows.NewCallback(m.procedure), Icon: m.tray.largeIcon, SmallIcon: m.tray.icon.Icon, Cursor: cursor, Background: 6}
	class.Size = uint32(unsafe.Sizeof(class))
	atom, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return fmt.Errorf("注册管理窗口失败: %w", err)
	}
	width, height := uintptr(1200), uintptr(800)
	var work [4]int32
	user32.NewProc("SystemParametersInfoW").Call(0x30, 0, uintptr(unsafe.Pointer(&work)), 0)
	if work[2]-work[0] > 0 && work[3]-work[1] > 0 {
		width = min(width, uintptr(work[2]-work[0])*9/10)
		height = min(height, uintptr(work[3]-work[1])*9/10)
	}
	x, y := (int64(work[2])+int64(work[0])-int64(width))/2, (int64(work[3])+int64(work[1])-int64(height))/2
	m.window, _, err = user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class.ClassName)), uintptr(unsafe.Pointer(wide("DNS Assistant"))), 0x00cf0000, uintptr(x), uintptr(y), width, height, 0, 0, instance, 0)
	if m.window == 0 {
		return fmt.Errorf("创建管理窗口失败: %w", err)
	}
	m.show()
	// Keep the browser cache outside the service data: runtime cache files can
	// remain locked briefly after Close and must not break cold database backups.
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	cache := filepath.Join(cacheRoot, "DNS Assistant", "WebView2", fmt.Sprintf("%x", sha256.Sum256([]byte(m.tray.options.Data))))
	if err := os.MkdirAll(cache, 0700); err != nil {
		return err
	}
	user32.NewProc("SetTimer").Call(m.window, 1, 30000, 0)
	return webviewloader.CreateCoreWebView2EnvironmentWithOptions(m, webviewloader.WithUserDataFolder(cache), webviewloader.WithLanguage("zh-CN"))
}

func (m *managementWindow) EnvironmentCompleted(result webviewloader.HRESULT, environment *webviewloader.ICoreWebView2Environment) webviewloader.HRESULT {
	if m.closed {
		return 0
	}
	if result < 0 || environment == nil {
		m.fail(fmt.Errorf("创建网页环境失败: 0x%08x", uint32(result)))
		return 0
	}
	m.environment = (*webCOM)(unsafe.Pointer(environment))
	m.environment.call(1) // AddRef: callback arguments are borrowed.
	handler := combridge.New[controllerHandler](m)
	defer handler.Close()
	if err := m.environment.call(3, m.window, handler.Ref()); err != nil {
		m.fail(err)
	}
	return 0
}

func (m *managementWindow) controllerCompleted(result uintptr, controller *webCOM) uintptr {
	if m.closed {
		if controller != nil {
			controller.call(24)
		}
		return 0
	}
	if int32(result) < 0 || controller == nil {
		m.fail(fmt.Errorf("创建网页控件失败: 0x%08x", uint32(result)))
		return 0
	}
	m.controller = controller
	controller.call(1)
	if err := controller.call(25, uintptr(unsafe.Pointer(&m.webview))); err != nil {
		m.fail(err)
		return 0
	}
	var settings *webCOM
	if err := m.webview.call(3, uintptr(unsafe.Pointer(&settings))); err != nil {
		m.fail(err)
		return 0
	}
	defer settings.call(2)
	// No native RPC or external host objects; keep standard script dialogs and
	// downloads available for the existing backup/restore interface.
	for _, slot := range []int{6, 10, 12, 14, 16} {
		if err := settings.call(slot, 0); err != nil {
			m.fail(err)
			return 0
		}
	}
	starting := combridge.New[navigationHandler](m)
	defer starting.Close()
	completed := combridge.New[completedHandler](m)
	defer completed.Close()
	popup := combridge.New[popupHandler](m)
	defer popup.Close()
	for _, event := range []struct {
		slot     int
		callback uintptr
	}{{7, starting.Ref()}, {15, completed.Ref()}, {44, popup.Ref()}} {
		var token int64
		if err := m.webview.call(event.slot, event.callback, uintptr(unsafe.Pointer(&token))); err != nil {
			m.fail(err)
			return 0
		}
	}
	user32.NewProc("KillTimer").Call(m.window, 1)
	m.resize()
	visible, _, _ := user32.NewProc("IsWindowVisible").Call(m.window)
	controller.call(4, visible)
	if err := m.webview.call(5, uintptr(unsafe.Pointer(wide(m.baseURL)))); err != nil {
		m.fail(err)
	}
	return 0
}

func (m *managementWindow) navigationStarting(_ *webCOM, args *webCOM) uintptr {
	// Never hand protocols to an external browser/application. Ordinary project
	// links stay in this window; reopening from the tray returns to management.
	address := args.text(3)
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && address != "about:blank") {
		args.call(8, 1) // put_Cancel
	}
	return 0
}

func (m *managementWindow) navigationCompleted(_ *webCOM, args *webCOM) uintptr {
	var success int32
	args.call(3, uintptr(unsafe.Pointer(&success)))
	if success != 0 && !m.closed {
		slog.Info("管理窗口页面已加载")
		user32.NewProc("SetPropW").Call(m.window, uintptr(unsafe.Pointer(wide("DNSAssistant.WebViewReady"))), 1)
	}
	return 0
}

func (m *managementWindow) newWindowRequested(_ *webCOM, args *webCOM) uintptr {
	args.call(6, 1) // put_Handled: suppress the runtime's separate popup window.
	m.pendingURL = args.text(3)
	user32.NewProc("PostMessageW").Call(m.window, wmManagementNavigate, 0, 0)
	return 0
}

func (m *managementWindow) fail(err error) {
	if m.closed || m.err != nil {
		return
	}
	m.err = err
	user32.NewProc("PostMessageW").Call(m.window, wmManagementError, 0, 0)
}

func (m *managementWindow) resize() {
	if m.controller == nil {
		return
	}
	var rect [4]int32
	user32.NewProc("GetClientRect").Call(m.window, uintptr(unsafe.Pointer(&rect)))
	if err := m.controller.call(6, uintptr(unsafe.Pointer(&rect))); err != nil {
		m.fail(err)
	}
}

func (m *managementWindow) show() {
	state, _, _ := user32.NewProc("IsIconic").Call(m.window)
	mode := uintptr(5) // SW_SHOW preserves maximized state.
	if state != 0 {
		mode = 9
	}
	user32.NewProc("ShowWindow").Call(m.window, mode)
	user32.NewProc("SetForegroundWindow").Call(m.window)
	if m.controller != nil {
		m.controller.call(4, 1)
		m.controller.call(12, 0) // MoveFocus(PROGRAMMATIC)
		u, _ := url.Parse(m.webview.text(4))
		base, _ := url.Parse(m.baseURL)
		if u == nil || u.Scheme != base.Scheme || u.Host != base.Host {
			m.webview.call(5, uintptr(unsafe.Pointer(wide(m.baseURL))))
		}
	}
}

func (m *managementWindow) close() {
	if m.closed {
		return
	}
	m.closed = true
	if m.window != 0 {
		user32.NewProc("KillTimer").Call(m.window, 1)
	}
	if m.controller != nil {
		m.controller.call(24)
		m.controller.call(2)
		m.controller = nil
	}
	if m.webview != nil {
		m.webview.call(2)
		m.webview = nil
	}
	if m.environment != nil {
		m.environment.call(2)
		m.environment = nil
	}
	if m.window != 0 {
		user32.NewProc("DestroyWindow").Call(m.window)
		m.window = 0
	}
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(wide(managementClass))), instance)
}

func (m *managementWindow) procedure(window uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case 0x10: // Closing management hides it; only the tray Exit stops DNS.
		if m.controller != nil {
			m.controller.call(4, 0)
		}
		user32.NewProc("ShowWindow").Call(window, 0)
		return 0
	case 0x5:
		m.resize()
	case 0x7:
		if m.controller != nil {
			m.controller.call(12, 0)
		}
	case 0x3:
		if m.controller != nil {
			m.controller.call(23)
		}
	case 0x2e0: // WM_DPICHANGED
		var rect [4]int32
		kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&rect)), lparam, unsafe.Sizeof(rect))
		user32.NewProc("SetWindowPos").Call(window, 0, uintptr(rect[0]), uintptr(rect[1]), uintptr(rect[2]-rect[0]), uintptr(rect[3]-rect[1]), 0x14)
		m.resize()
	case 0x113:
		m.fail(fmt.Errorf("管理窗口初始化超时，请从托盘重试；必要时修复 Microsoft Edge WebView2 Runtime"))
	case wmManagementNavigate:
		if !m.closed && m.webview != nil {
			if err := m.webview.call(5, uintptr(unsafe.Pointer(wide(m.pendingURL)))); err != nil {
				m.fail(err)
			}
		}
		return 0
	case wmManagementError:
		err := m.err
		m.close()
		desktopError("管理窗口启动失败，DNS 和托盘继续运行", err)
		return 0
	}
	result, _, _ := user32.NewProc("DefWindowProcW").Call(window, uintptr(message), wparam, lparam)
	return result
}
