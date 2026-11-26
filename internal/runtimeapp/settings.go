package runtimeapp

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"daily-log/internal/ai"
	"daily-log/internal/codex"
	"daily-log/internal/config"
	"daily-log/internal/model"
	"daily-log/internal/source"
	"daily-log/internal/store"
	"daily-log/internal/workspace"
)

func (w Workspace) updateSettings(ctx context.Context, apply func(config.Config, *config.Settings) error) error {
	unlock, err := store.Lock(ctx, config.SettingsPath(w.ConfigPath))
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := w.loadConfig()
	if err != nil {
		return err
	}
	settings := cfg.Settings
	if err := apply(cfg, &settings); err != nil {
		return err
	}
	return config.WriteSettings(cfg.Path, settings)
}

func (w Workspace) Connections(ctx context.Context) ([]workspace.Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := w.loadConfig()
	if err != nil {
		return nil, err
	}
	sources := cfg.Sources()
	specs := cfg.RegisteredSources()
	connections := make([]workspace.Connection, 0, len(specs))
	for _, spec := range specs {
		saved, recorded := cfg.Settings.Connections[spec.ID]
		// A source is in the user's list when they added or enabled it, or
		// when it shipped with the app before the catalog existed; only an
		// explicit removal sends it back to the catalog.
		added := (recorded && (saved.Added || saved.Enabled)) || spec.AddedByDefault
		item := workspace.Connection{
			ID: spec.ID, Label: spec.Label, Description: spec.Description,
			Enabled: sources[spec.ID].Enabled, Removed: saved.Removed,
			Integration: spec.Integration,
			Added:       added && !saved.Removed,
		}
		values, err := cfg.ConnectionValues(spec.ID)
		if err != nil {
			return nil, err
		}
		for _, field := range spec.Fields {
			item.Fields = append(item.Fields, workspace.Field{
				Key: field.Key, Label: field.Label, Kind: field.Kind, Value: values[field.Key],
				Configured: field.SecretKey != "" && cfg.SourceSecret(spec.ID, field.SecretKey) != "",
			})
		}
		connections = append(connections, item)
	}
	return connections, nil
}

func (w Workspace) SaveConnection(ctx context.Context, input workspace.ConnectionInput) error {
	return w.updateSettings(ctx, func(cfg config.Config, settings *config.Settings) error {
		change, err := prepareConnection(cfg, input)
		if err != nil {
			return err
		}
		if settings.Connections == nil {
			settings.Connections = map[string]config.Connection{}
		}
		settings.Connections[input.ID] = change
		return nil
	})
}

func (w Workspace) RemoveConnection(ctx context.Context, id string) error {
	return w.updateSettings(ctx, func(cfg config.Config, settings *config.Settings) error {
		spec, ok := cfg.FindSource(id)
		if !ok {
			return errors.New("找不到来源")
		}
		if settings.Connections == nil {
			settings.Connections = map[string]config.Connection{}
		}
		// Empty overrides also suppress old credentials from .env for this source.
		secrets := map[string]string{}
		for _, field := range spec.Fields {
			if field.SecretKey != "" {
				secrets[field.SecretKey] = ""
			}
		}
		settings.Connections[id] = config.Connection{Removed: true, Secrets: secrets}
		return nil
	})
}

func prepareConnection(cfg config.Config, input workspace.ConnectionInput) (config.Connection, error) {
	spec, ok := cfg.FindSource(input.ID)
	if !ok {
		return config.Connection{}, errors.New("找不到来源")
	}
	saved := cfg.Settings.Connections[input.ID]
	// Saving a connection adds it to the user's list; the catalog entry and
	// the saved card are then the same source.
	change := config.Connection{Enabled: input.Enabled, Added: true, Values: map[string]string{}, Secrets: map[string]string{}}
	maps.Copy(change.Values, saved.Values)
	maps.Copy(change.Values, input.Values)
	maps.Copy(change.Secrets, saved.Secrets)
	for key, value := range input.Secrets {
		secret := ""
		for _, field := range spec.Fields {
			if field.Key == key {
				secret = field.SecretKey
				break
			}
		}
		if secret == "" {
			return change, errors.New("未知的来源凭证")
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return change, errors.New("凭证须为单行文本")
		}
		change.Secrets[secret] = strings.TrimSpace(value)
	}
	if input.ID == "todoist" {
		if baseURL, edited := change.Values["base_url"]; edited {
			if err := config.CheckURL(strings.TrimSpace(baseURL), true); err != nil {
				return change, fmt.Errorf("Todoist API 地址：%w", err)
			}
		}
	}
	_, err := cfg.PreviewConnection(input.ID, change)
	return change, err
}

