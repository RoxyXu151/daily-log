package runtimeapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"daily-log/internal/config"
	"daily-log/internal/model"
	"daily-log/internal/source"
	"daily-log/internal/workspace"
)

func TestConfiguredCredentialIsMaskedInDiagnosticsAndLocalExports(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	const id, key, token = "echo-test", "DAILY_LOG_ECHO_TEST_TOKEN", `echo-test:token+with"quotes`
	escaped := url.QueryEscape(token)
	var log bytes.Buffer
	w := Workspace{ConfigPath: path, Logger: slog.New(slog.NewTextHandler(&log, nil)), Connectors: []SourceRegistration{{
		Spec: config.SourceSpec{ID: id, Label: "回显测试", Fields: []config.Field{
			{Key: "token", Label: "令牌", Kind: "secret", SecretKey: key},
		}},
		Open: func(cfg config.Config) (source.Collector, Probe, error) {
			value := cfg.SourceSecret(id, key)
			probe := func(context.Context) (string, error) { return "已连接 " + value, nil }
			collect := func(context.Context, source.Window) (source.Batch, error) {
				return source.Batch{}, fmt.Errorf("远端错误包含 %s 与 %s", value, url.QueryEscape(value))
			}
			return collect, probe, nil
		},
	}}}
	assertMasked := func(surface, text string) {
		t.Helper()
		if strings.Contains(text, token) || strings.Contains(text, escaped) ||
			!strings.Contains(text, "[已隐藏凭证]") {
			t.Fatalf("%s credential was not masked", surface)
		}
	}
	input := workspace.ConnectionInput{ID: id, Enabled: true, Secrets: map[string]string{"token": token}}
	probe, err := w.TestConnection(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	assertMasked("connection test", probe)
	if err := w.SaveConnection(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := w.Run(ctx); err == nil {
		t.Fatal("expected the synthetic collector to fail")
	}
	assertMasked("application log", log.String())
	overview, err := w.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The per-source failure is persisted masked on the source itself; the
	// job-level error only says that some required source failed.
	var sourceError string
	for _, item := range overview.Sources {
		if item.Name == id {
			sourceError = item.ErrorMessage
		}
	}
	assertMasked("persisted source error", sourceError)
	day := model.Day(time.Now()).Format(model.DateLayout)
	text := "保留的说明 " + token + " " + escaped
	// The run above already generated today's journal, so save against its
	// current revision rather than as a brand-new day.
	current, err := w.Journal(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	view, err := w.Save(ctx, day, current.View.Revision, model.JournalText{Morning: text}, true)
	if err != nil {
		t.Fatal(err)
	}
	if model.JournalTextFrom(view.Record.Payload).Morning != text {
		t.Fatal("export filtering affected the saved version")
	}
	if err := w.Export(ctx, "markdown"); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".md", ".json"} {
		data, err := os.ReadFile(filepath.Join(root, "outputs", "markdown", day+suffix))
		if err != nil {
			t.Fatal(err)
		}
		assertMasked(suffix, string(data))
	}
}

func TestRegisteredSourceUsesSharedSettingsAndActivityFlow(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	// Organizing a day needs a model: point the workspace at a stub that
	// answers with a valid digest.
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"summary\":\"完成样例文档的整理与核对，当天记录已可复查。\",\"work_items\":[]}"}}]}`)
	}))
	defer ai.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data,
		[]byte("ai:\n  enabled: false\n  base_url: https://api.deepseek.com\n  model: deepseek-flash"),
		[]byte("ai:\n  enabled: true\n  base_url: "+ai.URL+"/v1\n  model: test-model"), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".env"), []byte("AI_API_KEY=test-model-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const id, secretKey = "sample-adapter", "DAILY_LOG_SAMPLE_ADAPTER_TEST_KEY"
	w := Workspace{ConfigPath: path, Connectors: []SourceRegistration{{
		Spec: config.SourceSpec{ID: id, Label: "测试接入", Description: "验证已登记的办公来源", Fields: []config.Field{
			{Key: "topic", Label: "主题", Kind: "text"},
			{Key: "token", Label: "访问凭证", Kind: "secret", SecretKey: secretKey},
		}},
		Open: func(cfg config.Config) (source.Collector, Probe, error) {
			values, err := cfg.ConnectionValues(id)
			if err != nil {
				return nil, nil, err
			}
			probe := func(context.Context) (string, error) {
				if cfg.SourceSecret(id, secretKey) != "sample-token" {
					return "", errors.New("缺少测试凭证")
				}
				return "样例可读", nil
			}
			collect := func(ctx context.Context, window source.Window) (source.Batch, error) {
				if _, err := probe(ctx); err != nil {
					return source.Batch{}, err
				}
				event := model.NewEvent("sample-adapter:one", id, "one", "document_reviewed", values["topic"], model.Day(window.Until))
				return source.Batch{Events: []model.Event{event}, Details: model.Object{
					"effective_since": model.ISO(window.Since.Add(time.Hour)),
					"history_gap":     true, "history_limit_days": 7,
				}}, nil
			}
			return collect, probe, nil
		},
	}}}
	input := workspace.ConnectionInput{
		ID: id, Enabled: true, Values: map[string]string{"topic": "样例文档"}, Secrets: map[string]string{"token": "sample-token"},
	}
	if result, err := w.TestConnection(ctx, input); err != nil || result != "样例可读" {
		t.Fatalf("new source probe: %q, %v", result, err)
	}
	if err := w.SaveConnection(ctx, input); err != nil {
		t.Fatal(err)
	}
	connections, err := w.Connections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range connections {
		if entry.ID == id {
			found = entry.Enabled && entry.Fields[0].Value == "样例文档" && entry.Fields[1].Configured && entry.Fields[1].Value == ""
		}
	}
	if !found {
		t.Fatal("source metadata or private credential state did not reach the connection list")
	}
	for i := 0; i < 2; i++ {
		if err := w.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	activities, err := w.Activities(ctx, model.BrowseQuery{Source: id})
	if err != nil || len(activities.Rows) != 1 || activities.Rows[0].Reference != "one" || activities.Rows[0].Title != "样例文档" {
		t.Fatalf("registered source activity was not collected and deduplicated: %+v, %v", activities, err)
	}
	overview, err := w.Overview(ctx)
	if err != nil || len(overview.Recent) != 1 || overview.Recent[0].ID != activities.Rows[0].ID {
		t.Fatalf("recent activity was not available: %v", err)
	}
	record, err := w.Activity(ctx, overview.Recent[0].ID)
	if err != nil || record != activities.Rows[0] {
		t.Fatalf("direct activity lookup returned a different record: %+v, %v", record, err)
	}
	for _, item := range overview.Sources {
		if item.Name == id {
			attempt := item.Attempt
			if attempt == nil || !attempt.HasCounts || attempt.Returned != 1 || attempt.Changed != 0 ||
				!attempt.HistoryGap || attempt.HistoryLimitDays != 7 || attempt.RequestedSince == attempt.EffectiveSince {
				t.Fatalf("source diagnostics lost coverage or counts: %+v", attempt)
			}
		}
	}
	day := model.Day(time.Now()).Format(model.DateLayout)
	journal, err := w.Journal(ctx, day)
	if err != nil || len(journal.View.Record.Payload.List("evidence")) != 1 {
		t.Fatalf("registered source was not consumed by the journal rule: %v", err)
	}
	if err := w.RemoveConnection(ctx, id); err != nil {
		t.Fatal(err)
	}
	// A full run after removal must not collect the removed adapter again;
	// the history below stays exactly as it was.
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}
	activities, err = w.Activities(ctx, model.BrowseQuery{Source: id})
	if err != nil || len(activities.Rows) != 1 {
		t.Fatalf("removing the connection changed history: %+v, %v", activities, err)
	}
}

func TestWorkspaceCanReadAndRecoverDuringCollectionFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			if first.CompareAndSwap(false, true) {
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"id":"tester","is_premium":true}`)
		default:
			fmt.Fprint(w, `{"results":[]}`)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := Workspace{ConfigPath: path}
	if err := w.SaveConnection(ctx, workspace.ConnectionInput{
		ID: "todoist", Enabled: true, Values: map[string]string{"base_url": server.URL},
		Secrets: map[string]string{"token": "test-only"},
	}); err != nil {
		t.Fatal(err)
	}
	day := model.Day(time.Now()).Format(model.DateLayout)
	if _, err := w.Save(ctx, day, "new", model.JournalText{Morning: "保留的正文"}, true); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- w.Run(ctx) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("collection did not start")
	}
	overview, err := w.Overview(ctx)
	if err != nil || overview.JobStatus != "collecting" {
		t.Fatalf("unexpected collection progress: %q, %v", overview.JobStatus, err)
	}
	record, err := w.Journal(ctx, day)
	if err != nil || model.JournalTextFrom(record.View.Record.Payload).Morning != "保留的正文" {
		t.Fatalf("saved journal could not be read during collection: %v", err)
	}
	close(release)
	if err := <-result; err == nil {
		t.Fatal("the source failure was not reported")
	}
	overview, err = w.Overview(ctx)
	if err != nil || overview.JobStatus != "error" || overview.JobError == "" {
		t.Fatalf("failure was not persisted: %+v, %v", overview, err)
	}
	if err := w.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	overview, err = w.Overview(ctx)
	if err != nil || overview.JobStatus != "success" || overview.JobError != "" || overview.TodaySummary.Status != "已生成" {
		t.Fatalf("unexpected recovered workspace: %+v, %v", overview, err)
	}
}
