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

func runStandalone(parent context.Context, dir, dnsAddr, webAddr, proxyAddr string, ready ...func(string)) (err error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	store, err := app.OpenStore(filepath.Join(dir, "controller"))
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer store.Close()
	web, err := net.Listen("tcp4", webAddr)
	if err != nil {
		return fmt.Errorf("监听管理端口 %s: %w", webAddr, err)
	}
	defer web.Close()
	var m *app.Manager
	d := app.NewDNS(dnsAddr, filepath.Join(dir, "dns"), func(_ context.Context, q app.Query) { m.Observe(q) })
	m, err = app.NewManager(store, d)
	if err != nil {
		return err
	}
	if err = d.Start(ctx); err != nil {
		return fmt.Errorf("监听 DNS %s: %w", dnsAddr, err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if closeErr := d.Close(closeCtx); err == nil {
			err = closeErr
		}
	}()
	api := app.NewAPI(m)
	errs := make(chan error, 2)
	managerDone := make(chan struct{})
	go func() { m.Run(ctx); close(managerDone) }()
	go func() { errs <- app.ServeListener(ctx, web, api.Handler()) }()
	go func() { errs <- app.ServeDownloadProxy(ctx, proxyAddr, m.DownloadProxy) }()
	slog.Info("单机服务已启动，按 Ctrl+C 停止", "dns", dnsAddr, "web", web.Addr(), "data", dir)
	for _, notify := range ready {
		go notify(web.Addr().String())
	}
	remaining := 2
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