func (w Workspace) TestConnection(ctx context.Context, input workspace.ConnectionInput) (message string, err error) {
	cfg, err := w.loadConfig()
	if err != nil {
		return "", err
	}
	change, err := prepareConnection(cfg, input)
	if err != nil {
		return "", err
	}
	cfg, err = cfg.PreviewConnection(input.ID, change)
	if err != nil {
		return "", err
	}
	privacy := cfg.Redactor()
	defer func() { message, err = privacy.Text(message), privacy.Err(err) }()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cfg.Network = config.Network{Attempts: 1, TimeoutSeconds: 10}
	for _, registration := range w.Connectors {
		if registration.Spec.ID == input.ID {
			_, probe, err := registration.open(cfg)
			if err != nil {
				return "", err
			}
			return probe(ctx)
		}
	}
	_, integration, err := Configure(cfg, nil, w.Logger)
	if err != nil {
		return "", err
	}
	switch input.ID {
	case "zotero":
		err := integration.zotero.Check(ctx)
		return "Zotero 本地 API 已响应", err
	case "todoist":
		result, err := integration.todoist.Check(ctx)
		if err == nil && !result.Bool("account_access") {
			err = errors.New("Todoist 响应缺少账户信息")
		}
		return "Todoist 账户已验证", err
	case "calendar":
		if len(cfg.Calendar.CalendarIDs) == 0 {
			return "", errors.New("至少填写一个日历 ID")
		}
		now := time.Now().In(model.Shanghai)
		_, err := integration.calendar.Collect(ctx, source.Window{Since: now.Add(-time.Minute), Until: now})
		return "已验证配置的日历可读", err
	case "codex":
		if len(cfg.Codex.Hosts) == 0 {
			return "", errors.New("至少配置一台 Codex 主机")
		}
		for _, host := range cfg.Codex.Hosts {
			if err := codex.CheckHistory(ctx, host, 10*time.Second); err != nil {
				return "", fmt.Errorf("主机 %s：%w", host.Name, err)
			}
		}
		return fmt.Sprintf("%d 台主机的只读接口可用", len(cfg.Codex.Hosts)), nil
	default:
		spec, ok := cfg.FindSource(input.ID)
		if !ok || spec.Integration == "" {
			return "", errors.New("找不到来源")
		}
		collector, err := catalogCollector(input.ID, cfg, httpClient(cfg.Network))
		if err != nil {
			return "", err
		}
		message, err = collector.Check(ctx)
		return message, err
	}
}

func (w Workspace) Models(ctx context.Context) (workspace.ModelSettings, error) {
	if err := ctx.Err(); err != nil {
		return workspace.ModelSettings{}, err
	}
	cfg, err := w.loadConfig()
	if err != nil {
		return workspace.ModelSettings{}, err
	}
	result := workspace.ModelSettings{
		Default:                 cfg.Settings.EffectiveModelID(),
		LegacyName:              model.Object(cfg.AI).Text("model"),
		StopOnJournalModelError: cfg.Settings.StopOnJournalModelError,
	}
	for _, p := range cfg.Settings.Profiles {
		result.Profiles = append(result.Profiles, workspace.Profile{
			ID: p.ID, Name: p.Name, Model: p.Model, BaseURL: p.BaseURL,
			Enabled: p.Enabled, Anonymous: p.Anonymous, HasToken: p.Token != "",
		})
	}
	return result, nil
}

func (w Workspace) SetJournalModelFailureMode(ctx context.Context, stop bool) error {
	return w.updateSettings(ctx, func(_ config.Config, settings *config.Settings) error {
		settings.StopOnJournalModelError = stop
		return nil
	})
}

