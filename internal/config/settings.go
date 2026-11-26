package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"daily-log/internal/model"
	"daily-log/internal/privatefs"

	"gopkg.in/yaml.v3"
)

const (
	JournalModel = "journal"
	CodexModel   = "codex"
)

type Connection struct {
	Enabled bool              `json:"enabled"`
	Removed bool              `json:"removed,omitempty"`
	Added   bool              `json:"added,omitempty"`
	Values  map[string]string `json:"values,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

type Profile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	Token     string `json:"token,omitempty"`
	Enabled   bool   `json:"enabled"`
	Anonymous bool   `json:"anonymous,omitempty"`
}

type Schedule struct {
	Clock         string   `json:"clock"`
	LookbackDays  int      `json:"lookback_days"`
	Excluded      []string `json:"excluded,omitempty"`
	LastInstalled string   `json:"last_installed,omitempty"`
	AppliedClock  string   `json:"applied_clock,omitempty"`
}

type Settings struct {
	Schedule                *Schedule             `json:"schedule,omitempty"`
	Connections             map[string]Connection `json:"connections,omitempty"`
	Profiles                []Profile             `json:"profiles,omitempty"`
	Categories              []string              `json:"categories,omitempty"`
	CategoryShorts          map[string]string     `json:"category_shorts,omitempty"`
	DefaultModel            string                `json:"default_model,omitempty"`
	Selections              map[string]string     `json:"selections,omitempty"`
	StopOnJournalModelError bool                  `json:"stop_on_journal_model_error,omitempty"`
}

func DefaultCategoryNames() []string {
	return []string{model.Research, model.Course, model.Career, model.Life}
}

func (s Settings) CategoryNames() []string {
	if len(s.Categories) == 0 {
		return DefaultCategoryNames()
	}
	return append([]string(nil), s.Categories...)
}

// defaultCategoryShorts are the short labels the four built-in categories
// have always shown in the journal sheet. A saved 简称 overrides them, and a
// new category with no 简称 simply shows its full name — the same rule for
// every category, editable in 分类管理.
var defaultCategoryShorts = map[string]string{
	model.Research: "科研", model.Course: "课程", model.Career: "职业", model.Life: "生活",
}

func (s Settings) CategoryShort(name string) string {
	if short := strings.TrimSpace(s.CategoryShorts[name]); short != "" {
		return short
	}
	if short := defaultCategoryShorts[name]; short != "" {
		return short
	}
	return name
}

func SettingsPath(path string) string { return path + ".settings.json" }

// retiredConnectionValues are fields an older version saved and a newer one
// no longer knows: dropping them on read keeps a saved workspace loading,
// instead of failing every operation on an unknown-key error.
var retiredConnectionValues = map[string]bool{
	"course_projects": true, "include_thread_ids": true, "exclude_thread_ids": true,
}

func (s *Settings) sanitizeRetired() {
	for id, connection := range s.Connections {
		for key := range connection.Values {
			if retiredConnectionValues[key] {
				delete(connection.Values, key)
			}
		}
		s.Connections[id] = connection
	}
}

func ReadSettings(path string) (Settings, error) {
	var settings Settings
	path = SettingsPath(path)
	if err := privatefs.Restrict(path); err != nil {
		return settings, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if err := model.Decode(data, &settings); err != nil {
		return settings, errors.New("无法解析应用设置文件")
	}
	settings.sanitizeRetired()
	return settings, nil
}

func WriteSettings(path string, settings Settings) error {
	data, err := model.PrettyJSON(settings)
	if err != nil {
		return err
	}
	return privatefs.Write(SettingsPath(path), data)
}

func (c Config) Secret(key string) string {
	for _, spec := range c.RegisteredSources() {
		for _, field := range spec.Fields {
			if field.SecretKey == key {
				return c.SourceSecret(spec.ID, key)
			}
		}
	}
	return c.SourceSecret("", key)
}

func (c Config) SourceSecret(source, key string) string {
	if value, ok := c.Settings.Connections[source].Secrets[key]; ok {
		return value
	}
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return c.env[key]
}

func (c Config) Redactor() model.Redactor {
	values := []string{c.Secret("AI_API_KEY")}
	for _, spec := range c.RegisteredSources() {
		for _, field := range spec.Fields {
			if field.SecretKey != "" {
				values = append(values, c.SourceSecret(spec.ID, field.SecretKey))
			}
		}
	}
	for _, p := range c.Settings.Profiles {
		values = append(values, p.Token)
	}
	return model.NewRedactor(values)
}

func (s Settings) EffectiveModelID() string {
	if s.DefaultModel != "" {
		return s.DefaultModel
	}
	if selected := s.Selections[JournalModel]; selected != "" {
		return selected
	}
	if selected := s.Selections[CodexModel]; selected != "" {
		return selected
	}
	return ""
}

func (c Config) Model(string) (model.Object, string) {
	selected := c.Settings.EffectiveModelID()
	if selected == "" {
		return model.Object(c.AI), c.Secret("AI_API_KEY")
	}
	for _, profile := range c.Settings.Profiles {
		if profile.ID == selected && profile.Enabled {
			return profile.ModelOptions(), profile.Token
		}
	}
	return model.Object{"enabled": false}, ""
}

func (p Profile) ModelOptions() model.Object {
	return model.Object{
		"enabled": true, "base_url": p.BaseURL, "model": p.Model, "anonymous": p.Anonymous,
	}
}

func (s *Settings) ReplaceProfile(profile Profile) {
	at := slices.IndexFunc(s.Profiles, func(existing Profile) bool { return existing.ID == profile.ID })
	if at >= 0 {
		s.Profiles[at] = profile
	} else {
		s.Profiles = append(s.Profiles, profile)
	}
	if !profile.Enabled {
		s.clearSelections(profile.ID)
	}
	if profile.Enabled && s.EffectiveModelID() == "" {
		s.DefaultModel = profile.ID
	}
}

func (s *Settings) RemoveProfile(id string) {
	s.Profiles = slices.DeleteFunc(s.Profiles, func(profile Profile) bool { return profile.ID == id })
	s.clearSelections(id)
}

func (s *Settings) clearSelections(id string) {
	// With no profile left selected there is no model: 整理 will ask for one
	// rather than fall back to rules.
	if s.DefaultModel == id {
		s.DefaultModel = ""
	}
	for task, selected := range s.Selections {
		if selected == id {
			s.Selections[task] = ""
		}
	}
}

func (c *Config) applyConnection(name string, connection Connection) error {
	spec, ok := c.FindSource(name)
	if !ok {
		return fmt.Errorf("未知来源 %q", name)
	}
	section := c.connectionSection(name)
	if connection.Removed {
		section = c.resetConnection(name)
	}
	values := map[string]any{"enabled": connection.Enabled && !connection.Removed}
	if !connection.Removed {
		for key, value := range connection.Values {
			field, ok := spec.field(key)
			if !ok || field.SecretKey != "" {
				return fmt.Errorf("%s：未知配置项 %q", spec.Label, key)
			}
			parsed, err := field.parse(value)
			if err != nil {
				return fmt.Errorf("%s：%w", spec.Label, err)
			}
			values[key] = parsed
		}
	}
	if section == nil {
		return nil
	}
	data, err := yaml.Marshal(values)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, section)
}

func (c *Config) connectionSection(name string) any {
	switch name {
	case "zotero":
		return &c.Zotero
	case "todoist":
		return &c.Todoist
	case "calendar":
		return &c.Calendar
	case "codex":
		return &c.Codex
	default:
		return nil
	}
}

func (c *Config) resetConnection(name string) any {
	current, defaults := c.connectionSection(name), defaults()
	switch target := current.(type) {
	case *Zotero:
		*target = defaults.Zotero
	case *Todoist:
		*target = defaults.Todoist
	case *Calendar:
		*target = defaults.Calendar
	case *Codex:
		*target = defaults.Codex
	}
	return current
}

func (c Config) ConnectionValues(name string) (map[string]string, error) {
	spec, ok := c.FindSource(name)
	if !ok {
		return nil, fmt.Errorf("未知来源 %q", name)
	}
	section := c.connectionSection(name)
	if section == nil {
		values := map[string]string{}
		saved := c.Settings.Connections[name]
		for _, field := range spec.Fields {
			if field.SecretKey != "" || saved.Removed {
				continue
			}
			// A catalog source keeps its standard defaults until the user
			// edits the field, so adding GitHub needs no typing.
			if value := saved.Values[field.Key]; value != "" {
				values[field.Key] = value
			} else {
				values[field.Key] = field.Default
			}
		}
		return values, nil
	}
	data, err := yaml.Marshal(section)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := yaml.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, field := range spec.Fields {
		if field.SecretKey == "" {
			values[field.Key] = field.format(object[field.Key])
		}
	}
	return values, nil
}

func (c Config) PreviewConnection(name string, changes Connection) (Config, error) {
	saved := c.Settings.Connections[name]
	if saved.Removed && !changes.Removed {
		c.resetConnection(name)
	}
	// Use a separate settings map, so a connectivity test can't mutate a
	// loaded workspace or write the input credentials to disk.
	settings := c.Settings
	settings.Connections = make(map[string]Connection, len(c.Settings.Connections)+1)
	for key, value := range c.Settings.Connections {
		settings.Connections[key] = value
	}
	settings.Connections[name] = changes
	c.Settings = settings
	if err := c.applyConnection(name, changes); err != nil {
		return c, err
	}
	if err := c.validate(); err != nil {
		return c, err
	}
	return c, nil
}

func (c Config) Schedule() Schedule {
	if c.Settings.Schedule != nil {
		return *c.Settings.Schedule
	}
	return Schedule{Clock: model.DefaultDailyClock, LookbackDays: c.LookbackDays}
}
