package main

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

// A fixture workspace with enough content, in every state the app can show,
// to judge the design of each page: this file exists for the pictures it
// writes, not for assertions.
type fixtureWorkspace struct {
	categories []workspace.Category
	journals   []model.JournalSummary
	acts       []workspace.Evidence
}

func (f fixtureWorkspace) Overview(context.Context) (workspace.Overview, error) {
	return workspace.Overview{
		Today:        "2026-11-24",
		TodaySummary: f.journals[0],
		Recent: []workspace.ActivitySummary{
			{ID: "e1", Title: "整理论文第三章的论证结构，补充反例与数据来源", Source: "codex", Timestamp: "2026-11-24T09:12:00+08:00"},
			{ID: "e2", Title: "完成《分布式系统》作业：一致性哈希的分片模拟", Source: "chatgpt", Timestamp: "2026-11-24T11:02:00+08:00"},
			{ID: "e3", Title: "整理简历项目描述，突出可量化的性能收益", Source: "todoist", Timestamp: "2026-11-24T14:38:00+08:00"},
			{ID: "e4", Title: "组会材料：周报与实验排期", Source: "calendar", Timestamp: "2026-11-24T16:05:00+08:00"},
		},
		LastRun:   "2026-11-24T07:30:00+08:00",
		NextRun:   "2026-11-24T23:00:00+08:00",
		JobStatus: "success",
		Sources: []workspace.SourceStatus{
			{Name: "codex", Enabled: true, Status: "success", LastSuccess: "2026-11-24T15:04:00+08:00", Coverage: "ok", ObservedThrough: "2026-11-24T15:04:00+08:00",
				Attempt: &workspace.SourceAttempt{RequestedSince: "2026-11-24T00:00:00+08:00", RequestedUntil: "2026-11-24T15:04:00+08:00", Returned: 128, Changed: 12, HasCounts: true}},
			{Name: "chatgpt", Enabled: true, Status: "success", LastSuccess: "2026-11-24T15:04:00+08:00", Coverage: "limited", ObservedThrough: "2026-11-24T13:20:00+08:00",
				Attempt: &workspace.SourceAttempt{RequestedSince: "2026-11-24T00:00:00+08:00", RequestedUntil: "2026-11-24T15:04:00+08:00", EffectiveSince: "2026-11-23T08:00:00+08:00", Returned: 41, Changed: 3, HasCounts: true, HistoryGap: true, HistoryLimitDays: 30}},
			{Name: "todoist", Enabled: true, Status: "error", ErrorMessage: "令牌已过期，请重新配对", LastSuccess: "2026-11-23T23:02:00+08:00"},
			{Name: "calendar", Enabled: false, Status: ""},
			{Name: "zotero", Enabled: true, Status: "running"},
		},
		Outputs: []workspace.OutputStatus{
			{Name: "markdown", Enabled: true, Status: "success", LastSuccess: "2026-11-24T15:05:00+08:00", Summary: &workspace.OutputSummary{Files: 6}},
			{Name: "sheets", Enabled: true, Status: "error", ErrorMessage: "表格权限不足，重新配对后重试", LastSuccess: "2026-11-22T23:10:00+08:00",
				Summary: &workspace.OutputSummary{Written: 42, Protected: 8, Skipped: 2}},
			{Name: "excel", Enabled: false},
		},
	}, nil
}

func (f fixtureWorkspace) Categories(context.Context) ([]workspace.Category, error) {
	return f.categories, nil
}

func (f fixtureWorkspace) Journals(_ context.Context, q model.BrowseQuery) (workspace.Page[model.JournalSummary], error) {
	rows := f.journals
	if q.Day != "" {
		var only []model.JournalSummary
		for _, item := range rows {
			if item.Day == q.Day {
				only = append(only, item)
			}
		}
		rows = only
	}
	return workspace.Page[model.JournalSummary]{Rows: rows, More: len(rows) >= 40}, nil
}

func (f fixtureWorkspace) Activities(context.Context, model.BrowseQuery) (workspace.Page[workspace.Evidence], error) {
	return workspace.Page[workspace.Evidence]{Rows: f.acts, More: true}, nil
}