func (w Workspace) SaveModel(ctx context.Context, input workspace.ProfileInput) (string, error) {
	id := input.ID
	err := w.updateSettings(ctx, func(_ config.Config, settings *config.Settings) error {
		p, err := prepareModel(*settings, input)
		if err != nil {
			return err
		}
		if id == "" {
			var random [16]byte
			if _, err := rand.Read(random[:]); err != nil {
				return err
			}
			id = fmt.Sprintf("%x", random)
		}
		p.ID = id
		settings.ReplaceProfile(p)
		return nil
	})
	return id, err
}

func prepareModel(settings config.Settings, input workspace.ProfileInput) (config.Profile, error) {
	p := config.Profile{
		ID: input.ID, Name: strings.TrimSpace(input.Name), Model: strings.TrimSpace(input.Model),
		BaseURL: strings.TrimSpace(input.BaseURL), Enabled: input.Enabled, Anonymous: input.Anonymous,
	}
	if p.Name == "" || p.Model == "" {
		return p, errors.New("请填写配置名称和模型名称")
	}
	if err := config.CheckURL(p.BaseURL, true); err != nil {
		return p, err
	}
	if p.ID != "" {
		found := false
		for _, old := range settings.Profiles {
			if old.ID == p.ID {
				if old.Token != "" && input.Token == nil && strings.TrimRight(old.BaseURL, "/") != strings.TrimRight(p.BaseURL, "/") {
					return p, errors.New("更换模型服务地址时请重新输入该地址的 API 密钥")
				}
				p.Token, found = old.Token, true
				break
			}
		}
		if !found {
			return p, errors.New("模型配置已移除，请重新打开列表")
		}
	}
	if input.Token != nil {
		if strings.ContainsAny(*input.Token, "\r\n\x00") {
			return p, errors.New("模型凭证须为单行文本")
		}
		p.Token = strings.TrimSpace(*input.Token)
	}
	if p.Enabled && !p.Anonymous && p.Token == "" {
		return p, errors.New("启用模型需要 API 密钥或明确选择无密钥服务")
	}
	return p, nil
}

func (w Workspace) TestModel(ctx context.Context, input workspace.ProfileInput) error {
	cfg, err := w.loadConfig()
	if err != nil {
		return err
	}
	input.Enabled = true
	profile, err := prepareModel(cfg.Settings, input)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := ai.Client{Config: profile.ModelOptions(), Token: profile.Token}
	var result struct{ OK bool }
	if err := client.Complete(ctx, `这是连接测试。仅回复 JSON {"ok":true}。`, "daily-log connection test", &result); err != nil {
		return err
	}
	if !result.OK {
		return errors.New("模型已响应，但未返回预期 JSON")
	}
	return nil
}

// ListModels asks a service what it serves, without saving anything first:
// the form's current address and key are enough.
func (w Workspace) ListModels(ctx context.Context, input workspace.ProfileInput) ([]string, error) {
	cfg, err := w.loadConfig()
	if err != nil {
		return nil, err
	}
	privacy := cfg.Redactor()
	token := ""
	if input.Token != nil {
		token = strings.TrimSpace(*input.Token)
	} else if input.ID != "" {
		for _, profile := range cfg.Settings.Profiles {
			if profile.ID == input.ID {
				token = profile.Token
			}
		}
	}
	profile := config.Profile{BaseURL: strings.TrimSpace(input.BaseURL), Anonymous: input.Anonymous, Token: token}
	client := ai.Client{Config: profile.ModelOptions(), Token: profile.Token}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	models, err := client.ListModels(ctx)
	return models, privacy.Err(err)
}

func (w Workspace) RemoveModel(ctx context.Context, id string) error {
	return w.updateSettings(ctx, func(_ config.Config, settings *config.Settings) error {
		settings.RemoveProfile(id)
		return nil
	})
}

func (w Workspace) SetDefaultModel(ctx context.Context, id string) error {
	return w.updateSettings(ctx, func(_ config.Config, settings *config.Settings) error {
		if id != "" {
			found := false
			for _, profile := range settings.Profiles {
				found = found || profile.ID == id && profile.Enabled
			}
			if !found {
				return errors.New("请先启用该模型配置")
			}
		}
		if settings.Selections == nil {
			settings.Selections = map[string]string{}
		}
		settings.DefaultModel = id
		settings.Selections[config.JournalModel] = id
		settings.Selections[config.CodexModel] = id
		return nil
	})
}
