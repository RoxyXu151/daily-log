package mobilehost

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daily-log/internal/config"
	"daily-log/internal/mobileapi"
	"daily-log/internal/model"
	"daily-log/internal/runtimeapp"
	"daily-log/internal/workspace"
)

type originTransport struct {
	origin string
	next   http.RoundTripper
}

func (t originTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	if copy.Header.Get("Origin") == "" && r.Method == http.MethodPost {
		copy.Header.Set("Origin", t.origin)
	}
	return t.next.RoundTrip(copy)
}

func TestPairingAndCrossClientEditing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	configuration, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	configuration = []byte(strings.Replace(string(configuration), "excel:\n  enabled: false", "excel:\n  enabled: true", 1))
	if err := os.WriteFile(path, configuration, 0600); err != nil {
		t.Fatal(err)
	}
	// Force one output to fail while keeping native Markdown export usable.
	lockDir := filepath.Join(filepath.Dir(path), "outputs", "local")
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, "~$个人活动日志.xlsx"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	desktop := runtimeapp.Workspace{ConfigPath: path}
	host, code, err := New(desktop, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("application"))
	}))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(host)
	defer server.Close()
	httpClient := server.Client()
	httpClient.Jar, _ = cookiejar.New(nil)
	httpClient.Transport = originTransport{server.URL, httpClient.Transport}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	api := mobileapi.Client{BaseURL: server.URL, HTTP: httpClient}
	if _, err := api.Overview(ctx); !errors.Is(err, mobileapi.ErrSession) {
		t.Fatalf("unpaired API response = %v", err)
	}
	pair := func(origin string) *http.Response {
		t.Helper()
		body := url.Values{"code": {code}}.Encode()
		req, err := http.NewRequest(http.MethodPost, server.URL+"/pair", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := httpClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	if res := pair("https://different.example"); res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin pair response = %d", res.StatusCode)
	}
	res := pair(server.URL)
	cookies := res.Cookies()
	if res.StatusCode != http.StatusSeeOther || len(cookies) != 1 || !cookies[0].Secure ||
		!cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("pair response = %d; unexpected cookie policy", res.StatusCode)
	}
	if res := pair(server.URL); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pairing code could be reused: %d", res.StatusCode)
	}
	day := model.Day(time.Now()).Format(model.DateLayout)
	before, err := api.Journal(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	first, err := desktop.Save(ctx, day, before.View.Revision, model.JournalText{Morning: "桌面先保存"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Save(ctx, day, before.View.Revision, model.JournalText{Morning: "移动端的旧版本"}, true); !errors.Is(err, model.ErrJournalConflict) {
		t.Fatalf("stale mobile save = %v", err)
	}
	mobile, err := api.Save(ctx, day, first.Revision, model.JournalText{Morning: "移动端保存的合并正文"}, false)
	if err != nil || mobile.Record.Status != "已生成" {
		t.Fatalf("mobile save = %v", err)
	}
	observed, err := desktop.Journal(ctx, day)
	if err != nil || observed.View.Revision != mobile.Revision ||
		model.JournalTextFrom(observed.View.Record.Payload).Morning != "移动端保存的合并正文" {
		t.Fatalf("desktop did not see the saved mobile revision: %v", err)
	}
	if err := api.Export(ctx, "excel"); err == nil {
		t.Fatal("locked Excel output should fail")
	}
	if err := api.Export(ctx, "markdown"); err != nil {
		t.Fatalf("local export depended on the failed Excel output: %v", err)
	}
	for _, suffix := range []string{".md", ".json"} {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "outputs", "markdown", day+suffix))
		if err != nil || !strings.Contains(string(data), "移动端保存的合并正文") {
			t.Fatalf("saved journal missing from %s export: %v", suffix, err)
		}
	}
	overview, err := api.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{}
	for _, output := range overview.Outputs {
		outputs[output.Name] = output.Status
		if output.Name == "markdown" && (output.Summary == nil || output.Summary.Files != 2) {
			t.Fatalf("missing Markdown export count: %+v", output.Summary)
		}
	}
	if outputs["markdown"] != "success" || outputs["excel"] != "error" {
		t.Fatalf("expected independent export states, got %+v", outputs)
	}
	if _, err := api.TestConnection(ctx, workspace.ConnectionInput{ID: "codex", Values: map[string]string{"hosts": "[]"}}); err == nil {
		t.Fatal("mobile client could edit a host command")
	}
	list, err := api.Connections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	protected := 0
	for _, connection := range list {
		for _, field := range connection.Fields {
			if field.ReadOnly {
				protected++
				if field.Value != "" {
					t.Fatal("mobile interface exposed a protected value")
				}
			}
		}
	}
	if protected < 5 {
		t.Fatal("missing remote field restrictions")
	}
	if err := api.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Overview(ctx); !errors.Is(err, mobileapi.ErrSession) {
		t.Fatalf("API remained accessible after logout: %v", err)
	}
	server.Close()
	if _, err := api.Overview(ctx); !errors.Is(err, mobileapi.ErrDisconnected) {
		t.Fatalf("disconnection error = %v", err)
	}
}

func TestAssetServerOnlyExposesBuiltFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"index.html", "wasm_exec.js", "daily-log.0123456789ab.wasm", ".env", "data.sqlite3", "secret.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := Assets(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, status := range map[string]int{
		"/": http.StatusOK, "/daily-log.0123456789ab.wasm": http.StatusOK,
		"/.env": http.StatusNotFound, "/data.sqlite3": http.StatusNotFound, "/secret.txt": http.StatusNotFound,
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, name, nil))
		if w.Code != status {
			t.Fatalf("%s = %d, want %d", name, w.Code, status)
		}
	}
}
