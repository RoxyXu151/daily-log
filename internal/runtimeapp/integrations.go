package runtimeapp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"runtime"
	"strings"
	"time"

	"daily-log/internal/ai"
	"daily-log/internal/app"
	"daily-log/internal/codex"
	"daily-log/internal/config"
	"daily-log/internal/export"
	"daily-log/internal/googleauth"
	"daily-log/internal/model"
	"daily-log/internal/netclient"
	"daily-log/internal/source"
	"daily-log/internal/store"
	"daily-log/internal/tables"
)

type Integrations struct {
	config   config.Config
	db       *store.DB
	google   googleauth.Credentials
	todoist  source.Todoist
	zotero   source.Zotero
	calendar source.Calendar
}

func httpClient(options config.Network) netclient.Client {
	return netclient.Client{
		HTTP:     &http.Client{Timeout: time.Duration(options.TimeoutSeconds * float64(time.Second))},
		Attempts: options.Attempts, Backoff: time.Duration(options.BackoffSeconds * float64(time.Second)),
	}
}

func Configure(c config.Config, db *store.DB, logger *slog.Logger, registrations ...SourceRegistration) (app.Service, Integrations, error) {
	client := httpClient(c.Network)
	i := Integrations{
		config: c, db: db,
		google: googleauth.Credentials{Root: c.Root, ServiceAccount: c.Secret("GOOGLE_APPLICATION_CREDENTIALS"), UserToken: c.Secret("GOOGLE_OAUTH_TOKEN")},
		todoist: source.Todoist{
			HTTP: client, BaseURL: c.Todoist.BaseURL, Token: c.Secret("TODOIST_API_TOKEN"),
			HistoryDaysFree: c.Todoist.HistoryDaysFree,
		},
		zotero: source.Zotero{HTTP: client, BaseURL: c.Zotero.BaseURL, Library: c.Zotero.Library},
	}
	modelOptions, modelToken := c.Model("")
	aiClient := ai.Client{Config: modelOptions, Token: modelToken}
	useAI := modelOptions.Bool("enabled")
	if c.Settings.EffectiveModelID() == "" && c.Codex.UseAI != nil {
		useAI = *c.Codex.UseAI
	}
	codexCollector := codex.Collector{
		Hosts: c.Codex.Hosts, Timeout: time.Duration(c.Codex.TimeoutSeconds * float64(time.Second)),
		Cache: c.Resolve(c.Codex.CacheDirectory),
		UseAI: useAI, AI: ai.Client{Config: modelOptions, Token: modelToken}, Rules: c.Codex.Rules, Overrides: c.Codex.Overrides,
	}
	i.calendar = source.Calendar{
		IDs: c.Calendar.CalendarIDs,
		Client: func(ctx context.Context) (netclient.Client, error) {
			authenticated := client
			var err error
			authenticated.HTTP, err = i.google.Client(ctx, client.HTTP.Timeout, googleauth.CalendarScope)
			return authenticated, err
		},
	}
	planning := c.Schedule()
	s := app.Service{DB: db, DailyClock: planning.Clock, ExcludedEventTypes: planning.Excluded, Lookback: c.LookbackDays, SessionGap: c.Zotero.SessionGapMinutes, Categories: c.Settings.CategoryNames(), AIConfig: modelOptions, Logger: logger, Privacy: c.Redactor()}
	s.StopOnJournalModelError = c.Settings.StopOnJournalModelError
	for _, input := range []struct {
		name    string
		options config.Source
		collect source.Collector
	}{
		{"zotero", c.Zotero.Source, i.zotero.Collect},
		{"todoist", c.Todoist.Source, i.todoist.Collect},
		{"calendar", c.Calendar.Source, i.calendar.Collect},
		{"codex", c.Codex.Source, codexCollector.Collect},
	} {
		if !input.options.Enabled {
			continue
		}
		entry := app.Input{Name: input.name, Required: input.options.IsRequired(), Collect: input.collect}
		if input.name == "codex" && c.Codex.StartDate != "" {
			var err error
			entry.Start, err = model.ParseDay(c.Codex.StartDate)
			if err != nil {
				return s, i, fmt.Errorf("codex.start_date 必须使用 YYYY-MM-DD")
			}
		}
		s.Inputs = append(s.Inputs, entry)
	}
	// Catalog sources (GitHub, GitLab, WakaTime, ActivityWatch, Obsidian,
	// Logseq) are described entirely by their spec: enabled means wired,
	// with the optional flag so one failing service never blocks the day.
	typed := map[string]bool{"zotero": true, "todoist": true, "calendar": true, "codex": true}
	enabled := c.Sources()
	for _, spec := range c.RegisteredSources() {
		if typed[spec.ID] || spec.Integration == "" || !enabled[spec.ID].Enabled {
			continue
		}
		if spec.Integration == "file" {
			// A folder source without a folder is unconfigured, not broken:
			// it stays out of the run until the user points it somewhere.
			values, err := c.ConnectionValues(spec.ID)
			if err != nil || strings.TrimSpace(values[filePathKey(spec.ID)]) == "" {
				continue
			}
		}
		collector, err := catalogCollector(spec.ID, c, client)
		if err != nil {
			return s, i, err
		}
		s.Inputs = append(s.Inputs, app.Input{Name: spec.ID, Required: false, Collect: collector.Collect})
	}
	i.configureConnectors(&s, registrations)
	if c.Markdown.Enabled {
		s.Outputs = append(s.Outputs, markdownOutput(db, c.Markdown.Directory, s.Privacy))
	}
	for _, name := range i.tableOutputs() {
		s.Outputs = append(s.Outputs, app.Output{Name: name, Write: func(ctx context.Context) (model.Object, error) { return i.writeTable(ctx, name) }})
	}

	if aiClient.Config.Bool("enabled") {
		s.Digest = aiClient.DigestDay
	}
	return s, i, nil
}

