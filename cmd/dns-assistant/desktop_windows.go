package main

import (
	"golang.org/x/sys/windows"
	"path/filepath"
)

// FileShare.None in update.ps1 uses the same exclusive Windows file handle.
// Closing the handle (including a crash) releases the lock; the file may remain.
func lockStandalone(dir string) (func(), error) {
	path, err := windows.UTF16PtrFromString(filepath.Join(dir, ".dns-assistant.lock"))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return func() { windows.CloseHandle(handle) }, nil
}
