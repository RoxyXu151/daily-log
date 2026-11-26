package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/netclient"
)

// Catalog collectors are the common services reached the way their vendors
// document: a token against a public REST API, a local HTTP service on
// loopback, or a folder of files on this machine. Each says what it checked
// in a human line, so 测试连接 answers in words rather than a spinner.

type Catalog interface {
	Collect(context.Context, Window) (Batch, error)
	Check(context.Context) (string, error)
}

func number(value any) string {
	switch v := value.(type) {
	case json.Number:
		return v.String()
	case string:
		return v
	case nil:
		return "0"
	}
	return fmt.Sprint(value)
}

// GitHub reads the authenticated user's public event stream: pushes, pull
// requests, issues, reviews.
type GitHub struct {
	HTTP     netclient.Client
	BaseURL  string
	Username string
	Token    string
}

func (g GitHub) get(ctx context.Context, path string, params url.Values, result any) error {
	if strings.TrimSpace(g.Token) == "" {
		return errors.New("GitHub 已启用但没有 API 令牌")
	}
	headers := http.Header{"Authorization": {"Bearer " + g.Token}, "Accept": {"application/vnd.github+json"}}
	_, err := g.HTTP.JSON(ctx, http.MethodGet, strings.TrimRight(g.BaseURL, "/")+"/"+path, params, headers, nil, result)
	return err
}

func (g GitHub) user(ctx context.Context) (string, error) {
	if g.Username != "" {
		return g.Username, nil
	}
	var me model.Object
	if err := g.get(ctx, "user", nil, &me); err != nil {
		return "", err
	}
	login := me.Text("login")
	if login == "" {
		return "", errors.New("GitHub 响应缺少用户名")
	}
	return login, nil
}

func (g GitHub) Check(ctx context.Context) (string, error) {
	login, err := g.user(ctx)
	if err != nil {
		return "", err
	}
	return "GitHub 账户 " + login + " 已验证", nil
}

func (g GitHub) Collect(ctx context.Context, window Window) (Batch, error) {
	login, err := g.user(ctx)
	if err != nil {
		return Batch{}, err
	}
	batch := Batch{Details: model.Object{}}
	for page := 1; page <= 10; page++ {
		var events []model.Object
		params := url.Values{"per_page": {"100"}, "page": {fmt.Sprint(page)}}
		if err := g.get(ctx, "users/"+url.PathEscape(login)+"/events/public", params, &events); err != nil {
			return batch, err
		}
		if len(events) == 0 {
			break
		}
		done := false
		for _, item := range events {
			at, err := time.Parse(time.RFC3339, item.Text("created_at"))
			if err != nil {
				continue
			}
			if at.Before(window.Since) {
				done = true
				continue
			}
			if at.After(window.Until) {
				continue
			}
			event, ok := githubEvent(item, at)
			if !ok {
				continue
			}
			batch.Events = append(batch.Events, event)
		}
		if done || len(events) < 100 {
			break
		}
	}
	batch.Details["returned"] = len(batch.Events)
	return batch, nil
}

func githubEvent(item model.Object, at time.Time) (model.Event, bool) {
	kind := item.Text("type")
	repo := item.Object("repo")
	payload := item.Object("payload")
	repoName := repo.Text("name")
	repoURL := repo.Text("url")
	var title string
	switch kind {
	case "PushEvent":
		title = fmt.Sprintf("推送 %s 次提交 · %s", number(payload["size"]), repoName)
	case "PullRequestEvent":
		pr := payload.Object("pull_request")
		title = fmt.Sprintf("Pull Request #%s %s · %s · %s", number(payload["number"]), payload.Text("action"), pr.Text("title"), repoName)
	case "PullRequestReviewEvent", "PullRequestReviewCommentEvent":
		pr := payload.Object("pull_request")
		title = fmt.Sprintf("评审 Pull Request · %s · %s", pr.Text("title"), repoName)
	case "IssuesEvent":
		issue := payload.Object("issue")
		title = fmt.Sprintf("议题 #%s %s · %s · %s", number(payload["number"]), payload.Text("action"), issue.Text("title"), repoName)
	case "IssueCommentEvent":
		issue := payload.Object("issue")
		title = fmt.Sprintf("议题评论 · %s · %s", issue.Text("title"), repoName)
	case "CreateEvent":
		title = fmt.Sprintf("创建 %s %s · %s", payload.Text("ref_type"), payload.Text("ref_name"), repoName)
	default:
		return model.Event{}, false
	}
	event := model.NewEvent(model.StableID("github", item.Text("id")), "github", item.Text("id"), strings.ToLower(strings.TrimSuffix(kind, "Event")), title, at)
	event.URL, event.Updated, event.Raw = repoURL, model.ISO(at), item
	event.Evidence, event.Confidence = "direct", 0.95
	event.Metadata = model.Object{"project": repoName, "github_type": kind}
	return event, true
}

