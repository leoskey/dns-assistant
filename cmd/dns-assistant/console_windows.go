package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"golang.org/x/sys/windows"
)

// Normal tray launches never call this: the GUI subsystem creates no console.
// Explicit command-line use may attach to an existing terminal without creating one.
func attachCommandConsole() { attachConsole(false) }

func attachConsole(create bool) bool {
	streams := []struct {
		id   uint32
		file **os.File
	}{
		{windows.STD_INPUT_HANDLE, &os.Stdin},
		{windows.STD_OUTPUT_HANDLE, &os.Stdout},
		{windows.STD_ERROR_HANDLE, &os.Stderr},
	}
	var saved [3]windows.Handle
	for i, stream := range streams {
		handle := windows.Handle((*stream.file).Fd())
		if _, err := windows.GetFileType(handle); err == nil {
			saved[i] = handle
		}
	}
	attached, _, err := kernel32.NewProc("AttachConsole").Call(uintptr(uint32(0xffffffff)))
	if attached == 0 && err != windows.ERROR_ACCESS_DENIED {
		if !create {
			return false
		}
		attached, _, _ = kernel32.NewProc("AllocConsole").Call()
		if attached == 0 {
			return false
		}
	}
	for i, stream := range streams {
		if saved[i] != 0 {
			windows.SetStdHandle(stream.id, saved[i])
			continue
		}
		if handle, err := windows.GetStdHandle(stream.id); err == nil && handle != 0 {
			*stream.file = os.NewFile(uintptr(handle), (*stream.file).Name())
		}
	}
	log.SetOutput(os.Stderr)
	return true
}

func enableServiceConsole(cancel context.CancelFunc) (func(), error) {
	if !attachConsole(true) {
		return nil, fmt.Errorf("Windows 无法连接或创建控制台")
	}
	// Attaching/allocating a console resets Windows control handlers, including
	// Go's startup handler. Reinstall graceful Ctrl+C/Ctrl+Break for console mode.
	handler := windows.NewCallback(func(event uint32) uintptr {
		if event == 0 || event == 1 {
			cancel()
			return 1
		}
		return 0
	})
	result, _, err := kernel32.NewProc("SetConsoleCtrlHandler").Call(handler, 1)
	if result == 0 {
		return nil, fmt.Errorf("注册控制台退出处理失败: %v", err)
	}
	return func() { kernel32.NewProc("SetConsoleCtrlHandler").Call(handler, 0) }, nil
}
