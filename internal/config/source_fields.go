package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"daily-log/internal/codex"
	"daily-log/internal/model"
)

type Field struct {
	Key, Label, Kind, SecretKey string
	Default                     string
}

type SourceSpec struct {
	ID, Label, Description string
	Fields                 []Field
	// Integration names the standard way this service is reached:
	// "api" (official HTTP API), "local-api" (service on this machine),
	// "file" (a folder on this machine), "oauth", or "command".
	Integration string
	// AddedByDefault sources appear in 来源管理 from the start; the rest
	// wait in the catalog until the user adds one.
	AddedByDefault bool
}

var builtInSources = []SourceSpec{
	{ID: "zotero", Label: "Zotero", Description: "读取本机 Zotero 的标注、笔记和论文元数据。", Integration: "local-api", AddedByDefault: true, Fields: []Field{
		{Key: "base_url", Label: "本地 API 地址", Kind: "url", Default: "http://localhost:23119/api"},
		{Key: "library", Label: "资料库路径", Kind: "text", Default: "users/0"},
		{Key: "session_gap_minutes", Label: "阅读会话间隔（分钟）", Kind: "int", Default: "30"},
	}},
	{ID: "todoist", Label: "Todoist", Description: "读取本人完成的任务；可用历史取决于账号。", Integration: "api", AddedByDefault: true, Fields: []Field{
		{Key: "base_url", Label: "API 地址", Kind: "url", Default: "https://api.todoist.com/api/v1"},
		{Key: "token", Label: "API 令牌", Kind: "secret", SecretKey: "TODOIST_API_TOKEN"},
	}},
	{ID: "codex", Label: "Codex", Description: "通过已配置主机的只读历史接口读取允许的会话。", Integration: "command", AddedByDefault: true, Fields: []Field{
		{Key: "hosts", Label: "主机与只读命令（JSON）", Kind: "hosts"},
		{Key: "start_date", Label: "起始日期（可选）", Kind: "date"},
	}},
	{ID: "calendar", Label: "Google Calendar", Description: "只读已授权日历。Google 令牌和服务账号使用本机凭证文件。", Integration: "oauth", AddedByDefault: true, Fields: []Field{
		{Key: "calendar_ids", Label: "日历 ID（每行一项）", Kind: "list", Default: "primary"},
		{Key: "oauth_path", Label: "OAuth 令牌文件路径", Kind: "secret", SecretKey: "GOOGLE_OAUTH_TOKEN"},
		{Key: "account_path", Label: "服务账号文件路径", Kind: "secret", SecretKey: "GOOGLE_APPLICATION_CREDENTIALS"},
	}},
	{ID: "github", Label: "GitHub", Description: "读取本人的推送、Pull Request、议题与评审事件。", Integration: "api", Fields: []Field{
		{Key: "base_url", Label: "API 地址", Kind: "url", Default: "https://api.github.com"},
		{Key: "username", Label: "用户名（留空自动识别）", Kind: "text"},
		{Key: "token", Label: "API 令牌", Kind: "secret", SecretKey: "GITHUB_TOKEN"},
	}},
	{ID: "gitlab", Label: "GitLab", Description: "读取本人的推送、合并请求与议题事件。", Integration: "api", Fields: []Field{
		{Key: "base_url", Label: "API 地址", Kind: "url", Default: "https://gitlab.com"},
		{Key: "token", Label: "访问令牌", Kind: "secret", SecretKey: "GITLAB_TOKEN"},
	}},
	{ID: "wakatime", Label: "WakaTime", Description: "读取编码时长，按项目与语言汇总。", Integration: "api", Fields: []Field{
		{Key: "base_url", Label: "API 地址", Kind: "url", Default: "https://wakatime.com/api/v1"},
		{Key: "api_key", Label: "API Key", Kind: "secret", SecretKey: "WAKATIME_API_KEY"},
	}},
	{ID: "activitywatch", Label: "ActivityWatch", Description: "读取本机 ActivityWatch 的窗口、浏览与挂机记录。", Integration: "local-api", Fields: []Field{
		{Key: "base_url", Label: "本机 API 地址", Kind: "url", Default: "http://127.0.0.1:5600"},
	}},
	{ID: "obsidian", Label: "Obsidian", Description: "按修改时间记录本机 Vault 中的笔记编辑。", Integration: "file", Fields: []Field{
		{Key: "vault_path", Label: "Vault 路径", Kind: "text"},
	}},
	{ID: "logseq", Label: "Logseq", Description: "按修改时间记录本机图谱中的 Markdown 与 Org 编辑。", Integration: "file", Fields: []Field{
		{Key: "graph_path", Label: "图谱路径", Kind: "text"},
	}},
}