func (f fixtureWorkspace) Activity(_ context.Context, id string) (workspace.Evidence, error) {
	for _, item := range f.acts {
		if item.ID == id {
			return item, nil
		}
	}
	return f.acts[0], nil
}

func (f fixtureWorkspace) SetEventCategory(context.Context, string, string) error { return nil }

func (f fixtureWorkspace) SaveCategories(context.Context, []workspace.CategoryInput) error {
	return nil
}

func (f fixtureWorkspace) Reading(context.Context, model.BrowseQuery) (workspace.Page[model.Session], error) {
	return workspace.Page[model.Session]{}, nil
}

func (f fixtureWorkspace) Journal(_ context.Context, day string) (workspace.Journal, error) {
	payload := model.Object{"上午": "整理论文第三章的论证结构，补充了两组反例。", "下午": "完成一致性哈希的分片模拟，并写下测试笔记。",
		"晚上": "复盘今天的实验排期。", "manual": "", "coverage": model.Object{"codex": "ok", "chatgpt": "limited", "todoist": "unavailable"},
		"evidence": []any{model.Object{"event_id": "e1", "source": "codex", "type": "conversation_activity"}}}
	return workspace.Journal{
		View:     model.JournalView{Record: model.Record{Day: day, Payload: payload, Status: "已生成"}, Revision: "rev-1"},
		Evidence: f.acts[:3],
		History: []model.JournalVersion{
			{ID: 1, Status: "已生成", ReplacedAt: "2026-11-24T07:30:00+08:00", Text: model.JournalText{Morning: " earlier 版本", Afternoon: "分片模拟第一版"}},
			{ID: 2, Status: "已生成", ReplacedAt: "2026-11-23T23:04:00+08:00", Text: model.JournalText{Evening: "复盘排期"}},
		},
	}, nil
}

func (f fixtureWorkspace) Save(_ context.Context, day, _ string, text model.JournalText, _ bool) (model.JournalView, error) {
	payload := model.Object{}
	text.Apply(payload)
	return model.JournalView{Record: model.Record{Day: day, Payload: payload, Status: "已生成"}, Revision: "rev-2"}, nil
}

func (f fixtureWorkspace) Run(context.Context) error             { return nil }
func (f fixtureWorkspace) Recover(context.Context) error         { return nil }
func (f fixtureWorkspace) Regenerate(context.Context, int) error { return nil }
func (f fixtureWorkspace) Export(context.Context, string) error  { return nil }

func (f fixtureWorkspace) Schedule(context.Context) (workspace.ScheduleSettings, error) {
	return workspace.ScheduleSettings{
		ScheduleInput: workspace.ScheduleInput{Clock: "23:00", LookbackDays: 3},
		Next:          "2026-11-24T23:00:00+08:00", LastInstalled: "2026-11-20T10:00:00+08:00", AppliedClock: "23:00",
		EventTypes: []workspace.EventType{
			{ID: "conversation_activity", Label: "对话活动", Today: 42},
			{ID: "schedule", Label: "日程计划", Today: 6},
			{ID: "completed", Label: "已完成任务", Today: 11},
			{ID: "manual", Label: "手动补充", Today: 2},
		},
	}, nil
}

func (f fixtureWorkspace) SaveSchedule(context.Context, workspace.ScheduleInput) error { return nil }
func (f fixtureWorkspace) InstallSchedule(context.Context) error                       { return nil }

