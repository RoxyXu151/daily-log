package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/source"
)

func TestCollectorReadsCompletedTurnAndRedacts(t *testing.T) {
	t.Setenv("DAILY_LOG_TEST_CODEX_PEER", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 9, 0, 0, 0, 0, model.Shanghai)
	collector := Collector{Hosts: []Host{{Name: "test", Command: []string{binary, "-test.run=^TestRPCPeer$"}}}, Timeout: 10 * time.Second}
	batch, err := collector.Collect(context.Background(), source.Window{Since: at, Until: at.Add(time.Hour)})
	if err != nil || batch.Details.Bool("incomplete") || len(batch.Events) != 1 {
		t.Fatalf("unexpected result: %+v, %v", batch.Details, err)
	}
	event := batch.Events[0]
	if event.Timestamp != model.ISO(at.Add(2*time.Minute)) || event.Category != model.Research || !strings.Contains(event.Description, "[邮箱]") {
		t.Fatalf("unexpected event: %+v", event)
	}
	if strings.Contains(event.Description, "example.org") || strings.Contains(event.Description, "跳过这段") {
		t.Fatal("private content reached the persisted event")
	}
}

func TestRPCPeer(t *testing.T) {
	if os.Getenv("DAILY_LOG_TEST_CODEX_PEER") != "1" {
		return
	}
	at := time.Date(2026, 11, 9, 0, 0, 0, 0, model.Shanghai)
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request model.Object
		if err := model.Decode(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		var result model.Object
		switch request.Text("method") {
		case "initialize":
			result = model.Object{}
		case "initialized":
			continue
		case "thread/list":
			data := []model.Object{}
			if !request.Object("params").Bool("archived") {
				data = append(data, model.Object{"id": "thread", "updatedAt": at.Add(3 * time.Minute).Unix()})
			}
			result = model.Object{"data": data}
		case "thread/turns/list":
			result = model.Object{"data": []model.Object{{
				"id": "turn", "status": "completed", "completedAt": at.Add(2 * time.Minute).Unix(),
				"items": []model.Object{
					{"id": "user", "type": "userMessage", "startedAtMs": at.Add(-time.Minute).UnixMilli(),
						"content": []model.Object{{"type": "text", "text": "<system-reminder>跳过这段</system-reminder>讨论论文实验设计，联系 demo@example.org"}}},
					{"id": "reply", "type": "agentMessage", "phase": "final_answer", "completedAtMs": at.Add(2 * time.Minute).UnixMilli(),
						"text": "讨论了实验设计需要对照组和重复试验，列出相关影响因素。"},
				},
			}}}
		default:
			os.Exit(2)
		}
		if err := encoder.Encode(model.Object{"id": request["id"], "result": result}); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}
