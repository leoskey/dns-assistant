package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type desktopOptions struct {
	Executable  string
	Data        string
	DNS         string
	Web         string
	Proxy       string
	IP          string
	OpenBrowser bool
}

func (o desktopOptions) arguments() []string {
	args := []string{"standalone", "-data", o.Data, "-listen", o.DNS, "-web", o.Web, "-proxy", o.Proxy, "-tray=true", "-open-browser=false"}
	if o.IP != "" {
		args = append(args, "-ip", o.IP)
	}
	return args
}

func loadStartupArguments() ([]string, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(executable), ".startup.json"))
	if err != nil {
		return nil, err
	}
	var options desktopOptions
	if err = json.Unmarshal(b, &options); err != nil {
		return nil, err
	}
	if options.Data == "" || options.DNS == "" || options.Web == "" || options.Proxy == "" {
		return nil, fmt.Errorf("开机启动配置不完整，请从托盘重新开启")
	}
	return options.arguments(), nil
}

func runDesktopService(ctx context.Context, o desktopOptions, ready ...func(string)) error {
	if o.OpenBrowser && runtime.GOOS != "windows" {
		ready = append(ready, func(address string) { openManagement(ctx, address) })
	}
	return runStandalone(ctx, o.Data, o.DNS, o.Web, o.Proxy, ready...)
}

const latestReleaseURL = "https://api.github.com/repos/leoskey/dns-assistant/releases/latest"

func checkDesktopUpdate(ctx context.Context, client *http.Client, endpoint, current string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "DNS-Assistant-Updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("更新服务返回 HTTP %d，请稍后重试", response.StatusCode)
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil {
		return "", err
	}
	if release.Draft || release.Prerelease || !strings.HasPrefix(release.Tag, "v") {
		return "", fmt.Errorf("正式版信息无效")
	}
	comparison, err := compareVersions(strings.TrimPrefix(release.Tag, "v"), current)
	if err != nil {
		return "", err
	}
	if comparison > 0 {
		return release.Tag, nil
	}
	return "", nil
}

func compareVersions(a, b string) (int, error) {
	parse := func(s string) ([3]uint64, error) {
		var result [3]uint64
		parts := strings.Split(s, ".")
		if len(parts) != 3 {
			return result, fmt.Errorf("无效版本号 %q", s)
		}
		for i, part := range parts {
			value, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				return result, fmt.Errorf("无效版本号 %q", s)
			}
			result[i] = value
		}
		return result, nil
	}
	av, err := parse(a)
	if err != nil {
		return 0, err
	}
	bv, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range av {
		if av[i] > bv[i] {
			return 1, nil
		}
		if av[i] < bv[i] {
			return -1, nil
		}
	}
	return 0, nil
}

func standaloneDataDir(executable string) string {
	dir := filepath.Dir(executable)
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(filepath.Join(dir, "installed.flag")); err == nil && os.Getenv("LOCALAPPDATA") != "" {
			return filepath.Join(os.Getenv("LOCALAPPDATA"), "DNS Assistant", "data")
		}
	}
	return filepath.Join(dir, "data")
}

func managementURL(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func openManagement(ctx context.Context, address string) {
	url := managementURL(address)
	if url == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", url+"/healthz", nil)
		response, err := client.Do(req)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		return // Windows management is owned by the native desktop window.
	case "darwin":
		command = exec.Command("open", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Run(); err != nil {
		slog.Warn("无法自动打开浏览器，请访问管理地址", "url", url, "error", err)
	}
}
