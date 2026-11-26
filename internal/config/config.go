package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"daily-log/internal/codex"
	"daily-log/internal/model"
	"daily-log/internal/privatefs"
	resources "daily-log/internal/resources"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Source struct {
	Enabled  bool  `yaml:"enabled"`
	Required *bool `yaml:"required"`
}

func (s Source) IsRequired() bool { return s.Required == nil || *s.Required }

type Zotero struct {
	Source            `yaml:",inline"`
	BaseURL           string `yaml:"base_url"`
	Library           string `yaml:"library"`
	SessionGapMinutes int    `yaml:"session_gap_minutes"`
}

type Todoist struct {
	Source          `yaml:",inline"`
	BaseURL         string `yaml:"base_url"`
	HistoryDaysFree int    `yaml:"history_days_free"`
}

type Calendar struct {
	Source      `yaml:",inline"`
	CalendarIDs []string `yaml:"calendar_ids"`
}

type Codex struct {
	StartDate      string `yaml:"start_date"`
	Source         `yaml:",inline"`
	Hosts          []codex.Host         `yaml:"hosts"`
	TimeoutSeconds float64              `yaml:"timeout_seconds"`
	CacheDirectory string               `yaml:"cache_directory"`
	UseAI          *bool                `yaml:"use_ai"`
	Rules          []codex.CategoryRule `yaml:"category_rules"`
	Overrides      map[string]string    `yaml:"category_overrides"`
}

type Output struct {
	AutoLayout     bool   `yaml:"auto_layout"`
	ActivityColors bool   `yaml:"activity_colors"`
	Enabled        bool   `yaml:"enabled"`
	Directory      string `yaml:"directory"`
	Path           string `yaml:"path"`
	SpreadsheetID  string `yaml:"spreadsheet_id"`
}

type Network struct {
	Attempts       int     `yaml:"attempts"`
	TimeoutSeconds float64 `yaml:"timeout_seconds"`
	BackoffSeconds float64 `yaml:"backoff_seconds"`
}

type Config struct {
	Settings     Settings `yaml:"-"`
	env          map[string]string
	additional   []SourceSpec
	Path         string         `yaml:"-"`
	Root         string         `yaml:"-"`
	Timezone     string         `yaml:"timezone"`
	Database     string         `yaml:"database"`
	LogFile      string         `yaml:"log_file"`
	LookbackDays int            `yaml:"lookback_days"`
	Zotero       Zotero         `yaml:"zotero"`
	Todoist      Todoist        `yaml:"todoist"`
	Calendar     Calendar       `yaml:"calendar"`
	Codex        Codex          `yaml:"codex"`
	Markdown     Output         `yaml:"markdown"`
	Sheets       Output         `yaml:"sheets"`
	Excel        Output         `yaml:"excel"`
	AI           map[string]any `yaml:"ai"`
	Network      Network        `yaml:"network"`
}

func (c Config) Sources() map[string]Source {
	result := map[string]Source{
		"zotero": c.Zotero.Source, "todoist": c.Todoist.Source, "calendar": c.Calendar.Source,
		"codex": c.Codex.Source,
	}
	for _, spec := range c.RegisteredSources() {
		if _, typed := result[spec.ID]; typed {
			continue
		}
		// Catalog sources live only in the settings file: their enabled state
		// is exactly what the user saved there.
		connection := c.Settings.Connections[spec.ID]
		result[spec.ID] = Source{Enabled: connection.Enabled && !connection.Removed}
	}
	return result
}

// DefaultPath keeps an existing configuration in the current directory
// usable and gives new desktop installations a per-user location.
func DefaultPath() (string, error) {
	const name = "config.yaml"
	if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return name, err
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daily-log", name), nil
}

func Init(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = file.Write(resources.DefaultConfig)
	return path, errors.Join(err, file.Close())
}

