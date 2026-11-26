package runtimeapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daily-log/internal/config"
	"daily-log/internal/model"
	"daily-log/internal/source"
	"daily-log/internal/workspace"
)

func TestSourceSettingsArePersistedAndSecretsStayPrivate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer source-test-secret" {
			http.Error(w, "wrong test input", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"id":"tester"}`)
	}))
	defer server.Close()
	app := Workspace{ConfigPath: path}
	input := workspace.ConnectionInput{ID: "todoist", Enabled: true,
		Values:  map[string]string{"base_url": server.URL},
		Secrets: map[string]string{"token": "source-test-secret"}}
	if _, err := app.TestConnection(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.SettingsPath(path)); !os.IsNotExist(err) {
		t.Fatalf("test wrote to settings: %v", err)
	}
	if err := app.SaveConnection(ctx, input); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Todoist.Enabled || cfg.Todoist.BaseURL != server.URL || cfg.Secret("TODOIST_API_TOKEN") != "source-test-secret" {
		t.Fatal("runtime did not load the saved source")
	}
	items, err := app.Connections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(items)
	if err != nil || strings.Contains(string(data), "source-test-secret") {
		t.Fatal("connection readback exposed a credential")
	}
	if err := app.RemoveConnection(ctx, input.ID); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(path)
	if err != nil || cfg.Todoist.Enabled || !cfg.Settings.Connections["todoist"].Removed || cfg.Secret("TODOIST_API_TOKEN") != "" {
		t.Fatalf("removed source still active: %v", err)
	}
}

func TestNextRunUsesNewlySelectedModel(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	today := model.Day(time.Now())
	stamp := model.ISO(today)
	evidence := "完成了一个用于模型路由验证的独立对话摘要。"
	at, err := model.ParseTime(stamp, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	event := model.NewEvent("fixture-model", "fixture", "model", "task_completed", "测试活动", at)
	event.Description, event.Metadata = evidence, model.Object{"important": true, "topic": "测试活动"}
	messages := make(chan string, 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string
			Messages []struct{ Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || r.URL.Path != "/chat/completions" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		messages <- request.Model
		content := `{}`
		if len(request.Messages) > 1 && strings.Contains(request.Messages[1].Content, "daily-log connection test") {
			content = `{"ok":true}`
		} else if len(request.Messages) > 0 && strings.Contains(request.Messages[0].Content, "今日概要") {
			content = `{"summary":"测试期间围绕当前目标推进了相关讨论，并形成了可回看的阶段性要点。","category_summaries":[{"category":"校园事务与个人生活","summary":"完成模型验证任务。"}],"work_items":[{"title":"完成模型验证任务","summary":"验证全局模型调用并记录结果。","category":"校园事务与个人生活","period":"晚上","evidence_ids":["fixture-model"]}]}`
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
	defer server.Close()

	app := Workspace{ConfigPath: path, Connectors: []SourceRegistration{fixtureRegistration(&event)}}
	if err := app.SaveConnection(ctx, workspace.ConnectionInput{ID: "fixture", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		input := workspace.ProfileInput{Profile: workspace.Profile{Name: name, Model: name, BaseURL: server.URL, Enabled: true, Anonymous: true}}
		if err := app.TestModel(ctx, input); err != nil {
			t.Fatal(err)
		}
		if got := <-messages; got != name {
			t.Fatalf("test sent model %q, expected %q", got, name)
		}
		id, err := app.SaveModel(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if err := app.SetDefaultModel(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := app.Run(ctx); err != nil {
			t.Fatal(err)
		}
		used := false
		for {
			select {
			case got := <-messages:
				if got != name {
					t.Fatalf("run used model %q, expected %q", got, name)
				}
				used = true
			default:
				if !used {
					t.Fatal("configured model was not used")
				}
				goto nextModel
			}
		}
	nextModel:
		page, err := app.Journals(ctx, model.BrowseQuery{Day: today.Format(model.DateLayout)})
		if err != nil || len(page.Rows) != 1 || page.Rows[0].Summary != "测试期间围绕当前目标推进了相关讨论，并形成了可回看的阶段性要点。" {
			t.Fatalf("generated daily summary = %+v, %v", page.Rows, err)
		}
		overview, err := app.Overview(ctx)
		if err != nil || overview.TodaySummary.TopicCount != page.Rows[0].TopicCount ||
			overview.TodaySummary.EventCount != page.Rows[0].EventCount {
			t.Fatalf("overview and journal summary diverged: %+v / %+v, %v", overview.TodaySummary, page.Rows[0], err)
		}
		select {
		case got := <-messages:
			t.Fatalf("unexpected queued model call %q", got)
		default:
		}
	}
}

func TestJournalInclusionChangesApplyWithoutRemovingActivity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	day := model.Day(time.Now())
	event := model.NewEvent("fixture-excluded", "fixture", "excluded", "task_completed", "应保留为证据", day)
	event.Description = "用于验证事件纳入设置的完整活动证据。"
	event.Metadata = model.Object{"important": true, "topic": "应保留为证据"}
	w := Workspace{ConfigPath: path, Connectors: []SourceRegistration{fixtureRegistration(&event)}}
	if err := w.SaveConnection(ctx, workspace.ConnectionInput{ID: "fixture", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := w.SaveSchedule(ctx, workspace.ScheduleInput{Clock: "00:00", LookbackDays: 5, Excluded: []string{"task_completed"}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}
	record, err := w.Journal(ctx, day.Format(model.DateLayout))
	if err != nil || len(record.Evidence) != 1 || len(record.View.Record.Payload.List("evidence")) != 0 {
		t.Fatalf("expected raw activity but no included journal evidence: %v", err)
	}
	schedule, err := w.Schedule(ctx)
	if err != nil || schedule.Clock != "00:00" || schedule.LookbackDays != 5 {
		t.Fatalf("schedule settings = %+v, %v", schedule, err)
	}
}

func fixtureRegistration(event *model.Event) SourceRegistration {
	return SourceRegistration{
		Spec: config.SourceSpec{ID: "fixture", Label: "测试来源", Description: "测试活动证据"},
		Open: func(config.Config) (source.Collector, Probe, error) {
			return func(context.Context, source.Window) (source.Batch, error) {
					return source.Batch{Events: []model.Event{*event}}, nil
				}, func(context.Context) (string, error) {
					return "测试来源可用", nil
				}, nil
		},
	}
}

func TestCatalogSourceProbeAddAndRemove(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			fmt.Fprint(w, `{"login":"octocat","id":7}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	w := Workspace{ConfigPath: path}
	added := func(id string) (bool, bool) {
		items, err := w.Connections(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.ID == id {
				return item.Added, item.Enabled
			}
		}
		t.Fatalf("catalog source %s missing from connections", id)
		return false, false
	}
	// Catalog sources wait unadded; legacy sources stay in the list.
	if got, _ := added("github"); got {
		t.Fatal("github should start out of the list")
	}
	if got, _ := added("zotero"); !got {
		t.Fatal("zotero should stay added for existing workspaces")
	}
	input := workspace.ConnectionInput{
		ID: "github", Enabled: true, Values: map[string]string{"base_url": server.URL},
		Secrets: map[string]string{"token": "tok"},
	}
	message, err := w.TestConnection(ctx, input)
	if err != nil || !strings.Contains(message, "octocat") {
		t.Fatalf("catalog probe: %q, %v", message, err)
	}
	if err := w.SaveConnection(ctx, input); err != nil {
		t.Fatal(err)
	}
	if got, enabled := added("github"); !got || !enabled {
		t.Fatalf("saving should add the source: added=%v enabled=%v", got, enabled)
	}
	if err := w.RemoveConnection(ctx, "github"); err != nil {
		t.Fatal(err)
	}
	if got, _ := added("github"); got {
		t.Fatal("removing should return the source to the catalog")
	}
}

