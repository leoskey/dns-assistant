package main

import (
	"context"
	"dns-assistant/internal/app"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "autostart" {
		args, err := loadStartupArguments()
		if err != nil {
			desktopError("无法读取开机启动配置", err)
			return
		}
		os.Args = append(os.Args[:1], args...)
	}
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "standalone")
	}
	command := os.Args[1]
	if command != "standalone" {
		attachCommandConsole()
	}
	if command == "version" {
		fmt.Printf("DNS Assistant v%s (%s)\n", app.Version(), app.Revision)
		return
	}
	if command == "config" {
		json.NewEncoder(os.Stdout).Encode(app.DefaultConfig())
		return
	}
	if command == "health" {
		if len(os.Args) > 2 && os.Args[2] == "dns" {
			if e := app.QueryHealth(env("DNS_HEALTH_ADDR", "127.0.0.1:5353")); e != nil {
				slog.Error("DNS 未就绪", "error", e)
				os.Exit(1)
			}
		} else {
			client := &http.Client{Timeout: 2 * time.Second}
			r, e := client.Get("http://127.0.0.1:8080/healthz")
			if e != nil || r.StatusCode != 200 {
				os.Exit(1)
			}
			r.Body.Close()
		}
		return
	}
	if command != "standalone" {
		slog.Error("请直接启动程序，DNS、代理与管理已合并运行", "command", command)
		os.Exit(2)
	}
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	executable, e := os.Executable()
	if e != nil {
		slog.Error("无法确定程序目录", "error", e)
		os.Exit(1)
	}
	dir := flags.String("data", env("DATA_DIR", standaloneDataDir(executable)), "数据目录")
	addr := flags.String("listen", env("LISTEN_ADDR", ":53"), "DNS 监听地址")
	ip := flags.String("ip", env("SERVER_IP", ""), "服务主机固定局域网 IPv4，用于接入指引")
	browser := flags.Bool("open-window", runtime.GOOS == "windows", "启动后显示管理窗口（Windows）")
	flags.BoolVar(browser, "open-browser", runtime.GOOS == "windows", "兼容旧参数；Windows 显示程序内管理窗口")
	tray := flags.Bool("tray", runtime.GOOS == "windows", "显示 Windows 托盘菜单")
	web := flags.String("web", env("WEB_ADDR", ":8080"), "管理页面监听地址")
	proxy := flags.String("proxy", ":8082", "下载代理监听地址")
	flags.Parse(os.Args[2:])
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if !*tray {
		release, err := enableServiceConsole(cancel)
		if err != nil {
			desktopError("无法打开控制台", err)
			return
		}
		defer release()
	}
	unlock, err := lockStandalone(filepath.Dir(executable))
	if err != nil {
		if *tray && activateDesktop(executable, *browser) {
			return
		}
		if *tray {
			desktopError("程序已运行、正在更新或目录不可写，请检查右下角托盘", err)
		} else {
			slog.Error("程序已运行、正在更新或目录不可写", "error", err)
		}
		os.Exit(1)
	}
	defer unlock()
	if *ip != "" {
		address := net.ParseIP(*ip)
		if address == nil || address.To4() == nil || !address.IsGlobalUnicast() || address.IsLoopback() {
			slog.Error("-ip 必须是服务主机固定局域网 IPv4")
			os.Exit(2)
		}
		os.Setenv("SERVER_IP", *ip)
	}
	absoluteDir, e := filepath.Abs(*dir)
	if e != nil {
		desktopError("无法读取数据目录", e)
		return
	}
	options := desktopOptions{Executable: executable, Data: absoluteDir, DNS: *addr, Web: *web, Proxy: *proxy, IP: *ip, OpenBrowser: *browser}
	if e := runDesktop(ctx, options, *tray); e != nil {
		slog.Error("单机服务退出", "error", e)
		if *tray {
			desktopError("单机服务退出", e)
		}
		os.Exit(1)
	}
}
