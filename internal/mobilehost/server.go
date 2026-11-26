package mobilehost

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"daily-log/internal/workspace"
)

type Options struct {
	Listen, Assets, Certificate, Key string
}

type Server struct {
	*Host
	listener net.Listener
	server   *http.Server
	ctx      context.Context
}

func Open(ctx context.Context, backend workspace.Backend, options Options) (*Server, string, error) {
	if options.Certificate == "" || options.Key == "" {
		return nil, "", errors.New("移动访问需要 HTTPS 证书和私钥")
	}
	assets, err := Assets(options.Assets)
	if err != nil {
		return nil, "", err
	}
	cert, err := tls.LoadX509KeyPair(options.Certificate, options.Key)
	if err != nil {
		return nil, "", err
	}
	host, code, err := New(backend, assets)
	if err != nil {
		return nil, "", err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", options.Listen)
	if err != nil {
		return nil, "", err
	}
	server := &http.Server{
		Handler:           host,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}},
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 32 << 10,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	return &Server{Host: host, listener: listener, server: server, ctx: ctx}, code, nil
}

func (s *Server) Addr() net.Addr { return s.listener.Addr() }

func (s *Server) Close() error { return s.listener.Close() }

func (s *Server) Serve() error {
	defer s.Close()
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-s.ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if s.server.Shutdown(shutdown) != nil {
				_ = s.server.Close()
			}
		case <-done:
		}
	}()
	err := s.server.ServeTLS(s.listener, "", "")
	close(done)
	<-stopped
	if errors.Is(err, http.ErrServerClosed) && s.ctx.Err() != nil {
		return nil
	}
	return err
}

func DefaultAssets(executable string) string {
	if executable != "" {
		dir := filepath.Dir(executable)
		for _, candidate := range []string{filepath.Join(dir, "web"), filepath.Join(dir, "..", "web")} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
	}
	return filepath.Join("dist", "desktop", "web")
}