// GitLab reads the authenticated user's event stream from a GitLab host.
type GitLab struct {
	HTTP    netclient.Client
	BaseURL string
	Token   string
}

func (g GitLab) get(ctx context.Context, path string, params url.Values, result any) error {
	if strings.TrimSpace(g.Token) == "" {
		return errors.New("GitLab 已启用但没有访问令牌")
	}
	headers := http.Header{"PRIVATE-TOKEN": {g.Token}}
	_, err := g.HTTP.JSON(ctx, http.MethodGet, strings.TrimRight(g.BaseURL, "/")+"/api/v4/"+path, params, headers, nil, result)
	return err
}

func (g GitLab) Check(ctx context.Context) (string, error) {
	var me model.Object
	if err := g.get(ctx, "user", nil, &me); err != nil {
		return "", err
	}
	user := me.Text("username")
	if user == "" {
		return "", errors.New("GitLab 响应缺少用户名")
	}
	return "GitLab 账户 " + user + " 已验证", nil
}

func (g GitLab) Collect(ctx context.Context, window Window) (Batch, error) {
	var me model.Object
	if err := g.get(ctx, "user", nil, &me); err != nil {
		return Batch{}, err
	}
	id := number(me["id"])
	if id == "0" || id == "" {
		return Batch{}, errors.New("GitLab 用户缺少 ID")
	}
	batch := Batch{Details: model.Object{}}
	for page := 1; page <= 10; page++ {
		var events []model.Object
		params := url.Values{"per_page": {"100"}, "page": {fmt.Sprint(page)}}
		if err := g.get(ctx, "users/"+url.PathEscape(id)+"/events", params, &events); err != nil {
			return batch, err
		}
		if len(events) == 0 {
			break
		}
		done := false
		for _, item := range events {
			at, err := time.Parse(time.RFC3339, item.Text("created_at"))
			if err != nil {
				continue
			}
			if at.Before(window.Since) {
				done = true
				continue
			}
			if at.After(window.Until) {
				continue
			}
			project := item.Object("project")
			title := fmt.Sprintf("GitLab %s · %s · %s", item.Text("action_name"), item.Text("target_type"), project.Text("path_with_namespace"))
			event := model.NewEvent(model.StableID("gitlab", number(item["id"])), "gitlab", number(item["id"]), item.Text("action_name"), title, at)
			event.URL, event.Updated, event.Raw = project.Text("web_url"), model.ISO(at), item
			event.Evidence, event.Confidence = "direct", 0.95
			event.Metadata = model.Object{"project": project.Text("path_with_namespace")}
			batch.Events = append(batch.Events, event)
		}
		if done || len(events) < 100 {
			break
		}
	}
	batch.Details["returned"] = len(batch.Events)
	return batch, nil
}

// WakaTime reads daily coding summaries, one event per project per day.
type WakaTime struct {
	HTTP    netclient.Client
	BaseURL string
	Key     string
}

func (w WakaTime) get(ctx context.Context, path string, params url.Values, result any) error {
	if strings.TrimSpace(w.Key) == "" {
		return errors.New("WakaTime 已启用但没有 API Key")
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(w.Key+":"))
	headers := http.Header{"Authorization": {auth}}
	_, err := w.HTTP.JSON(ctx, http.MethodGet, strings.TrimRight(w.BaseURL, "/")+"/"+path, params, headers, nil, result)
	return err
}