// TestLegacySettingsLoadAndList guards the migration: a settings file saved
// by an older version — retired fields, no added flags — must still load and
// keep its sources in the list.
func TestLegacySettingsLoadAndListPreCatalogConnections(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	legacy := `{"connections":{` +
		`"todoist":{"enabled":true,"values":{"base_url":"https://api.todoist.com/api/v1","course_projects":"课程 A"},"secrets":{}},` +
		`"codex":{"enabled":true,"values":{"exclude_thread_ids":"t1","include_thread_ids":"t2","hosts":"[{\"name\":\"local\",\"command\":[\"codex\",\"app-server\"]}]"},"secrets":{}}}}`
	if err := os.WriteFile(config.SettingsPath(path), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	w := Workspace{ConfigPath: path}
	items, err := w.Connections(ctx)
	if err != nil {
		t.Fatalf("legacy settings must load: %v", err)
	}
	listed := map[string]workspace.Connection{}
	for _, item := range items {
		listed[item.ID] = item
	}
	for _, id := range []string{"todoist", "codex", "zotero", "calendar"} {
		if !listed[id].Added {
			t.Errorf("%s: a pre-catalog source must stay in the list", id)
		}
	}
	if listed["github"].Added {
		t.Error("catalog sources must not be added by the legacy migration")
	}
	// Saving rewrites the connection without the retired keys.
	input := workspace.ConnectionInput{ID: "todoist", Enabled: true, Values: map[string]string{"base_url": "https://api.todoist.com/api/v1"}}
	if err := w.SaveConnection(ctx, input); err != nil {
		t.Fatal(err)
	}
	settings, err := config.ReadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, kept := settings.Connections["todoist"].Values["course_projects"]; kept {
		t.Error("the retired course_projects key must be dropped on load")
	}
}

// TestListModelsSendsTheKey guards the typed-key and saved-key paths: the
// listing request must carry the credential, or 获取模型列表 always fails.
func TestListModelsSendsTheKey(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer typed-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"}]}`)
	}))
	defer server.Close()
	app := Workspace{ConfigPath: path}
	key := "typed-key"
	typed := workspace.ProfileInput{Profile: workspace.Profile{BaseURL: server.URL, Enabled: true}, Token: &key}
	models, err := app.ListModels(ctx, typed)
	if err != nil || len(models) != 2 {
		t.Fatalf("typed key: %v, %v", models, err)
	}
	// A saved profile lists with its stored token, without retyping it.
	id, err := app.SaveModel(ctx, workspace.ProfileInput{
		Profile: workspace.Profile{BaseURL: server.URL, Name: "DeepSeek", Model: "deepseek-chat", Enabled: true},
		Token:   &key,
	})
	if err != nil {
		t.Fatal(err)
	}
	saved := workspace.ProfileInput{Profile: workspace.Profile{ID: id, BaseURL: server.URL, Name: "DeepSeek", Model: "deepseek-chat", Enabled: true}}
	models, err = app.ListModels(ctx, saved)
	if err != nil || len(models) != 2 {
		t.Fatalf("saved key: %v, %v", models, err)
	}
}