func (c Config) RegisteredSources() []SourceSpec {
	return slices.Concat(builtInSources, c.additional)
}

func (c Config) FindSource(name string) (SourceSpec, bool) {
	for _, spec := range c.RegisteredSources() {
		if spec.ID == name {
			return spec, true
		}
	}
	return SourceSpec{}, false
}

func (c *Config) registerSource(spec SourceSpec) error {
	if _, exists := c.FindSource(spec.ID); exists {
		return fmt.Errorf("重复登记来源 %q", spec.ID)
	}
	if spec.ID == "" || spec.Label == "" || strings.ContainsAny(spec.ID, " :\r\n\t/") ||
		slices.Contains([]string{"ai", "markdown", "excel", "sheets"}, spec.ID) {
		return fmt.Errorf("来源标识或名称无效 %q", spec.ID)
	}
	c.additional = append(c.additional, spec)
	return nil
}

func (s SourceSpec) field(key string) (Field, bool) {
	for _, field := range s.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return Field{}, false
}

func CheckURL(value string, localHTTP bool) error {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return errors.New("请输入有效的服务地址")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	loopback := strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
	if u.Scheme == "https" || u.Scheme == "http" && (!localHTTP || loopback) {
		return nil
	}
	if localHTTP {
		return errors.New("远端服务须使用 HTTPS，本机服务可使用 HTTP")
	}
	return errors.New("服务地址须使用 HTTP 或 HTTPS")
}

func (f Field) parse(value string) (any, error) {
	value = strings.TrimSpace(value)
	switch f.Kind {
	case "int":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%s须填写正整数", f.Label)
		}
		return n, nil
	case "url":
		if err := CheckURL(value, false); err != nil {
			return nil, fmt.Errorf("%s：%w", f.Label, err)
		}
	case "list":
		lines := []string{}
		for _, line := range strings.Split(value, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				lines = append(lines, line)
			}
		}
		return lines, nil
	case "hosts":
		var hosts []codex.Host
		if err := model.Decode([]byte(value), &hosts); err != nil || len(hosts) == 0 {
			return nil, fmt.Errorf("%s须填写有效的非空 JSON 数组", f.Label)
		}
		seen := map[string]bool{}
		for _, host := range hosts {
			if strings.TrimSpace(host.Name) == "" || len(host.Command) == 0 || strings.TrimSpace(host.Command[0]) == "" || seen[host.Name] {
				return nil, errors.New("各 Codex 主机须有唯一名称和可执行命令")
			}
			seen[host.Name] = true
		}
		return hosts, nil
	case "date":
		if value != "" {
			if _, err := model.ParseDay(value); err != nil {
				return nil, fmt.Errorf("%s须使用 YYYY-MM-DD", f.Label)
			}
		}
	case "text":
		if value == "" {
			return nil, fmt.Errorf("%s不能为空", f.Label)
		}
	}
	return value, nil
}

func (f Field) format(value any) string {
	if value == nil {
		return ""
	}
	switch f.Kind {
	case "hosts":
		data, _ := json.MarshalIndent(value, "", "  ")
		return string(data)
	case "list":
		items, _ := value.([]any)
		lines := make([]string, 0, len(items))
		for _, item := range items {
			lines = append(lines, fmt.Sprint(item))
		}
		return strings.Join(lines, "\n")
	default:
		return fmt.Sprint(value)
	}
}
