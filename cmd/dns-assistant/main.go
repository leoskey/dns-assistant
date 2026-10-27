package main

import (
	"context"
	"dns-assistant/internal/app"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: dns-assistant dns|controller|health|keygen|config|version")
		os.Exit(2)
	}
	command := os.Args[1]
	if command == "version" {
		fmt.Printf("DNS Assistant v%s (%s)\n", app.Version(), app.Revision)
		return
	}
	if command == "keygen" {
		fmt.Println(app.NewKey())
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
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	dir := flags.String("data", env("DATA_DIR", "data/"+command), "数据目录")
	addr := flags.String("listen", env("LISTEN_ADDR", ":5353"), "监听地址")
	flags.Parse(os.Args[2:])
	token := env("SERVICE_TOKEN", "")
	if f := os.Getenv("SERVICE_TOKEN_FILE"); f != "" {
		b, e := os.ReadFile(f)
		if e != nil {
			slog.Error("服务密钥读取失败", "error", e)
			os.Exit(1)
		}
		token = strings.TrimSpace(string(b))
	}
	if len(token) < 32 || strings.Contains(token, "replace-with") {
		slog.Error("请设置至少 32 字符的 SERVICE_TOKEN，使用 keygen 生成")
		os.Exit(1)
	}
	if e := os.MkdirAll(filepath.Clean(*dir), 0700); e != nil {
		slog.Error("数据目录不可写", "error", e)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	errs := make(chan error, 3)
	switch command {
	case "dns":
		d := app.NewDNS(*addr, *dir, token, env("MANAGER_INTERNAL_URL", "http://127.0.0.1:8081"))
		if e := d.Start(ctx); e != nil {
			slog.Error("DNS 启动失败", "error", e)
			os.Exit(1)
		}
		go func() { errs <- app.Serve(ctx, env("CONTROL_ADDR", ":9090"), d.Handler()) }()
		slog.Info("DNS 已启动", "address", *addr)
		select {
		case <-ctx.Done():
		case e := <-errs:
			if e != nil {
				slog.Error("服务退出", "error", e)
			}
			cancel()
		}
		closeCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		d.Close(closeCtx)
	case "controller":
		store, e := app.OpenStore(*dir)
		if e != nil {
			slog.Error("数据库启动失败", "error", e)
			os.Exit(1)
		}
		defer store.Close()
		m, e := app.NewManager(store, env("DNS_CONTROL_URL", "http://127.0.0.1:9090"), token)
		if e != nil {
			slog.Error("配置读取失败", "error", e)
			os.Exit(1)
		}
		api := app.NewAPI(m)
		managerDone := make(chan struct{})
		go func() { m.Run(ctx); close(managerDone) }()
		go func() { errs <- app.Serve(ctx, env("WEB_ADDR", ":8080"), api.Handler()) }()
		go func() { errs <- app.Serve(ctx, env("INTERNAL_ADDR", ":8081"), api.Internal()) }()
		go func() { errs <- app.ServeDownloadProxy(ctx, ":8082", m.DownloadProxy) }()
		slog.Info("管理服务已启动", "address", env("WEB_ADDR", ":8080"))
		select {
		case <-ctx.Done():
		case e := <-errs:
			if e != nil {
				slog.Error("服务退出", "error", e)
			}
			cancel()
		}
		<-managerDone

	default:
		slog.Error("未知子命令", "command", command)
		os.Exit(2)
	}
}
