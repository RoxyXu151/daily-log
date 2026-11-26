package journal

import (
	"strings"
	"testing"
	"time"

	"daily-log/internal/model"
)

func TestRenderUsesAIItemsForAllSources(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 0, 0, 0, model.Shanghai)
	first := model.NewEvent("turn-1", "codex", "thread-a", "conversation_activity", "讨论系统分区", at)
	first.Category = model.Research
	first.Description = "对话摘录：梳理系统分区"
	first.Metadata = model.Object{"topic": "讨论系统分区", "thread_title": "迁移开发环境"}
	second := model.NewEvent("turn-2", "codex", "thread-a", "conversation_activity", "检查磁盘", at.Add(20*time.Minute))
	second.Category = model.Research
	second.Description = "对话摘录：检查磁盘挂载"
	second.Metadata = model.Object{"topic": "检查磁盘", "thread_title": "迁移开发环境"}

	task := model.NewEvent("task-1", "todoist", "task-1", "task_completed", "原始待办标题", at)
	draft := Draft{day: "2026-11-11", events: []model.Event{first, second, task}, Hash: "hash"}
	payload := draft.Render([]model.JournalTopic{{
		Title: "完成系统迁移准备", Summary: "梳理分区并检查磁盘挂载", Period: "上午", Conversations: 2,
	}, {
		Title: "核对报销材料", Summary: "核对发票和付款凭证", Period: "上午", Completed: 1,
	}})
	morning := payload.Text("上午")
	want := "完成系统迁移准备 · 梳理分区并检查磁盘挂载\n核对报销材料 · 核对发票和付款凭证"
	if morning != want || payload.Text("achievements") != "核对报销材料" {
		t.Fatalf("body should contain only the AI-authored items: %q", morning)
	}
	empty := draft.Render(nil)
	if empty.Text("上午") != NoItemsText || strings.Contains(empty.Text("achievements"), task.Title) {
		t.Fatalf("an unorganized journal should read as empty, not as raw evidence: %+v", empty)
	}
}

// TestCurrentRecordRetiresPendingText: the placeholder the removed rule mode
// wrote into days is read back as empty, and real text is left alone.
func TestCurrentRecordRetiresPendingText(t *testing.T) {
	record := model.Record{
		Day: "2026-11-25", InputHash: "hash", Status: "已生成",
		Payload: model.Object{
			"input_hash": "hash", "ai_work_items_only": true,
			"上午": "事项待 AI 整理。", "下午": "核对发票和付款凭证。", "achievements": "事项待 AI 整理。",
		},
	}
	got := CurrentRecord(record)
	if got.Payload.Text("上午") != NoItemsText || got.Payload.Text("achievements") != NoItemsText {
		t.Fatalf("the retired placeholder should read as empty: %+v", got.Payload)
	}
	if got.Payload.Text("下午") != "核对发票和付款凭证。" {
		t.Fatalf("real text must survive: %+v", got.Payload)
	}
}
