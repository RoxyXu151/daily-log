//go:build !js

package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"

	"daily-log/internal/mobilehost"
	"daily-log/internal/workspace"
)

func startMobileAccess(parent context.Context, backend workspace.Backend, form mobileAccessOptions) (*mobileAccessSession, error) {
	listen := strings.TrimSpace(form.listen)
	host, _, err := net.SplitHostPort(listen)
	if err != nil || host == "" || net.ParseIP(host).IsUnspecified() {
		return nil, errors.New("请输入明确的主机地址和端口，例如 192.168.1.20:8443")
	}
	ctx, stop := context.WithCancel(parent)
	server, code, err := mobilehost.Open(ctx, backend, mobilehost.Options{
		Listen: listen, Assets: strings.TrimSpace(form.assets),
		Certificate: strings.TrimSpace(form.certificate), Key: strings.TrimSpace(form.key),
	})
	if err != nil {
		stop()
		return nil, err
	}
	if address, ok := server.Addr().(*net.TCPAddr); ok && address.IP.IsUnspecified() {
		_ = server.Close()
		stop()
		return nil, errors.New("主机地址须对应一张网卡")
	}
	_, port, err := net.SplitHostPort(server.Addr().String())
	if err != nil {
		_ = server.Close()
		stop()
		return nil, err
	}
	address := (&url.URL{Scheme: "https", Host: net.JoinHostPort(host, port)}).String()
	done := make(chan error, 1)
	go func() {
		defer stop()
		defer close(done)
		done <- server.Serve()
	}()
	return &mobileAccessSession{url: address, code: code, issueCode: server.IssueCode, stop: stop, done: done}, nil
}