func MarkdownOutput(db *store.DB, directory string) app.Output {
	return markdownOutput(db, directory, model.Redactor{})
}

func markdownOutput(db *store.DB, directory string, privacy model.Redactor) app.Output {
	exporter := export.Markdown{Directory: directory, Records: db, Privacy: privacy}
	return app.Output{Name: "markdown", Write: exporter.Write}
}

func (i Integrations) tableOutputs() []string {
	var outputs []string
	if i.config.Excel.Enabled {
		outputs = append(outputs, "excel")
	}
	if i.config.Sheets.Enabled {
		outputs = append(outputs, "sheets")
	}
	return outputs
}

func (i Integrations) Enabled() map[string]bool {
	modelOptions, _ := i.config.Model("")
	result := map[string]bool{
		"ai":       modelOptions.Bool("enabled"),
		"markdown": i.config.Markdown.Enabled, "excel": i.config.Excel.Enabled, "sheets": i.config.Sheets.Enabled,
	}
	for name, options := range i.config.Sources() {
		result[name] = options.Enabled
	}
	return result
}

func (i Integrations) Inspect(ctx context.Context) (model.Object, error) {
	result := model.Object{}
	for _, name := range i.tableOutputs() {
		value, err := i.inspectTable(ctx, name)
		if err != nil {
			return result, err
		}
		result[name] = value
	}
	return result, nil
}

func (i Integrations) Doctor(ctx context.Context) (model.Object, error) {
	checks := model.Object{}
	result := model.Object{"go": runtime.Version(), "timezone": i.config.Timezone, "checks": checks}
	checks["todoist"] = model.Object{"enabled": false, "token_configured": strings.TrimSpace(i.todoist.Token) != ""}
	if i.config.Todoist.Enabled {
		i.todoist.HTTP = httpClient(config.Network{Attempts: 1, TimeoutSeconds: 8})
		status, err := i.todoist.Check(ctx)
		checks["todoist"] = status
		if err != nil {
			return result, err
		}
	}
	if i.config.Zotero.Enabled {
		i.zotero.HTTP = httpClient(config.Network{Attempts: 1, TimeoutSeconds: 4})
		checks["zotero"] = "连通"
		if err := i.zotero.Check(ctx); err != nil {
			checks["zotero"] = "不可用，请启动 Zotero 并启用 Local API"
		}
	}
	if i.config.Calendar.Enabled {
		if _, err := i.google.Client(ctx, 8*time.Second, googleauth.CalendarScope); err != nil {
			return result, err
		}
		checks["google_credentials"] = "已加载"
	}
	for _, name := range i.tableOutputs() {
		value, err := i.inspectTable(ctx, name)
		if err != nil {
			return result, err
		}
		checks[name] = value
		if name == "sheets" {
			checks["google_credentials"] = "已加载"
		}
	}
	return result, nil
}

func (i Integrations) sheets(ctx context.Context) (tables.Sheets, error) {
	client := httpClient(i.config.Network)
	authenticated, err := i.google.Client(ctx, client.HTTP.Timeout, tables.SheetsScope)
	if err != nil {
		return tables.Sheets{}, err
	}
	client.HTTP = authenticated
	return tables.Sheets{ID: i.config.Sheets.SpreadsheetID, AutoLayout: i.config.Sheets.AutoLayout, ActivityColors: i.config.Sheets.ActivityColors, HTTP: client, DB: i.db, Privacy: i.config.Redactor()}, nil
}
func (i Integrations) excel() tables.Excel {
	return tables.Excel{Path: i.config.Excel.Path, DB: i.db, Privacy: i.config.Redactor()}
}
func (i Integrations) writeTable(ctx context.Context, name string) (model.Object, error) {
	if name == "excel" {
		return i.excel().Write(ctx)
	}
	s, err := i.sheets(ctx)
	if err != nil {
		return nil, err
	}
	return s.Write(ctx)
}
func (i Integrations) inspectTable(ctx context.Context, name string) (model.Object, error) {
	if name == "excel" {
		return i.excel().Inspect(ctx)
	}
	s, err := i.sheets(ctx)
	if err != nil {
		return nil, err
	}
	return s.Inspect(ctx)
}
