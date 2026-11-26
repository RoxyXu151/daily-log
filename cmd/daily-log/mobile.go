package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"daily-log/internal/config"
	"daily-log/internal/mobilehost"
	"daily-log/internal/runtimeapp"
)

func serveMobile(ctx context.Context, o options, stdout, stderr io.Writer) error {
	if _, err := config.Load(o.config); err != nil {
		return err
	}
	backend := runtimeapp.Workspace{ConfigPath: o.config, Logger: slog.New(slog.NewTextHandler(stderr, nil))}
	server, code, err := mobilehost.Open(ctx, backend, mobilehost.Options{
		Listen: o.listen, Assets: o.assets, Certificate: o.certificate, Key: o.key,
	})
	if err != nil {
		return err
	}
	defer server.Close()
	if _, err := fmt.Fprintf(stdout, "HTTPS 已监听 %s\n一次性配对码（10 分钟有效）：%s\n", server.Addr(), code); err != nil {
		return err
	}
	return server.Serve()
}

func defaultMobileListen() string {
	if addr := os.Getenv("DAILY_LOG_MOBILE_LISTEN"); addr != "" {
		return addr
	}
	return "127.0.0.1:8443"
}

func defaultMobileAssets() string {
	executable, _ := os.Executable()
	return mobilehost.DefaultAssets(executable)
}
