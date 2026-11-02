package main

import (
	"archive/zip"
	"dns-assistant/internal/app"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestTrayUpdateRestart(t *testing.T) {
	archive := os.Getenv("DNS_ASSISTANT_DESKTOP_PACKAGE")
	if archive == "" {
		t.Skip("set DNS_ASSISTANT_DESKTOP_PACKAGE for packaged update integration")
	}
	archive, err := filepath.Abs(archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, failChecksum := range []bool{false, true} {
		t.Run(fmt.Sprintf("checksumFailure=%v", failChecksum), func(t *testing.T) {
			root := t.TempDir()
			reader, err := zip.OpenReader(archive)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range reader.File {
				relative := strings.ReplaceAll(file.Name, `\`, "/")
				if !filepath.IsLocal(relative) {
					t.Fatal("unsafe fixture archive")
				}
				path := filepath.Join(root, relative)
				if file.FileInfo().IsDir() || strings.HasSuffix(relative, "/") {
					os.MkdirAll(path, 0700)
					continue
				}
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				source, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				target, err := os.Create(path)
				if err != nil {
					source.Close()
					t.Fatal(err)
				}
				_, err = io.Copy(target, source)
				target.Close()
				source.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			reader.Close()
			options := desktopOptions{Executable: filepath.Join(root, "dns-assistant.exe"), Data: filepath.Join(root, "data with spaces"), DNS: freeAddress(t), Web: freeAddress(t), Proxy: freeAddress(t)}
			store, err := app.OpenStore(filepath.Join(options.Data, "controller"))
			if err != nil {
				t.Fatal(err)
			}
			config := app.DefaultConfig()
			config.Enabled = false
			if err := store.Set("config", config); err != nil {
				t.Fatal(err)
			}
			store.Close()
			startup, err := json.Marshal(options)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".startup.json"), startup, 0600); err != nil {
				t.Fatal(err)
			}
			original := exec.Command(options.Executable, "autostart")
			if err := original.Start(); err != nil {
				t.Fatal(err)
			}
			defer original.Process.Kill()
			originalDone := make(chan error, 1)
			go func() { originalDone <- original.Wait() }()
			window := waitForTrayService(t, uint32(original.Process.Pid), options.Web)
			user32.NewProc("PostMessageW").Call(window, 0x111, menuOpen, 0)
			waitForManagementWindow(t, uint32(original.Process.Pid))
			defer cleanupManagementCache(t, options.Data)
			manifest := filepath.Join(filepath.Dir(archive), "SHA256SUMS")
			if failChecksum {
				manifest = filepath.Join(root, "bad-checksums")
				if err := os.WriteFile(manifest, []byte(strings.Repeat("0", 64)+"  "+filepath.Base(archive)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(windows.ComposeCommandLine(options.arguments())))
			helper := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(root, "update.ps1"), "-InstallDir", root, "-WaitForProcessId", strconv.Itoa(original.Process.Pid), "-Package", archive, "-Checksums", manifest, "-DataDir", options.Data, "-RestartArguments", encoded)
			helper.WaitDelay = 3 * time.Second
			type completion struct {
				output []byte
				err    error
			}
			helperDone := make(chan completion, 1)
			go func() { output, err := helper.CombinedOutput(); helperDone <- completion{output, err} }()
			select {
			case early := <-helperDone:
				t.Fatalf("updater did not wait for running app: %s %v", early.output, early.err)
			case <-time.After(300 * time.Millisecond):
			}
			user32.NewProc("PostMessageW").Call(window, 0x111, menuExit, 0)
			select {
			case err := <-originalDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("original did not stop")
			}
			var completed completion
			select {
			case completed = <-helperDone:
			case <-time.After(60 * time.Second):
				if helper.Process != nil {
					helper.Process.Kill()
				}
				t.Fatal("updater timed out")
			}
			match := regexp.MustCompile(`Restarted PID: (\d+)`).FindSubmatch(completed.output)
			if len(match) != 2 {
				t.Fatalf("no restarted process: %s %v", completed.output, completed.err)
			}
			pid, _ := strconv.Atoi(string(match[1]))
			restarted, err := os.FindProcess(pid)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Kill()
			if (completed.err != nil) != failChecksum {
				t.Fatalf("unexpected update result: %s %v", completed.output, completed.err)
			}
			window = waitForTrayService(t, uint32(pid), options.Web)
			user32.NewProc("PostMessageW").Call(window, 0x111, menuExit, 0)
			restartedDone := make(chan error, 1)
			go func() { _, err := restarted.Wait(); restartedDone <- err }()
			select {
			case err := <-restartedDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("restarted app did not exit")
			}
			store, err = app.OpenStore(filepath.Join(options.Data, "controller"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var saved app.Config
			if err := store.Get("config", &saved); err != nil || saved.Enabled {
				t.Fatal("data changed after update", err)
			}
			backups, _ := filepath.Glob(filepath.Join(root, ".update-backups", "*-data.zip"))
			if failChecksum {
				if len(backups) != 0 {
					t.Fatal("backup created before package verification")
				}
			} else {
				if len(backups) != 1 {
					t.Fatal("cold data backup missing")
				}
				backup, err := zip.OpenReader(backups[0])
				if err != nil {
					t.Fatal(err)
				}
				defer backup.Close()
				found := false
				for _, file := range backup.File {
					if file.Name == "controller/assistant.db" {
						found = true
					}
				}
				if !found {
					t.Fatal("database missing from data backup")
				}
			}
		})
	}
}

func waitForTrayService(t *testing.T, pid uint32, address string) uintptr {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		window := trayWindowForPID(pid)
		response, err := client.Get(managementURL(address) + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 && window != 0 {
				assertNoConsole(t, int(pid))
				return window
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("tray service did not become ready")
	return 0
}
