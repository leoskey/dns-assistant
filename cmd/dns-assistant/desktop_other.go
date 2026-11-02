//go:build !windows

package main

import (
	"context"
	"log/slog"
)

func lockStandalone(string) (func(), error) { return func() {}, nil }
func activateDesktop(string, bool) bool     { return false }

func runDesktop(ctx context.Context, o desktopOptions, tray bool) error {
	return runDesktopService(ctx, o)
}
func desktopError(message string, err error)                  { slog.Error(message, "error", err) }
func attachCommandConsole()                                   {}
func enableServiceConsole(context.CancelFunc) (func(), error) { return func() {}, nil }