func (w WakaTime) Check(ctx context.Context) (string, error) {
	var result struct {
		Data model.Object `json:"data"`
	}
	if err := w.get(ctx, "user", nil, &result); err != nil {
		return "", err
	}
	if result.Data.Text("id") == "" {
		return "", errors.New("WakaTime 响应缺少账户信息")
	}
	return "WakaTime 账户已验证", nil
}

func parseDay(value string) (time.Time, error) {
	if at, err := time.Parse(time.RFC3339, value); err == nil {
		return at, nil
	}
	return time.Parse("2006-01-02", value)
}

func (w WakaTime) Collect(ctx context.Context, window Window) (Batch, error) {
	params := url.Values{
		"since": {window.Since.Format("2006-01-02")},
		"until": {window.Until.Format("2006-01-02")},
	}
	var result struct {
		Data []model.Object `json:"data"`
	}
	if err := w.get(ctx, "user/summaries", params, &result); err != nil {
		return Batch{}, err
	}
	batch := Batch{Details: model.Object{}}
	for _, day := range result.Data {
		date := day.Object("range").Text("start")
		at, err := parseDay(date)
		if err != nil {
			continue
		}
		for _, raw := range day.List("projects") {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			project := model.Object(item)
			minutes := 0.0
			if seconds, ok := project["total_seconds"].(json.Number); ok {
				if value, err := seconds.Float64(); err == nil {
					minutes = value / 60
				}
			}
			if minutes < 1 {
				continue
			}
			name := project.Text("name")
			title := fmt.Sprintf("编码 %s · %.0f 分钟", name, minutes)
			event := model.NewEvent(model.StableID("wakatime", date, name), "wakatime", name, "coding", title, at)
			event.Updated, event.Raw = model.ISO(at), project
			event.Evidence, event.Confidence = "direct", 0.9
			event.Metadata = model.Object{"project": name, "minutes": model.Float(minutes)}
			batch.Events = append(batch.Events, event)
		}
	}
	batch.Details["returned"] = len(batch.Events)
	return batch, nil
}

// ActivityWatch reads the local tracking server's window, browser, and AFK
// buckets for the collection window.
type ActivityWatch struct {
	HTTP    netclient.Client
	BaseURL string
}

func (a ActivityWatch) get(ctx context.Context, path string, result any) error {
	_, err := a.HTTP.JSON(ctx, http.MethodGet, strings.TrimRight(a.BaseURL, "/")+"/api/0/"+path, nil, nil, nil, result)
	return err
}

func (a ActivityWatch) Check(ctx context.Context) (string, error) {
	var buckets map[string]model.Object
	if err := a.get(ctx, "buckets", &buckets); err != nil {
		return "", err
	}
	if len(buckets) == 0 {
		return "", errors.New("ActivityWatch 没有返回任何存储桶")
	}
	return fmt.Sprintf("ActivityWatch 本机服务已响应 · %d 个存储桶", len(buckets)), nil
}

