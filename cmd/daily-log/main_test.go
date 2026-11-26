package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"daily-log/internal/model"
)

func invoke(t *testing.T, args ...string) (model.Object, int) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := run(context.Background(), args, &out, &stderr)
	var result model.Object
	if len(out.Bytes()) > 0 && out.Bytes()[0] == '{' {
		if err := model.Decode(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	}
	if code != 0 {
		t.Logf("exit %d: %s", code, stderr.String())
	}
	return result, code
}

func TestLocalWorkflow(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	command := func(args ...string) (model.Object, int) {
		t.Helper()
		return invoke(t, append([]string{"--config", path}, args...)...)
	}
	if _, code := command("init"); code != 0 {
		t.Fatalf("init exited %d", code)
	}
	at := time.Now().In(model.Shanghai).Add(-12 * time.Hour).Truncate(time.Second)
	day := at.Format(model.DateLayout)
	title, fail := "原始任务", false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The failure under test is the source's, not the model's: the
		// chat endpoint keeps answering so the day can still be organized
		// and exported.
		if fail && r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"id":"user-1","is_premium":true}`)
		case r.URL.Path == "/v1/chat/completions":
			fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"summary\":\"完成了测试任务的核对与补充，整理相关材料，当天记录已可复查。\",\"work_items\":[]}"}}]}`)
		case r.URL.Path == "/projects":
			fmt.Fprint(w, `{"results":[{"id":"project-1","name":"测试项目"}]}`)
		case r.URL.Path == "/activities":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{
				"id": "completion-1", "event_type": "completed", "object_type": "item",
				"initiator_id": "user-1", "object_id": "task-1", "parent_project_id": "project-1",
				"event_date": model.ISO(at), "extra_data": map[string]any{"content": title, "description": "测试任务说明"},
			}}, "next_cursor": ""})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("todoist:\n  enabled: false"), []byte("todoist:\n  enabled: true\n  base_url: "+server.URL), 1)
	data = bytes.Replace(data, []byte("ai:\n  enabled: false\n  base_url: https://api.deepseek.com\n  model: deepseek-flash"),
		[]byte("ai:\n  enabled: true\n  base_url: "+server.URL+"/v1\n  model: test-model"), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TODOIST_API_TOKEN=test-token\nAI_API_KEY=test-model-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, code := command("run"); code != 0 {
		t.Fatalf("first run exited %d", code)
	}
	title = "补充任务"
	if _, code := command("run"); code != 0 {
		t.Fatalf("second run exited %d", code)
	}
	if _, code := command("run"); code != 0 {
		t.Fatalf("repeat run exited %d", code)
	}
	journal, code := command("show", day)
	if code != 0 || journal.Text("status") != "已生成" {
		t.Fatalf("journal missing after regeneration: %+v", journal)
	}
	status, code := command("status")
	if code != 0 || status["events"] != json.Number("1") || status["suggestions"] != json.Number("0") {
		t.Fatalf("expected one event and no confirmation queue: %+v", status)
	}
	before := status.Object("enabled").Bool("todoist")
	if !before {
		t.Fatal("Todoist source was not enabled")
	}
	fail = true
	result, code := command("run")
	if code != 1 || result.Object("collection").Object("todoist").Bool("ok") || result.Object("outputs").Object("markdown") == nil {
		t.Fatalf("required failure must still produce local output: code=%d, result=%+v", code, result)
	}
}

func TestDemoIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo %")
	for _, expected := range []json.Number{"2", "0"} {
		result, code := invoke(t, "demo", "--directory", root)
		got := result.Object("outputs").Object("markdown")["written_files"]
		if code != 0 || got != expected {
			t.Fatalf("got %v written files and exit %d, want %v files", got, code, expected)
		}
	}
}
