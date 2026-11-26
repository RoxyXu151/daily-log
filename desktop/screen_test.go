package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"daily-log/internal/config"
	"daily-log/internal/model"
	"daily-log/internal/runtimeapp"
	"daily-log/internal/store"
	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

func TestEvidenceLinksAndWideEditor(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	// Organizing a day needs a model: point the workspace at a stub whose
	// digest finds no work items, so the sheet must still not pass raw
	// evidence off as one.
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"summary\":\"当天没有值得单列的事项，仅有一次证据记录。\",\"work_items\":[]}"}}]}`)
	}))
	defer ai.Close()
	const link = "https://example.test/source-record"
	day := model.Day(time.Now())
	event := model.NewEvent("fixture-event", "fixture", "example", "conversation_activity", "测试来源活动", day.Add(9*time.Hour))
	event.Description, event.URL = "可查看的原始证据", link
	event.Metadata = model.Object{"important": true, "topic": "测试来源活动"}
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
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("AI_API_KEY=test-model-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := runtimeapp.Workspace{
		ConfigPath: path,
		Connectors: []runtimeapp.SourceRegistration{fixtureSource(&event)},
	}
	if err := backend.SaveConnection(ctx, workspace.ConnectionInput{
		ID: "fixture", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Run(ctx); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 4)
	var opened string
	s := &screen{ctx: ctx, backend: backend, dispatch: func(fn func()) { updates <- fn },
		openLink: func(link string) error { opened = link; return nil }}
	view := ui.NewTester(s.view, 1140, 900)
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
		case <-time.After(20 * time.Second):
			t.Fatal("desktop did not refresh")
		}
		if s.failed {
			t.Fatal(s.message)
		}
	}
	s.refresh()
	settle()
	click("打开今日日记")
	settle()
	editor, hasEditor := view.Find("编辑本人补充")
	evidence, hasEvidence := view.Find("查看活动证据")
	if !hasEditor || !hasEvidence || editor.X >= evidence.X {
		t.Fatalf("editor and evidence not side by side: %+v / %+v", editor, evidence)
	}
	click("编辑本人补充")
	view.Type("保留本次输入")
	click("查看活动证据")
	if !view.HasText("可查看的原始证据") {
		t.Fatal("journal evidence did not open")
	}
	snapshot(t, view, "manual-category.png")
	click("打开原始记录")
	if opened != link {
		t.Fatal("source link did not open")
	}
	activityID := s.overview.Recent[0].ID
	click("返回日记")
	view.Frame()
	click("查看活动证据")
	click("课程")
	settle()
	activity, err := backend.Activity(ctx, activityID)
	if err != nil || activity.Category != model.Course {
		t.Fatalf("manual category was not saved: %+v, %v", activity, err)
	}
	if s.fields.Note != "保留本次输入" {
		t.Fatal("visiting evidence lost unsaved journal input")
	}
	click("返回日记")
	click("保存日记")
	settle()
	click("日记列表")
	settle()
	click(s.overview.Today)
	// The model found no work item in that evidence: the sheet must say so
	// with an empty count, never fall back to showing the raw title.
	if !view.HasText("0 件事项") || view.HasText(event.Title) {
		t.Fatalf("journal overview presented raw evidence as an AI item")
	}
	snapshot(t, view, "journal-topics.png")
	view.SetSize(1800, 900)
	view.Frame()
	snapshot(t, view, "journal-topics-wide.png")
	if permittedSourceLink("fixture", "file:///tmp/test", false) ||
		permittedSourceLink("fixture", "https://name:password@example.test", false) ||
		permittedSourceLink("zotero", "zotero://select/library/items/ABCD", true) ||
		!permittedSourceLink("zotero", "zotero://select/library/items/ABCD", false) {
		t.Fatal("source link policy did not respect URL scheme and client")
	}
}

func TestDesktopJournalWorkflow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	update := make(chan func(), 4)
	backend := runtimeapp.Workspace{ConfigPath: path}
	if _, err := backend.SaveModel(ctx, workspace.ProfileInput{Profile: workspace.Profile{
		Name: "本机模型", Model: "demo-model", BaseURL: "http://127.0.0.1:8080/v1",
		Enabled: true, Anonymous: true,
	}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	day := model.Day(time.Now()).Format(model.DateLayout)
	if err := db.Transaction(ctx, func(tx *store.Tx) error {
		return tx.SaveDayDigest(ctx, day, "完成界面整理并梳理后续计划", "fixture-hash", []model.CategoryDigest{
			{Category: model.Research, Summary: "梳理研究主线并补充证据", ItemCount: 1},
			{Category: model.Course, Summary: "整理课程材料并形成笔记", ItemCount: 1},
		}, []model.JournalTopic{
			{ID: "work-1", Title: "梳理研究主线", Summary: "整理论证结构并补充关键证据", Category: model.Research, Period: "上午", Source: "codex", Conversations: 1, EventCount: 1},
			{ID: "work-2", Title: "整理课程材料", Summary: "形成结构化课程笔记", Category: model.Course, Period: "下午", Source: "todoist", EventCount: 1},
		}, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	screen := &screen{ctx: ctx, backend: backend, dispatch: func(fn func()) { update <- fn }}
	view := ui.NewTester(screen.view, 1100, 800)
	settle := func() {
		t.Helper()
		select {
		case fn := <-update:
			fn()
			view.Frame()
		case <-time.After(20 * time.Second):
			t.Fatal("desktop update timed out")
		}
		if screen.failed {
			t.Fatal(screen.message)
		}
	}
	click := func(label string) {
		t.Helper()
		if err := view.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	screen.refresh()
	settle()
	click("打开今日日记")
	settle()
	click("编辑上午")
	view.Type("测试日记正文")
	if screen.fields.Morning != "测试日记正文" {
		t.Fatalf("input did not reach editor: %q", screen.fields.Morning)
	}
	click("保存日记")
	settle()
	journal, err := backend.Journal(ctx, screen.overview.Today)
	if err != nil {
		t.Fatal(err)
	}
	if journal.View.Record.Status != "已生成" || model.JournalTextFrom(journal.View.Record.Payload).Morning != "测试日记正文" {
		t.Fatalf("unexpected saved journal: %+v", journal.View.Record)
	}
	click("采集证据")
	if !view.HasText("人工编辑") || !view.HasText("当前版本关联的证据") || !view.HasText("同日其他活动") {
		t.Fatal("journal did not distinguish manual edits and related evidence")
	}
	click("日记列表")
	settle()
	if len(screen.table.journals.Rows) != 1 || screen.table.journals.Rows[0].Day != screen.overview.Today {
		t.Fatal("journal table did not display the saved record")
	}
	summary := screen.table.journals.Rows[0]
	if summary.Weekday == "" || summary.Morning != "测试日记正文" || len(summary.Categories) != 4 {
		t.Fatalf("journal sheet did not expose periods and categories: %+v", summary)
	}
	if len(summary.CategoryDigests) != 2 || !view.HasText("梳理研究主线") {
		t.Fatalf("journal sheet did not render per-category summaries: %+v", summary.CategoryDigests)
	}
	if !view.HasText("今日概要") {
		t.Fatal("journal overview did not render its summary columns")
	}
	snapshot(t, view, "journal-sheet.png")
	click(screen.overview.Today)
	if !view.HasText("三时段事项") || !view.HasText("编辑日记") {
		t.Fatal("journal row did not expand into period details")
	}
	snapshot(t, view, "journal-expanded.png")
	view.SetSize(1800, 900)
	view.Frame()
	if !view.HasText("分类分布") || !view.HasText("记录") {
		t.Fatal("wide journal overview did not render its full columns")
	}
	snapshot(t, view, "journal-sheet-wide.png")
	view.SetSize(1100, 800)
	view.Frame()
	click("最早优先")
	settle()
	if !screen.table.applied.Ascending {
		t.Fatal("journal sort direction did not change")
	}
	if view.HasText("活动流水") || view.HasText("论文阅读") || view.HasText("当前日记") {
		t.Fatal("redundant activity, journal, and reading navigation should be removed")
	}
	view.SetSize(390, 760)
	click("工作台")
	click("日记列表")
	settle()
	if !view.HasText("日记总表") {
		t.Fatal("narrow layout did not show the journal sheet")
	}
	view.SetSize(1100, 800)
	click("来源管理")
	settle()
	if view.HasText("ChatGPT 摘要") {
		t.Fatal("removed ChatGPT summary source is still visible")
	}
	if _, ok := view.AccessByRole(ui.RoleSwitch, "启用Todoist采集"); !ok {
		t.Fatal("source management did not render the Todoist enable switch")
	}
	click("Todoist · 暂停")
	if !view.HasText("API 令牌") {
		t.Fatal("source settings did not render")
	}
	click("Todoist · 暂停")
	if view.HasText("API 令牌") {
		t.Fatal("source settings did not collapse")
	}
	click("Todoist · 暂停")
	if !view.HasText("API 令牌") {
		t.Fatal("source settings did not expand again")
	}
	view.Advance(disclosure)
	snapshot(t, view, "sources-expanded.png")
	click("外观")
	click("紫罗兰")
	view.Frame()
	snapshot(t, view, "appearance-violet.png")
	click("日记列表")
	settle()
	snapshot(t, view, "journal-sheet-violet.png")
	view.SetSize(1100, 1100)
	view.Frame()
	click("分类管理")
	settle()
	snapshot(t, view, "categories-page.png")
	if !view.HasText("分类列表") || len(screen.categoryEdits) != 4 ||
		screen.categoryEdits[0].previous != model.Research {
		t.Fatal("category management did not render configured categories")
	}
	click("新分类名称")
	view.Type("实验室")
	click("添加")
	click("保存分类")
	settle()
	found := slices.ContainsFunc(screen.categories, func(category workspace.Category) bool {
		return category.Name == "实验室"
	})
	if !found {
		t.Fatal("custom category was not saved")
	}
	click("日记列表")
	settle()
	click(screen.overview.Today)
	if !view.HasText("实验室 0") {
		t.Fatal("expanded journal did not show the custom category")
	}
	snapshot(t, view, "journal-custom-category.png")
	view.SetSize(1100, 800)
	view.Frame()
	click("模型设置")
	settle()
	snapshot(t, view, "models-list.png")
	if view.HasText("直连") || view.HasText("路由") {
		t.Fatal("model settings should not expose connector routing modes")
	}
	if view.HasText("任务使用的模型") || !view.HasText("使用中") {
		t.Fatal("model settings should expose one model in use at a time")
	}
	click("添加模型")
	if !view.HasText("连接参数") {
		t.Fatal("model settings did not render")
	}
	click("日志更新")
	settle()
	if !view.HasText("定时与补采") {
		t.Fatal("schedule settings did not render")
	}
}

func snapshot(t *testing.T, view *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("DAILY_LOG_SNAPSHOT_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, view.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestTimesUseLocalDisplayFormat(t *testing.T) {
	if got := displayTime("2026-11-11T20:31:46.909828+08:00"); got != "2026-11-11 20:31" {
		t.Fatalf("display time = %q", got)
	}
}