func (f fixtureWorkspace) Connections(context.Context) ([]workspace.Connection, error) {
	return []workspace.Connection{
		{ID: "codex", Label: "Codex", Description: "本机命令行中的编码对话", Enabled: true, Added: true, Integration: "command", Fields: []workspace.Field{
			{Key: "roots", Label: "工作目录", Kind: "list", Value: "~/work, ~/papers"},
			{Key: "account", Label: "账户", Kind: "text", Value: "researcher", ReadOnly: true},
		}},
		{ID: "chatgpt", Label: "ChatGPT", Description: "网页端与桌面端的对话", Enabled: true, Added: true, Integration: "api", Fields: []workspace.Field{
			{Key: "token", Label: "API 令牌", Kind: "secret", Value: "", Configured: true},
			{Key: "hosts", Label: "域名", Kind: "hosts", Value: "chatgpt.com"},
		}},
		{ID: "todoist", Label: "Todoist", Description: "任务与完成记录", Enabled: false, Added: true, Integration: "api", Fields: []workspace.Field{
			{Key: "token", Label: "API 令牌", Kind: "secret", Value: ""},
		}},
		{ID: "calendar", Label: "Google Calendar", Description: "日程与会议", Enabled: true, Removed: true, Fields: nil},
		{ID: "github", Label: "GitHub", Description: "读取本人的推送、Pull Request、议题与评审事件。", Integration: "api", Fields: []workspace.Field{
			{Key: "base_url", Label: "API 地址", Kind: "url", Value: "https://api.github.com"},
			{Key: "username", Label: "用户名（留空自动识别）", Kind: "text"},
			{Key: "token", Label: "API 令牌", Kind: "secret"},
		}},
		{ID: "activitywatch", Label: "ActivityWatch", Description: "读取本机 ActivityWatch 的窗口、浏览与挂机记录。", Integration: "local-api", Fields: []workspace.Field{
			{Key: "base_url", Label: "本机 API 地址", Kind: "url", Value: "http://127.0.0.1:5600"},
		}},
		{ID: "obsidian", Label: "Obsidian", Description: "按修改时间记录本机 Vault 中的笔记编辑。", Integration: "file", Fields: []workspace.Field{
			{Key: "vault_path", Label: "Vault 路径", Kind: "text"},
		}},
	}, nil
}

func (f fixtureWorkspace) SaveConnection(context.Context, workspace.ConnectionInput) error {
	return nil
}
func (f fixtureWorkspace) RemoveConnection(context.Context, string) error { return nil }
func (f fixtureWorkspace) TestConnection(context.Context, workspace.ConnectionInput) (string, error) {
	return "连接正常，读取到 12 条记录", nil
}

func (f fixtureWorkspace) Models(context.Context) (workspace.ModelSettings, error) {
	return workspace.ModelSettings{
		Profiles: []workspace.Profile{
			{ID: "m1", Name: "本机模型", BaseURL: "http://127.0.0.1:8080/v1", Model: "glm-4.5-air", Enabled: true, Anonymous: true},
			{ID: "m2", Name: "云端备用", BaseURL: "https://api.example.com/v1", Model: "claude-sonnet-5-5", Enabled: true, HasToken: true},
			{ID: "m3", Name: "离线小模型", BaseURL: "http://127.0.0.1:11434/v1", Model: "qwen3:8b", Enabled: false},
		},
		Default: "m1",
	}, nil
}

func (f fixtureWorkspace) SaveModel(context.Context, workspace.ProfileInput) (string, error) {
	return "m4", nil
}
func (f fixtureWorkspace) TestModel(context.Context, workspace.ProfileInput) error { return nil }
func (f fixtureWorkspace) ListModels(context.Context, workspace.ProfileInput) ([]string, error) {
	return []string{"deepseek-chat", "deepseek-reasoner"}, nil
}
func (f fixtureWorkspace) RemoveModel(context.Context, string) error              { return nil }
func (f fixtureWorkspace) SetDefaultModel(context.Context, string) error          { return nil }
func (f fixtureWorkspace) SetJournalModelFailureMode(context.Context, bool) error { return nil }