// TestCategoryShortsAreData: a 简称 is saved per category, a custom category
// with no 简称 falls back to its full name, and the four built-in labels
// remain the shipped defaults until one is edited.
func TestCategoryShortsAreData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	app := Workspace{ConfigPath: path}
	items, err := app.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	builtIn := map[string]string{}
	for _, item := range items {
		builtIn[item.Name] = item.Short
	}
	if builtIn[model.Research] != "科研" {
		t.Fatalf("the built-in default short should survive: %v", builtIn)
	}
	err = app.SaveCategories(ctx, []workspace.CategoryInput{
		{Previous: model.Research, Name: model.Research, Short: "论文"},
		{Name: "论文写作", Short: "写作"},
		{Name: "健身"},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err = app.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	shorts := map[string]string{}
	for _, item := range items {
		shorts[item.Name] = item.Short
	}
	if shorts[model.Research] != "论文" || shorts["论文写作"] != "写作" {
		t.Fatalf("saved shorts missing: %v", shorts)
	}
	if shorts["健身"] != "健身" {
		t.Fatalf("a category without a 简称 should show its full name: %v", shorts)
	}
	if err := app.SaveCategories(ctx, []workspace.CategoryInput{
		{Name: model.Research}, {Name: "健身", Short: "这个简称长得没有道理"},
	}); err == nil {
		t.Fatal("an over-long 简称 should be refused")
	}
}