func defaults() Config {
	return Config{
		Timezone: "Asia/Shanghai", LookbackDays: 3,
		Calendar: Calendar{CalendarIDs: []string{"primary"}},
		Codex:    Codex{TimeoutSeconds: 45, CacheDirectory: "data/codex-cache", Hosts: []codex.Host{{Name: "local", Command: []string{"codex", "app-server", "--listen", "stdio://"}}}},
		Zotero:   Zotero{BaseURL: "http://localhost:23119/api", Library: "users/0", SessionGapMinutes: 30},
		Todoist:  Todoist{BaseURL: "https://api.todoist.com/api/v1", HistoryDaysFree: 7},
		Markdown: Output{Directory: "outputs/markdown"},
		Network:  Network{Attempts: 3, TimeoutSeconds: 20, BackoffSeconds: 2},
	}
}

func Load(path string, additional ...SourceSpec) (Config, error) {
	c := defaults()
	for _, spec := range additional {
		if err := c.registerSource(spec); err != nil {
			return c, err
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return c, errors.New("找不到配置，请先运行 daily-log init 或通过 --config 指定配置文件")
	}
	if err != nil {
		return c, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err = decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("无法解析配置：%w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return c, errors.New("配置文件必须只包含一份 YAML 文档")
	}
	c.Settings, err = ReadSettings(absolute)
	if err != nil {
		return c, err
	}
	for name, change := range c.Settings.Connections {
		if _, exists := c.FindSource(name); !exists {
			// Removed sources may remain in older settings files; keep their
			// secrets untouched but do not register or execute them again.
			continue
		}
		if err := c.applyConnection(name, change); err != nil {
			return c, err
		}
	}
	if c.Settings.Schedule != nil {
		if !model.ValidDailyClock(c.Settings.Schedule.Clock) {
			return c, errors.New("定时时间须使用 HH:MM")
		}
		c.LookbackDays = c.Settings.Schedule.LookbackDays
	}
	if err := c.validate(); err != nil {
		return c, err
	}
	c.Path, c.Root = absolute, filepath.Dir(absolute)
	c.Database = c.Resolve(c.Database)
	c.LogFile = c.Resolve(c.LogFile)
	c.Markdown.Directory = c.Resolve(c.Markdown.Directory)
	if c.Excel.Path != "" {
		c.Excel.Path = c.Resolve(c.Excel.Path)
	}
	envFile := c.Resolve(".env")
	if err := privatefs.Restrict(envFile); err != nil {
		return c, err
	}
	c.env, err = godotenv.Read(envFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, errors.New("无法读取 .env，请检查文件格式")
	}
	preserveFloats(c.AI)
	return c, nil
}

func (c Config) validate() error {
	if c.Timezone != "Asia/Shanghai" || c.LookbackDays < 3 || c.Zotero.SessionGapMinutes <= 0 {
		return errors.New("timezone 须为 Asia/Shanghai，lookback_days 至少为 3，session_gap_minutes 须为正数")
	}
	if c.Database == "" || c.LogFile == "" {
		return errors.New("配置必须包含 database 和 log_file")
	}
	if c.Network.Attempts < 1 || c.Network.TimeoutSeconds <= 0 || c.Network.BackoffSeconds < 0 {
		return errors.New("network.attempts 和 timeout_seconds 须为正数，backoff_seconds 不能为负数")
	}
	if c.Todoist.Enabled && c.Todoist.HistoryDaysFree <= 0 {
		return errors.New("todoist.history_days_free 须为正数")
	}
	if c.Codex.Enabled && c.Codex.TimeoutSeconds <= 0 {
		return errors.New("codex.timeout_seconds 须为正数")
	}
	return nil
}

func (c Config) Resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(c.Root, path)
}

func preserveFloats(value any) any {
	switch value := value.(type) {
	case map[string]any:
		for key, entry := range value {
			value[key] = preserveFloats(entry)
		}
	case []any:
		for i, entry := range value {
			value[i] = preserveFloats(entry)
		}
	case float64:
		return model.Float(value)
	}
	return value
}