func (a ActivityWatch) Collect(ctx context.Context, window Window) (Batch, error) {
	var buckets map[string]model.Object
	if err := a.get(ctx, "buckets", &buckets); err != nil {
		return Batch{}, err
	}
	batch := Batch{Details: model.Object{}}
	// The watcher's window and browser buckets say what was used; AFK
	// buckets say what was not, and add nothing.
	for id, bucket := range buckets {
		kind := bucket.Text("type")
		if strings.Contains(id, "afk") || kind == "afkstatus" {
			continue
		}
		isWindow := strings.Contains(id, "window") || strings.Contains(kind, "window")
		isWeb := strings.Contains(id, "web") || strings.Contains(id, "browser") || strings.Contains(kind, "web")
		if !isWindow && !isWeb {
			continue
		}
		var events []model.Object
		path := "buckets/" + url.QueryEscape(id) + "/events?start=" + window.Since.Format(time.RFC3339Nano) + "&end=" + window.Until.Format(time.RFC3339Nano)
		if err := a.get(ctx, path, &events); err != nil {
			return batch, err
		}
		for _, item := range events {
			at, err := time.Parse(time.RFC3339, item.Text("timestamp"))
			if err != nil || at.Before(window.Since) || at.After(window.Until) {
				continue
			}
			data := item.Object("data")
			duration := 0.0
			if raw, ok := item["duration"].(json.Number); ok {
				if value, err := raw.Float64(); err == nil {
					duration = value
				}
			}
			if duration < 30 {
				continue
			}
			title, kind := data.Text("app")+" · "+data.Text("title"), "window"
			if isWeb {
				title = data.Text("domain")
				if title == "" {
					title = data.Text("url")
				}
				title = "浏览 " + title
				kind = "web"
			}
			title = strings.TrimSuffix(strings.TrimPrefix(title, " · "), " · ")
			if title == "" {
				continue
			}
			event := model.NewEvent(model.StableID("activitywatch", id, item.Text("timestamp"), title), "activitywatch", item.Text("id"), kind, title, at)
			event.Updated, event.Raw = model.ISO(at), item
			event.Evidence, event.Confidence = "direct", 0.6
			event.Metadata = model.Object{"seconds": model.Float(duration)}
			batch.Events = append(batch.Events, event)
		}
	}
	batch.Details["returned"] = len(batch.Events)
	return batch, nil
}

// LocalFolder watches a folder of notes: every file whose content changed in
// the window becomes one editing event. Obsidian and Logseq share it.
type LocalFolder struct {
	Name       string
	Root       string
	Extensions []string
	// Ignore holds directories that must never be read as notes: the app's
	// own exports live in the workspace, and scanning them would turn every
	// sync into next run's "notes".
	Ignore []string
}

func (l LocalFolder) ignored(path string) bool {
	for _, dir := range l.Ignore {
		if dir != "" && strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (l LocalFolder) Check(context.Context) (string, error) {
	if strings.TrimSpace(l.Root) == "" {
		return "", fmt.Errorf("%s 还没有配置目录路径", l.Name)
	}
	info, err := os.Stat(l.Root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s 路径不可读取，请检查目录是否存在", l.Name)
	}
	files := 0
	_ = filepath.WalkDir(l.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || l.ignored(path) {
			return nil
		}
		if l.match(path) {
			files++
		}
		return nil
	})
	return fmt.Sprintf("%s 路径可读取 · %d 个笔记文件", l.Name, files), nil
}

func (l LocalFolder) match(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, want := range l.Extensions {
		if ext == want {
			return true
		}
	}
	return false
}

func (l LocalFolder) Collect(_ context.Context, window Window) (Batch, error) {
	if strings.TrimSpace(l.Root) == "" {
		return Batch{}, fmt.Errorf("%s 还没有配置目录路径", l.Name)
	}
	type touched struct {
		path string
		at   time.Time
	}
	var found []touched
	err := filepath.WalkDir(l.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if l.ignored(path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "target" || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !l.match(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		at := info.ModTime()
		if at.Before(window.Since) || at.After(window.Until) {
			return nil
		}
		found = append(found, touched{path, at})
		return nil
	})
	if err != nil {
		return Batch{}, err
	}
	slices.SortFunc(found, func(a, b touched) int { return b.at.Compare(a.at) })
	batch := Batch{Details: model.Object{"scanned": len(found)}}
	for i, file := range found {
		if i >= 500 {
			batch.Details["truncated"] = true
			break
		}
		rel, _ := filepath.Rel(l.Root, file.path)
		rel = filepath.ToSlash(rel)
		title := fmt.Sprintf("编辑笔记 %s", strings.TrimSuffix(rel, filepath.Ext(rel)))
		event := model.NewEvent(model.StableID(l.Name, rel, fmt.Sprint(file.at.Unix())), strings.ToLower(l.Name), rel, "note_edit", title, file.at)
		event.URL = "file://" + file.path
		event.Updated, event.Evidence, event.Confidence = model.ISO(file.at), "direct", 0.8
		batch.Events = append(batch.Events, event)
	}
	return batch, nil
}
