//go:build !js

package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daily-log/internal/config"
	"daily-log/internal/runtimeapp"

	"github.com/egoist/mygo/ui"
)

func TestDesktopMobileAccessLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	cfg := filepath.Join(root, "config.yaml")
	if _, err := config.Init(cfg); err != nil {
		t.Fatal(err)
	}
	backend := runtimeapp.Workspace{ConfigPath: cfg}
	testTLS := httptest.NewTLSServer(http.NotFoundHandler())
	client := testTLS.Client()
	client.Timeout = 10 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cert := testTLS.TLS.Certificates[0]
	testTLS.Close()
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	form := mobileAccessOptions{
		listen: "127.0.0.1:0", assets: root,
		certificate: filepath.Join(root, "certificate.pem"), key: filepath.Join(root, "key.pem"),
	}
	files := map[string][]byte{
		form.certificate:                                   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}),
		form.key:                                           pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
		filepath.Join(root, "index.html"):                  []byte("browser app"),
		filepath.Join(root, "wasm_exec.js"):                []byte("test"),
		filepath.Join(root, "daily-log.0123456789ab.wasm"): []byte("test"),
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	updates := make(chan func(), 4)
	s := &screen{
		ctx: ctx, backend: backend, mobileOptions: form, dispatch: func(fn func()) { updates <- fn },
		startMobile: func(options mobileAccessOptions) (*mobileAccessSession, error) {
			return startMobileAccess(ctx, backend, options)
		},
	}
	view := ui.NewTester(s.view, 1100, 800)
	click := func(label string) {
		t.Helper()
		if err := view.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	settle := func() {
		t.Helper()
		select {
		case fn := <-updates:
			fn()
			view.Frame()
		case <-time.After(15 * time.Second):
			t.Fatal("desktop did not report the server state")
		}
		if s.failed {
			t.Fatal(s.message)
		}
	}
	click("手机配对")
	click("启用手机访问")
	settle()
	session := s.mobileSession
	if session == nil || !view.HasText(session.url) {
		t.Fatal("mobile access did not expose the running address")
	}
	request := func(path, contentType, body string, cookie *http.Cookie, want int) *http.Cookie {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, session.url+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", session.url)
		req.Header.Set("Content-Type", contentType)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s returned %d, want %d", path, resp.StatusCode, want)
		}
		if cookies := resp.Cookies(); len(cookies) > 0 {
			return cookies[0]
		}
		return nil
	}
	pair := func(code string, want int) *http.Cookie {
		return request("/pair", "application/x-www-form-urlencoded", url.Values{"code": {code}}.Encode(), nil, want)
	}
	firstCode := session.code
	click("生成新配对码")
	pair(firstCode, http.StatusUnauthorized)
	first := pair(session.code, http.StatusSeeOther)
	pair(session.code, http.StatusUnauthorized)
	click("生成新配对码")
	request("/api/overview", "application/json", "null", first, http.StatusOK)
	second := pair(session.code, http.StatusSeeOther)
	request("/api/logout", "application/json", "null", first, http.StatusUnauthorized)
	request("/api/overview", "application/json", "null", second, http.StatusOK)
	click("停止手机访问")
	settle()
	if s.mobileSession != nil || !view.HasText("手机访问已停止") {
		t.Fatal("desktop did not clear the stopped session")
	}
	if resp, err := client.Get(session.url); err == nil {
		resp.Body.Close()
		t.Fatal("stopped mobile server remained reachable")
	}
}