func fixtureData() fixtureWorkspace {
	minute := func(v float64) *float64 { return &v }
	categories := []workspace.Category{
		{Name: model.Research, Short: "科研", Count: 128}, {Name: model.Course, Short: "课程", Count: 74},
		{Name: model.Career, Short: "职业", Count: 31}, {Name: model.Life, Short: "生活", Count: 19},
		{Name: "论文写作", Short: "写作", Count: 5}, {Name: model.Uncategorized, Count: 4},
	}
	digests := []model.CategoryDigest{
		{Category: model.Research, Summary: "第三章的论证结构定稿，补了两组反例与数据来源", ItemCount: 3},
		{Category: model.Course, Summary: "一致性哈希的分片模拟完成，写了测试笔记", ItemCount: 2},
	}
	topics := []model.JournalTopic{
		{ID: "t1", Title: "整理论文第三章论证结构", Summary: "定下反例与数据来源的对应关系，改写两段推导。", Category: model.Research, Period: "上午", Source: "codex", EventCount: 14, Conversations: 3, Start: "2026-11-24T09:00:00+08:00", End: "2026-11-24T10:40:00+08:00", Evidence: []string{"e1"}},
		{ID: "t2", Title: "一致性哈希分片模拟", Summary: "实现虚拟节点，跑通扩容时的迁移测试。", Category: model.Course, Period: "下午", Source: "chatgpt", EventCount: 9, Start: "2026-11-24T13:30:00+08:00", End: "2026-11-24T15:10:00+08:00", Evidence: []string{"e2"}},
		{ID: "t3", Title: "简历项目描述改写", Summary: "把三个项目改成可量化的结果。", Category: model.Career, Period: "下午", Source: "todoist", EventCount: 4, Start: "2026-11-24T15:30:00+08:00", End: "2026-11-24T16:20:00+08:00"},
		{ID: "t4", Title: "组会材料与实验排期", Summary: "整理周报，定下下周的三组实验。", Category: model.Research, Period: "晚上", Source: "calendar", EventCount: 3, Start: "2026-11-24T20:00:00+08:00", End: "2026-11-24T21:15:00+08:00"},
	}
	summary := func(day, weekday, text string, topics []model.JournalTopic) model.JournalSummary {
		return model.JournalSummary{
			Day: day, Status: "已生成", Weekday: weekday, Summary: text,
			Morning: "整理论文第三章的论证结构，补充了两组反例。", Afternoon: "完成一致性哈希的分片模拟。",
			Evening: "复盘今天的实验排期。", Categories: []model.JournalCategory{
				{Name: model.Research, Count: 6, Minutes: minute(182)}, {Name: model.Course, Count: 3, Minutes: minute(96)},
				{Name: model.Career, Count: 2}, {Name: model.Life, Count: 1, Minutes: minute(20)},
			},
			CategoryDigests: digests, Topics: topics,
			Sources:   []string{"codex", "chatgpt", "todoist", "calendar"},
			Completed: 4, EventCount: 30, ConversationCount: 7, TopicCount: len(topics),
			ActiveStart: day + "T08:40:00+08:00", ActiveEnd: day + "T22:05:00+08:00",
		}
	}
	var journals []model.JournalSummary
	for i := range 6 {
		day := time.Date(2026, 11, 24-i, 0, 0, 0, 0, time.Local)
		journals = append(journals, summary(day.Format(model.DateLayout), "周"+
			[]string{"一", "二", "三", "四", "五", "六", "日"}[int(day.Weekday())],
			"整理论文第三章的论证结构并补两组反例；完成一致性哈希的分片模拟与迁移测试；下午改写简历项目描述，晚上定下周实验排期。",
			topics))
	}
	acts := []workspace.Evidence{
		{ID: "e1", Source: "codex", Category: model.Research, Title: "整理论文第三章的论证结构", Kind: "direct", Timestamp: "2026-11-24T09:12:00+08:00",
			Description: "围绕反例与数据来源的对应关系，改写了两段推导，并留下一条待确认的假设。", URL: "https://example.test/threads/1", Reference: "codex://threads/1", EventType: "conversation_activity"},
		{ID: "e2", Source: "chatgpt", Category: model.Course, Title: "一致性哈希的分片模拟", Kind: "inferred", Timestamp: "2026-11-24T11:02:00+08:00",
			Description: "实现虚拟节点并跑通扩容迁移测试。", URL: "https://example.test/c/2", Reference: "chatgpt://c/2"},
		{ID: "e3", Source: "todoist", Category: model.Career, Title: "简历项目描述改写", Kind: "completed", Timestamp: "2026-11-24T14:38:00+08:00",
			Description: "三个项目改成可量化的结果。", Reference: "todoist://task/3"},
		{ID: "e4", Source: "calendar", Category: model.Research, Title: "组会材料：周报与实验排期", Kind: "schedule", Timestamp: "2026-11-24T16:05:00+08:00",
			Description: "组会：周报与下周实验安排。", Reference: "calendar://evt/4"},
		{ID: "e5", Source: "codex", Category: model.Life, Title: "整理本机环境", Kind: "manual", Timestamp: "2026-11-24T18:20:00+08:00",
			Description: "", Reference: "codex://threads/5"},
	}
	return fixtureWorkspace{categories: categories, journals: journals, acts: acts}
}

