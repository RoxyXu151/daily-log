package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"daily-log/internal/model"
)

func TestDedupeSummary(t *testing.T) {
	got := dedupeSummary("评估外接固态运行新系统；评估外接固态运行新系统；分析屏幕显示差异。")
	want := "评估外接固态运行新系统；分析屏幕显示差异。"
	if got != want {
		t.Fatalf("dedupeSummary = %q", got)
	}
}

func TestDigestUsesAnEvidencePeriod(t *testing.T) {
	const day = "2026-11-24"
	start := time.Date(2026, 11, 24, 15, 0, 0, 0, model.Shanghai)
	events := map[string]model.Event{
		"first": model.NewEvent("first", "codex", "thread", "conversation_activity", "排查保存", start),
		"later": model.NewEvent("later", "codex", "thread", "conversation_activity", "分析版本", start.Add(5*time.Hour)),
	}
	for suggested, want := range map[string]string{"上午": "下午", "晚上": "晚上"} {
		got := digestTopic(day, "梳理保存方案", "核对版本冲突处理流程。", model.Life, suggested, []string{"first", "later"}, events)
		if got.Period != want {
			t.Errorf("suggested period %q: got %q, want %q", suggested, got.Period, want)
		}
	}
}

func TestDigestRewritesInvalidItemsBeforePublishing(t *testing.T) {
	const quote = "我想知道移动端与桌面端同时保存时会出现哪些版本冲突"
	const reply = "先保存的一端会更新本地日记的版本标识"
	const oldTitle = "讨论版本处理方案"
	event := model.NewEvent("chat", "codex", "thread", "conversation_activity", oldTitle, time.Now())
	event.Description = "对话摘录：" + quote
	event.Raw = model.Object{"evidence": []model.Object{{"role": "assistant", "quote": reply}}}
	good := workItemAnswer{
		Title: "梳理日记保存冲突", Summary: "核对桌面和移动端同时保存时的版本比较流程。",
		Category: model.Life, Period: "上午", EvidenceIDs: []string{event.ID},
	}
	for _, example := range []struct {
		name, title, summary string
		recover              bool
	}{
		{"long title", strings.Repeat("保存", 15), good.Summary, true},
		{"copied title", oldTitle, good.Summary, true},
		{"copied conversation", good.Title, "具体情况为" + quote, true},
		{"copied reply", good.Title, "排查时提出" + reply + "。", true},
		{"conversation labels", good.Title, "[用户]：怎么保存？\n[助手]：覆盖本地文件。", false},
	} {
		t.Run(example.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				item := good
				if requests.Add(1) == 1 || !example.recover {
					item.Title, item.Summary = example.title, example.summary
				}
				content, err := json.Marshal(model.Object{
					"summary": "分析跨端保存的处理流程。", "work_items": []workItemAnswer{item},
				})
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				fmt.Fprintf(w, "{\"choices\":[{\"message\":{\"content\":%q}}]}", content)
			}))
			defer server.Close()
			client := Client{Config: model.Object{"enabled": true, "anonymous": true, "model": "test", "base_url": server.URL}}
			got, err := client.DigestDay(context.Background(), "2026-11-24", []model.Event{event}, []string{model.Life})
			if requests.Load() != 2 {
				t.Fatalf("requests = %d", requests.Load())
			}
			if !example.recover {
				if err == nil || len(got.WorkItems) != 0 {
					t.Fatalf("dialogue was accepted: %+v, %v", got, err)
				}
			} else if err != nil || len(got.WorkItems) != 1 ||
				got.WorkItems[0].Title != good.Title || got.WorkItems[0].Summary != good.Summary {
				t.Fatalf("rewritten item = %+v, %v", got, err)
			}
		})
	}
}
