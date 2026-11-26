package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/netclient"
)

func TestTodoistPreservesCompletionIdentityAndPrecision(t *testing.T) {
	const user = "9007199254740993"
	const task = "9007199254740995"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake" {
			t.Errorf("authorization header missing")
		}
		switch r.URL.Path {
		case "/user":
			fmt.Fprintf(w, `{"id":%s,"is_premium":false}`, user)
		case "/projects":
			fmt.Fprint(w, `{"results":[{"id":"project","name":"课程学习"}]}`)
		case "/activities":
			if r.URL.Query().Get("initiator_id") != user || r.URL.Query().Get("date_from") != "2026-11-02T04:01:00Z" {
				t.Errorf("unexpected query: %v", r.URL.Query())
			}
			fmt.Fprintf(w, `{"results":[
				{"id":"foreign","event_type":"completed","object_type":"item","initiator_id":"other"},
				{"id":"done","event_type":"completed","object_type":"item","initiator_id":%s,
				 "object_id":%s,"parent_project_id":"project","event_date":"2026-11-09T00:00:00.123456Z",
				 "extra_data":{"content":"课程资料","description":"测试证据"}}
			]}`, user, task)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	until := time.Date(2026, 11, 9, 12, 0, 0, 0, model.Shanghai)
	c := Todoist{
		HTTP: netclient.Client{HTTP: server.Client(), Attempts: 1}, BaseURL: server.URL,
		Token: "fake", HistoryDaysFree: 7,
	}
	batch, err := c.Collect(context.Background(), Window{Since: until.AddDate(0, 0, -10), Until: until})
	if err != nil || len(batch.Events) != 1 || !batch.Details.Bool("history_gap") {
		t.Fatalf("unexpected collection: %+v, %v", batch, err)
	}
	event := batch.Events[0]
	want := model.StableID("todoist", user, task, "2026-11-09T08:00:00.123000+08:00")
	if event.ID != want || event.Timestamp != "2026-11-09T08:00:00.123456+08:00" || event.Kind != "task_completed" {
		t.Fatalf("unexpected normalized event: %+v", event)
	}
}

func TestZoteroSeparatesCreationFromModification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/0/items" || r.URL.Query().Get("format") != "json" {
			t.Errorf("unexpected request: %v", r.URL)
		}
		w.Header().Set("Zotero-Server-ID", "server")
		w.Header().Set("Total-Results", "3")
		fmt.Fprint(w, `[
			{"key":"paper","data":{"itemType":"journalArticle","title":"论文"}},
			{"key":"attachment","data":{"itemType":"attachment","parentItem":"paper"}},
			{"key":"note","data":{"itemType":"annotation","parentItem":"attachment","dateAdded":"2026-11-08 17:01:00",
			 "dateModified":"2026-11-09T10:30:00Z","annotationText":"摘录","annotationPageLabel":"3"}}
		]`)
	}))
	defer server.Close()
	today, err := model.ParseDay("2026-11-09")
	if err != nil {
		t.Fatal(err)
	}
	c := Zotero{HTTP: netclient.Client{HTTP: server.Client(), Attempts: 1}, BaseURL: server.URL, Library: "users/0"}
	batch, err := c.Collect(context.Background(), Window{Since: today, Until: today.Add(22 * time.Hour)})
	if err != nil || len(batch.Events) != 2 {
		t.Fatalf("unexpected collection: %+v, %v", batch, err)
	}
	created, modified := batch.Events[0], batch.Events[1]
	if created.Title != "论文" || created.Kind != "annotation_created" || created.Timestamp != "2026-11-09T01:01:00+08:00" ||
		created.ID != model.StableID("zotero", "server:users/0", "note", "created") ||
		created.URL != "zotero://open-pdf/library/items/attachment?annotation=note" || modified.Kind != "annotation_modified" {
		t.Fatalf("unexpected Zotero events: %+v", batch.Events)
	}
}

