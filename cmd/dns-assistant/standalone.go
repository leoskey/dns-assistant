package main

import (
	"context"
	"dns-assistant/internal/app"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"time"
)

func runStandalone(parent context.Context, dir, dnsAddr, webAddr string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	store, err := app.OpenStore(filepath.Join(dir, "controller"))
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer store.Close()
	control, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer control.Close()
	internal, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer internal.Close()
	web, err := net.Listen("tcp4", webAddr)
	if err != nil {
		return fmt.Errorf("监听管理端口 %s: %w", webAddr, err)
	}
	defer web.Close()
	// 单机内部通信仅绑定环回随机端口，不占用容器模式的固定控制端口。
	token := app.NewKey()
	d := app.NewDNS(dnsAddr, filepath.Join(dir, "dns"), token, "http://"+internal.Addr().String())
	m, err := app.NewManager(store, "http://"+control.Addr().String(), token)
	if err != nil {
		return err
	}
	if err = d.Start(ctx); err != nil {
		return fmt.Errorf("监听 DNS %s: %w", dnsAddr, err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		d.Close(closeCtx)
	}()
	api := app.NewAPI(m)
	errs := make(chan error, 3)
	managerDone := make(chan struct{})
	go func() { m.Run(ctx); close(managerDone) }()
	go func() { errs <- app.ServeListener(ctx, control, d.Handler()) }()
	go func() { errs <- app.ServeListener(ctx, internal, api.Internal()) }()
	go func() { errs <- app.ServeListener(ctx, web, api.Handler()) }()
	slog.Info("单机服务已启动，按 Ctrl+C 停止", "dns", dnsAddr, "web", web.Addr(), "data", dir)
	remaining := 3
	select {
	case <-ctx.Done():
	case err = <-errs:
		remaining--
	}
	cancel()
	for range remaining {
		if serveErr := <-errs; err == nil {
			err = serveErr
		}
	}
	<-managerDone
	return err
}