// TestVisualSnapshots writes each page of the app, in both appearances, so
// that the design can be looked at rather than imagined.
func TestVisualSnapshots(t *testing.T) {
	dir := os.Getenv("DAILY_LOG_VISUAL_DIR")
	if dir == "" {
		t.Skip("set DAILY_LOG_VISUAL_DIR to write snapshots")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := fixtureData()
	shoot := func(name string, dark bool, width, height int, open func(*screen, *ui.Tester)) {
		t.Helper()
		s := &screen{page: "", backend: fixture, overviewLoaded: true,
			overview: mustOverview(t, fixture), categories: fixture.categories,
			connections: mustConnections(t, fixture), models: mustModels(t, fixture),
			table:      tableView{selected: -1, journals: mustJournals(t, fixture), activities: mustActivities(t, fixture)},
			appearance: appearance{Mode: modeName(dark), Accent: "默认"}}
		s.scheduling = mustSchedule(t, fixture)
		view := ui.NewTester(s.view, width, height)
		open(s, view)
		view.Frame()
		view.Advance(time.Second)
		view.Frame()
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, view.Image()); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", name)
	}

	for _, dark := range []bool{false, true} {
		tag := "light"
		if dark {
			tag = "dark"
		}
		shoot(tag+"-dashboard.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {})
		shoot(tag+"-dashboard-wide.png", dark, 1700, 950, func(s *screen, v *ui.Tester) {})
		shoot(tag+"-dashboard-narrow.png", dark, 760, 800, func(s *screen, v *ui.Tester) {})
		shoot(tag+"-dashboard-phone.png", dark, 420, 860, func(s *screen, v *ui.Tester) {})
		shoot(tag+"-journal.png", dark, 1140, 800, func(s *screen, v *ui.Tester) { s.page = "days" })
		shoot(tag+"-journal-expanded.png", dark, 1140, 900, func(s *screen, v *ui.Tester) {
			s.page = "days"
			s.table.selected = 0
		})
		shoot(tag+"-editor.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			j, err := fixture.Journal(context.Background(), "2026-11-24")
			if err != nil {
				t.Fatal(err)
			}
			s.current, s.page = j, "editor"
			s.fields = model.JournalTextFrom(j.View.Record.Payload)
			s.saved = s.fields
		})
		shoot(tag+"-editor-history.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			j, _ := fixture.Journal(context.Background(), "2026-11-24")
			s.current, s.page, s.panel = j, "editor", "history"
			s.fields = model.JournalTextFrom(j.View.Record.Payload)
		})
		shoot(tag+"-editor-evidence.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			j, _ := fixture.Journal(context.Background(), "2026-11-24")
			s.current, s.page, s.panel = j, "editor", "evidence"
			s.fields = model.JournalTextFrom(j.View.Record.Payload)
		})
		shoot(tag+"-activity.png", dark, 1140, 800, func(s *screen, v *ui.Tester) { s.page = "activity" })
		shoot(tag+"-evidence.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			s.page, s.activityDetail, s.activityBack = "activity-detail", fixture.acts[0], "days"
		})
		shoot(tag+"-categories.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			s.page = "categories"
			s.resetCategoryEdits()
		})
		shoot(tag+"-sources.png", dark, 1140, 800, func(s *screen, v *ui.Tester) { s.page = "connections" })
		shoot(tag+"-sources-open.png", dark, 1140, 900, func(s *screen, v *ui.Tester) {
			s.page = "connections"
			s.selectConnection(s.connections[1])
		})
		shoot(tag+"-sources-catalog.png", dark, 1140, 900, func(s *screen, v *ui.Tester) {
			s.page, s.addCatalog = "connections", true
			s.connections = mustConnections(t, fixture)
		})
		shoot(tag+"-models.png", dark, 1140, 800, func(s *screen, v *ui.Tester) { s.page = "models" })
		shoot(tag+"-models-form.png", dark, 1140, 900, func(s *screen, v *ui.Tester) {
			s.page = "models"
			s.profile = profileForm{open: true, profile: s.models.Profiles[1]}
		})
		shoot(tag+"-schedule.png", dark, 1140, 860, func(s *screen, v *ui.Tester) { s.page = "schedule" })
		shoot(tag+"-appearance.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			s.page = "appearance"
			s.appearance = appearance{Mode: modeName(dark), Accent: "默认"}
		})
		shoot(tag+"-mobile.png", dark, 1140, 800, func(s *screen, v *ui.Tester) { s.page = "mobile" })
		shoot(tag+"-skeleton.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			s.busy, s.message, s.overviewLoaded = true, "读取日志数据…", false
		})
		shoot(tag+"-skeleton-table.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			s.page, s.busy, s.message = "days", true, "查询表格…"
			s.table.journalLoaded = false
		})
		shoot(tag+"-notice.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			s.failed, s.message = true, "无法连接采集主机：令牌已过期，请重新配对"
			s.jobRunning, s.jobMessage, s.jobPhase = true, "正在更新日记…", "整理日记"
		})
		// What the app says on its own, one page at a time: a phone is
		// where a card runs out of room first.
		shoot(tag+"-toast.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			s.notify("日记已保存")
		})
		// The room's color temperature is supposed to follow the accent,
		// so the picture of it has to be looked at, not reasoned about.
		shoot(tag+"-dashboard-sea-blue.png", dark, 1140, 800, func(s *screen, v *ui.Tester) {
			s.appearance.Accent = "海蓝"
		})
		for _, page := range []struct {
			name string
			open func(*screen)
		}{
			{"journal", func(s *screen) { s.page = "days" }},
			{"editor", func(s *screen) {
				j, _ := fixture.Journal(context.Background(), "2026-11-24")
				s.current, s.page = j, "editor"
				s.fields = model.JournalTextFrom(j.View.Record.Payload)
			}},
			{"categories", func(s *screen) { s.page = "categories"; s.resetCategoryEdits() }},
			{"sources", func(s *screen) {
				s.page = "connections"
				s.selectConnection(s.connections[1])
			}},
			{"models", func(s *screen) { s.page = "models" }},
			{"schedule", func(s *screen) { s.page = "schedule" }},
		} {
			name := tag + "-phone-" + page.name + ".png"
			open := page.open
			shoot(name, dark, 420, 860, func(s *screen, v *ui.Tester) { open(s) })
		}
	}
}

// modeName is what the appearance settings call the look a snapshot asks for.
func modeName(dark bool) string {
	if dark {
		return "深色"
	}
	return "浅色"
}

func mustOverview(t *testing.T, f fixtureWorkspace) workspace.Overview {
	o, err := f.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func mustConnections(t *testing.T, f fixtureWorkspace) []workspace.Connection {
	items, err := f.Connections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func mustModels(t *testing.T, f fixtureWorkspace) workspace.ModelSettings {
	m, err := f.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func mustSchedule(t *testing.T, f fixtureWorkspace) scheduleForm {
	s, err := f.Schedule(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return newScheduleForm(s)
}

func mustJournals(t *testing.T, f fixtureWorkspace) workspace.Page[model.JournalSummary] {
	p, err := f.Journals(context.Background(), model.BrowseQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustActivities(t *testing.T, f fixtureWorkspace) workspace.Page[workspace.Evidence] {
	p, err := f.Activities(context.Background(), model.BrowseQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var _ = fmt.Sprintf