func TestGitHubChecksAndCollectsPublicEvents(t *testing.T) {
	at := time.Date(2026, 11, 9, 8, 0, 0, 0, model.Shanghai)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"login":"octocat","id":7}`)
		case "/users/octocat/events/public":
			fmt.Fprintf(w, `[
				{"id":"e1","type":"PushEvent","created_at":%q,"repo":{"name":"x/app","url":"https://github.com/x/app"},"payload":{"size":3}},
				{"id":"e2","type":"WatchEvent","created_at":%q,"payload":{}},
				{"id":"e3","type":"PullRequestEvent","created_at":%q,"repo":{"name":"x/app"},"payload":{"action":"closed","number":12,"pull_request":{"title":"接入 GitHub"}}}
			]`, model.ISO(at), model.ISO(at), model.ISO(at))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	github := GitHub{HTTP: netclient.Client{HTTP: server.Client(), Attempts: 1}, BaseURL: server.URL, Token: "tok"}
	if msg, err := github.Check(context.Background()); err != nil || msg != "GitHub 账户 octocat 已验证" {
		t.Fatalf("check: %q, %v", msg, err)
	}
	batch, err := github.Collect(context.Background(), Window{Since: at.AddDate(0, 0, -3), Until: at.Add(time.Hour)})
	if err != nil || len(batch.Events) != 2 {
		t.Fatalf("unexpected collection: %+v, %v", batch, err)
	}
	if batch.Events[0].Title != "推送 3 次提交 · x/app" || batch.Events[0].Source != "github" {
		t.Fatalf("unexpected push event: %+v", batch.Events[0])
	}
	if !strings.Contains(batch.Events[1].Title, "接入 GitHub") {
		t.Fatalf("unexpected pull request event: %+v", batch.Events[1])
	}
}

func TestGitLabChecksAndCollectsEvents(t *testing.T) {
	at := time.Date(2026, 11, 9, 8, 0, 0, 0, model.Shanghai)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "gl-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v4/user":
			fmt.Fprint(w, `{"id":9,"username":"octocat"}`)
		case "/api/v4/users/9/events":
			fmt.Fprintf(w, `[{"id":1,"action_name":"pushed","target_type":"","created_at":%q,"project":{"path_with_namespace":"x/app","web_url":"https://gitlab.com/x/app"}}]`, model.ISO(at))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	gitlab := GitLab{HTTP: netclient.Client{HTTP: server.Client(), Attempts: 1}, BaseURL: server.URL, Token: "gl-tok"}
	if msg, err := gitlab.Check(context.Background()); err != nil || msg != "GitLab 账户 octocat 已验证" {
		t.Fatalf("check: %q, %v", msg, err)
	}
	batch, err := gitlab.Collect(context.Background(), Window{Since: at.AddDate(0, 0, -3), Until: at.Add(time.Hour)})
	if err != nil || len(batch.Events) != 1 || batch.Events[0].Source != "gitlab" {
		t.Fatalf("unexpected collection: %+v, %v", batch, err)
	}
}

func TestWakaTimeSummariesBecomeDailyProjectEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"data":{"id":"u1"}}`)
		case "/user/summaries":
			fmt.Fprint(w, `{"data":[{"range":{"start":"2026-11-09T00:00:00Z"},"projects":[{"name":"daily-log","total_seconds":3600.5},{"name":"tiny","total_seconds":20}]}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	waka := WakaTime{HTTP: netclient.Client{HTTP: server.Client(), Attempts: 1}, BaseURL: server.URL, Key: "wk"}
	if msg, err := waka.Check(context.Background()); err != nil || msg != "WakaTime 账户已验证" {
		t.Fatalf("check: %q, %v", msg, err)
	}
	since := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	batch, err := waka.Collect(context.Background(), Window{Since: since, Until: since.AddDate(0, 0, 3)})
	if err != nil || len(batch.Events) != 1 {
		t.Fatalf("unexpected collection: %+v, %v", batch, err)
	}
	if batch.Events[0].Title != "编码 daily-log · 60 分钟" {
		t.Fatalf("unexpected summary event: %+v", batch.Events[0])
	}
}

func TestActivityWatchKeepsWindowAndWebBuckets(t *testing.T) {
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/0/buckets":
			fmt.Fprint(w, `{"aw-watcher-window_x":{"type":"watcher.window"},"aw-watcher-afk_x":{"type":"afkstatus"},"aw-watcher-web-chrome":{"type":"webhistory"}}`)
		case "/api/0/buckets/aw-watcher-window_x/events":
			fmt.Fprintf(w, `[{"id":"1","timestamp":%q,"duration":120,"data":{"app":"GoLand","title":"catalog.go"}},{"id":"2","timestamp":%q,"duration":5,"data":{"app":"Brief","title":"gone"}}]`, model.ISO(at), model.ISO(at))
		case "/api/0/buckets/aw-watcher-web-chrome/events":
			fmt.Fprintf(w, `[{"id":"3","timestamp":%q,"duration":60,"data":{"url":"https://go.dev/doc","domain":"go.dev"}}]`, model.ISO(at))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	watch := ActivityWatch{HTTP: netclient.Client{HTTP: server.Client(), Attempts: 1}, BaseURL: server.URL}
	if _, err := watch.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	batch, err := watch.Collect(context.Background(), Window{Since: at.AddDate(0, 0, -3), Until: at.Add(time.Hour)})
	if err != nil || len(batch.Events) != 2 {
		t.Fatalf("unexpected collection: %+v, %v", batch, err)
	}
	for _, event := range batch.Events {
		if event.Kind == "web" && event.Title != "浏览 go.dev" {
			t.Fatalf("unexpected web event: %+v", event)
		}
	}
}

func TestLocalFolderRecordsEditedNotes(t *testing.T) {
	root := t.TempDir()
	at := time.Now()
	for _, name := range []string{"a.md", "notes/b.md", ".obsidian/c.md", "d.txt"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The skipped paths keep an old mtime; the kept ones were just written.
	old := at.AddDate(0, 0, -30)
	for _, name := range []string{".obsidian/c.md", "d.txt"} {
		if err := os.Chtimes(filepath.Join(root, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	folder := LocalFolder{Name: "Obsidian", Root: root, Extensions: []string{".md"}}
	if _, err := folder.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	batch, err := folder.Collect(context.Background(), Window{Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Hour)})
	if err != nil || len(batch.Events) != 2 {
		t.Fatalf("unexpected collection: %+v, %v", batch, err)
	}
	for _, event := range batch.Events {
		if event.Source != "obsidian" || !strings.HasPrefix(event.Title, "编辑笔记 ") {
			t.Fatalf("unexpected note event: %+v", event)
		}
	}
	if _, err := folder.Collect(context.Background(), Window{Since: time.Now().AddDate(0, 0, -60), Until: old.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
}

// TestLocalFolderSkipsIgnoredAndUnsetRoots: the app's own export directory
// must never come back as notes, and a folder source with no path fails
// loudly instead of walking the config root.
func TestLocalFolderSkipsIgnoredAndUnsetRoots(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"note.md", "outputs/markdown/2026-11-26.md"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	folder := LocalFolder{
		Name: "Obsidian", Root: root, Extensions: []string{".md"},
		Ignore: []string{filepath.Join(root, "outputs")},
	}
	batch, err := folder.Collect(context.Background(), Window{Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Hour)})
	if err != nil || len(batch.Events) != 1 || batch.Events[0].ObjectID != "note.md" {
		t.Fatalf("ignored directory leaked into collection: %+v, %v", batch, err)
	}
	unset := LocalFolder{Name: "Logseq", Extensions: []string{".md"}}
	if _, err := unset.Collect(context.Background(), Window{}); err == nil {
		t.Fatal("a folder source without a path must not collect anything")
	}
	if _, err := unset.Check(context.Background()); err == nil {
		t.Fatal("a folder source without a path must fail its check")
	}
}
