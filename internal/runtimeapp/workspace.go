package runtimeapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

	"daily-log/internal/app"
	"daily-log/internal/config"
	"daily-log/internal/model"
	"daily-log/internal/source"
	"daily-log/internal/store"
	"daily-log/internal/workspace"
)

type Workspace struct {
	ConfigPath      string
	SchedulerBinary string
	Logger          *slog.Logger
	Connectors      []SourceRegistration
}

const workspaceJob = "job:workspace"

func (w Workspace) withService(ctx context.Context, write bool, fn func(app.Service, Integrations) error) error {
	cfg, err := w.loadConfig()
	if err != nil {
		return err
	}
	if write {
		unlock, err := store.Lock(ctx, cfg.Database)
		if err != nil {
			return err
		}
		defer unlock()
	}
	db, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	logger := w.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s, i, err := Configure(cfg, db, logger, w.Connectors...)
	if err != nil {
		return err
	}
	return s.Privacy.Err(fn(s, i))
}

func (w Workspace) Overview(ctx context.Context) (workspace.Overview, error) {
	now := time.Now()
	result := workspace.Overview{Today: model.Day(now).Format(model.DateLayout)}
	err := w.withService(ctx, false, func(s app.Service, i Integrations) error {
		today, err := s.DB.BrowseJournals(ctx, model.BrowseQuery{Day: result.Today}, 1)
		if err != nil {
			return err
		}
		if len(today) > 0 {
			result.TodaySummary = today[0]
		}
		recent, err := s.DB.RecentEvents(ctx, 4)
		if err != nil {
			return err
		}
		for _, event := range recent {
			result.Recent = append(result.Recent, workspace.ActivitySummary{
				ID: event.ID, Title: s.Privacy.Text(event.Title), Source: event.Source, Timestamp: event.Timestamp,
			})
		}
		last, err := s.DB.Job(ctx, "generate")
		if err != nil {
			return err
		}
		if !last.IsZero() {
			result.LastRun = model.ISO(last)
		}
		result.NextRun = model.ISO(model.NextDailyRun(now, s.DailyClock))
		job, err := s.DB.State(ctx, workspaceJob)
		if err != nil {
			return err
		}
		result.JobStatus, result.JobError = job.Status, s.Privacy.Text(job.ErrorMessage)
		enabled := i.Enabled()
		for _, spec := range i.config.RegisteredSources() {
			name := spec.ID
			state, err := s.DB.State(ctx, name)
			if err != nil {
				return err
			}
			coverage, through, err := s.DB.ObservedCoverage(ctx, result.Today, name)
			if err != nil {
				return err
			}
			details, err := state.Details()
			if err != nil {
				return err
			}
			result.Sources = append(result.Sources, workspace.SourceStatus{
				Name: name, Enabled: enabled[name], Status: state.Status, LastSuccess: state.LastSuccess,
				Coverage: coverage, ObservedThrough: through, ErrorMessage: s.Privacy.Text(state.ErrorMessage),
				Attempt: sourceAttempt(details),
			})
		}
		for _, name := range []string{"markdown", "sheets", "excel"} {
			state, err := s.DB.State(ctx, name)
			if err != nil {
				return err
			}
			output := workspace.OutputStatus{
				Name: name, Enabled: enabled[name], Status: state.Status,
				LastSuccess: state.LastSuccess, ErrorMessage: s.Privacy.Text(state.ErrorMessage),
			}
			if output.Enabled && state.Status == "success" {
				details, err := state.Details()
				if err != nil {
					return err
				}
				output.Summary = outputSummary(details)
			}
			result.Outputs = append(result.Outputs, output)
		}
		return nil
	})
	return result, err
}

func sourceAttempt(details model.Object) *workspace.SourceAttempt {
	if len(details) == 0 {
		return nil
	}
	_, hasCounts := details["returned"].(json.Number)
	return &workspace.SourceAttempt{
		RequestedSince: details.Text("requested_since"), RequestedUntil: details.Text("requested_until"),
		EffectiveSince: details.Text("effective_since"), Returned: resultCount(details, "returned"),
		Changed: resultCount(details, "changed"), HistoryLimitDays: resultCount(details, "history_limit_days"),
		HasCounts: hasCounts, HistoryGap: details.Bool("history_gap"),
	}
}

func resultCount(object model.Object, key string) int {
	n, _ := object[key].(json.Number)
	value, _ := n.Int64()
	return int(value)
}

func outputSummary(details model.Object) *workspace.OutputSummary {
	if len(details) == 0 {
		return nil
	}
	summary := workspace.OutputSummary{Files: resultCount(details, "written_files")}
	for _, name := range []string{"活动流水", "论文阅读", "每日活动", "统计看板"} {
		table := details.Object(name)
		summary.Written += resultCount(table, "written")
		summary.Protected += resultCount(table, "protected")
		summary.Skipped += resultCount(table, "skipped_private_keys")
	}
	return &summary
}

func (w Workspace) Activity(ctx context.Context, id string) (workspace.Evidence, error) {
	var result workspace.Evidence
	err := w.withService(ctx, false, func(s app.Service, _ Integrations) error {
		event, err := s.DB.Activity(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("找不到活动记录")
		}
		if err == nil {
			result = activityEvidence(event, s.Privacy)
		}
		return err
	})
	return result, err
}

func (w Workspace) SetEventCategory(ctx context.Context, id, category string) error {
	return w.withService(ctx, true, func(s app.Service, _ Integrations) error {
		return s.DB.Transaction(ctx, func(tx *store.Tx) error {
			return tx.SetEventCategory(ctx, id, category, time.Now())
		})
	})
}

func (w Workspace) Categories(ctx context.Context) ([]workspace.Category, error) {
	cfg, err := w.loadConfig()
	if err != nil {
		return nil, err
	}
	names := cfg.Settings.CategoryNames()
	counts := map[string]int{}
	err = w.withService(ctx, false, func(s app.Service, _ Integrations) error {
		var err error
		counts, err = s.DB.CategoryCounts(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	result := make([]workspace.Category, 0, len(names)+len(counts))
	seen := map[string]bool{}
	for _, name := range names {
		result = append(result, workspace.Category{
			Name: name, Short: cfg.Settings.CategoryShort(name), Count: counts[name],
		})
		seen[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		if !seen[name] {
			result = append(result, workspace.Category{
				Name: name, Short: cfg.Settings.CategoryShort(name), Count: counts[name],
			})
		}
	}
	return result, nil
}

func (w Workspace) SaveCategories(ctx context.Context, items []workspace.CategoryInput) error {
	cfg, err := w.loadConfig()
	if err != nil {
		return err
	}
	names, mapping, err := prepareCategoryChanges(cfg.Settings.CategoryNames(), items)
	if err != nil {
		return err
	}
	shorts, err := prepareCategoryShorts(items)
	if err != nil {
		return err
	}
	return w.withService(ctx, true, func(s app.Service, _ Integrations) error {
		if err := s.DB.Transaction(ctx, func(tx *store.Tx) error {
			return tx.ReclassifyCategories(ctx, mapping, time.Now())
		}); err != nil {
			return err
		}
		cfg.Settings.Categories = names
		cfg.Settings.CategoryShorts = shorts
		return config.WriteSettings(cfg.Path, cfg.Settings)
	})
}

// prepareCategoryShorts keeps only the 简称 that differ from the name: an
// empty map means every category shows its full name, and the four built-in
// short labels stay the shipped defaults until the user edits one.
func prepareCategoryShorts(items []workspace.CategoryInput) (map[string]string, error) {
	shorts := map[string]string{}
	for i, item := range items {
		name, short := strings.TrimSpace(item.Name), strings.TrimSpace(item.Short)
		if name == "" || short == "" || short == name {
			continue
		}
		if len([]rune(short)) > 8 || strings.IndexFunc(short, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("第 %d 个分类的简称无效或过长（最多 8 字）", i+1)
		}
		shorts[name] = short
	}
	return shorts, nil
}

func prepareCategoryChanges(current []string, items []workspace.CategoryInput) ([]string, map[string]string, error) {
	if len(items) == 0 {
		return nil, nil, errors.New("至少保留一个活动分类")
	}
	names := make([]string, 0, len(items)+1)
	seen := map[string]bool{}
	included := map[string]bool{}
	mapping := map[string]string{}
	for i, item := range items {
		previous, name := strings.TrimSpace(item.Previous), strings.TrimSpace(item.Name)
		if previous != "" {
			included[previous] = true
		}
		if name == "" {
			if previous == model.Uncategorized {
				return nil, nil, errors.New("“未分类”不能删除")
			}
			if previous != "" && previous != model.Uncategorized {
				mapping[previous] = model.Uncategorized
			}
			continue
		}
		if len([]rune(name)) > 32 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return nil, nil, fmt.Errorf("第 %d 个分类名称无效或过长", i+1)
		}
		if seen[name] {
			return nil, nil, fmt.Errorf("分类名称不能重复：%s", name)
		}
		seen[name] = true
		names = append(names, name)
		if previous != "" && previous != name {
			mapping[previous] = name
		}
	}
	for _, name := range current {
		if !included[name] && name != model.Uncategorized {
			mapping[name] = model.Uncategorized
		}
	}
	needsFallback := false
	for _, target := range mapping {
		needsFallback = needsFallback || target == model.Uncategorized
	}
	if needsFallback && !seen[model.Uncategorized] {
		names = append(names, model.Uncategorized)
	}
	if len(names) == 0 {
		return nil, nil, errors.New("至少保留一个活动分类")
	}
	return names, mapping, nil
}

func (w Workspace) Journals(ctx context.Context, filter model.BrowseQuery) (workspace.Page[model.JournalSummary], error) {
	var result workspace.Page[model.JournalSummary]
	err := w.withService(ctx, false, func(s app.Service, _ Integrations) error {
		journals, err := s.DB.BrowseJournals(ctx, filter, workspace.PageSize+1)
		if err == nil {
			result.More = len(journals) > workspace.PageSize
			result.Rows = journals[:min(len(journals), workspace.PageSize)]
			for i := range result.Rows {
				row := &result.Rows[i]
				row.Morning = s.Privacy.Text(row.Morning)
				row.Afternoon = s.Privacy.Text(row.Afternoon)
				row.Evening = s.Privacy.Text(row.Evening)
				row.Summary = s.Privacy.Text(row.Summary)
				for j := range row.Topics {
					row.Topics[j].Title = s.Privacy.Text(row.Topics[j].Title)
					row.Topics[j].Summary = s.Privacy.Text(row.Topics[j].Summary)
				}
				for j := range row.CategoryDigests {
					row.CategoryDigests[j].Summary = s.Privacy.Text(row.CategoryDigests[j].Summary)
				}
				row.Categories = mergeJournalCategories(row.Categories, s.Categories)
			}
		}
		return err
	})
	return result, err
}

func mergeJournalCategories(found []model.JournalCategory, configured []string) []model.JournalCategory {
	byName := make(map[string]model.JournalCategory, len(found)+len(configured))
	for _, category := range found {
		byName[category.Name] = category
	}
	result := make([]model.JournalCategory, 0, len(byName)+len(configured))
	for _, name := range configured {
		category, ok := byName[name]
		if !ok {
			category = model.JournalCategory{Name: name}
		}
		result = append(result, category)
		delete(byName, name)
	}
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		result = append(result, byName[name])
	}
	return result
}

func (w Workspace) Journal(ctx context.Context, day string) (workspace.Journal, error) {
	var result workspace.Journal
	err := w.withService(ctx, false, func(s app.Service, _ Integrations) error {
		view, err := s.ReadJournal(ctx, day)
		if err != nil {
			return err
		}
		result.View = view
		result.History, err = s.DB.JournalHistory(ctx, day, 30)
		if err != nil {
			return err
		}
		events, err := s.DB.Events(ctx, day)
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.Metadata.Bool("cancelled") {
				continue
			}
			result.Evidence = append(result.Evidence, activityEvidence(event, s.Privacy))
		}
		return nil
	})
	return result, err
}

func (w Workspace) Activities(ctx context.Context, filter model.BrowseQuery) (workspace.Page[workspace.Evidence], error) {
	var result workspace.Page[workspace.Evidence]
	err := w.withService(ctx, false, func(s app.Service, _ Integrations) error {
		events, err := s.DB.BrowseEvents(ctx, filter, workspace.PageSize+1)
		if err != nil {
			return err
		}
		result.More = len(events) > workspace.PageSize
		for _, event := range events[:min(len(events), workspace.PageSize)] {
			result.Rows = append(result.Rows, activityEvidence(event, s.Privacy))
		}
		return nil
	})
	return result, err
}

func activityEvidence(event model.Event, privacy model.Redactor) workspace.Evidence {
	return workspace.Evidence{
		ID: event.ID, Source: event.Source, Title: privacy.Text(event.Title), Kind: event.Evidence,
		Category: event.Category, Timestamp: event.Timestamp, Description: privacy.Text(event.Description), URL: visibleLink(event.URL, privacy),
		EventType: event.Kind, Reference: privacy.Text(event.ObjectID),
	}
}

func visibleLink(link string, privacy model.Redactor) string {
	if privacy.Text(link) != link {
		return ""
	}
	return link
}

func (w Workspace) Reading(ctx context.Context, filter model.BrowseQuery) (workspace.Page[model.Session], error) {
	var result workspace.Page[model.Session]
	err := w.withService(ctx, false, func(s app.Service, _ Integrations) error {
		sessions, err := s.DB.BrowseSessions(ctx, filter, workspace.PageSize+1)
		if err == nil {
			result.More = len(sessions) > workspace.PageSize
			result.Rows = sessions[:min(len(sessions), workspace.PageSize)]
			for i := range result.Rows {
				item := &result.Rows[i]
				item.Title, item.Summary = s.Privacy.Text(item.Title), s.Privacy.Text(item.Summary)
				item.URL = visibleLink(item.URL, s.Privacy)
			}
		}
		return err
	})
	return result, err
}

func (w Workspace) Save(ctx context.Context, day, revision string, text model.JournalText, confirm bool) (model.JournalView, error) {
	var result model.JournalView
	err := w.withService(ctx, true, func(s app.Service, _ Integrations) error {
		var err error
		result, err = s.EditJournal(ctx, time.Now(), day, revision, text, confirm)
		return err
	})
	return result, err
}

func (w Workspace) Run(ctx context.Context) error { return w.run(ctx, true) }

func (w Workspace) Recover(ctx context.Context) error { return w.run(ctx, false) }

// Regenerate collects, then rewrites the last n days whether or not they
// changed, and syncs: the 整理最近一周 button's whole job.
func (w Workspace) Regenerate(ctx context.Context, days int) error {
	return w.withJob(ctx, func(s app.Service) error {
		at := time.Now().In(model.Shanghai)
		report, err := s.Collect(ctx, at, source.Window{})
		if err != nil {
			return err
		}
		if _, err := s.GenerateRange(ctx, at, max(1, days)); err != nil {
			return err
		}
		_, err = s.Sync(ctx, at)
		if !s.RequiredOK(report) {
			err = errors.Join(err, errors.New("部分必要来源采集失败，已保留可用结果"))
		}
		return err
	})
}

func (w Workspace) run(ctx context.Context, collect bool) error {
	return w.withJob(ctx, func(s app.Service) error {
		at := time.Now().In(model.Shanghai)
		var report app.Report
		var err error
		if collect {
			report, err = s.Collect(ctx, at, source.Window{})
			if err != nil {
				return err
			}
		}
		_, report, err = s.Recover(ctx, at, report)
		if !s.RequiredOK(report) {
			err = errors.Join(err, errors.New("部分必要来源采集失败，已保留可用结果"))
		}
		return err
	})
}

func (w Workspace) Export(ctx context.Context, target string) error {
	return w.withJob(ctx, func(s app.Service) error {
		for _, output := range s.Outputs {
			if output.Name == target {
				s.Outputs = []app.Output{output}
				_, err := s.Sync(ctx, time.Now())
				return err
			}
		}
		return errors.New("此输出未启用")
	})
}

func (w Workspace) withJob(ctx context.Context, work func(app.Service) error) error {
	return w.withService(ctx, true, func(s app.Service, _ Integrations) error {
		writeState := func(ctx context.Context, status, success, message string) error {
			return s.DB.Transaction(ctx, func(tx *store.Tx) error {
				return tx.SetState(ctx, workspaceJob, status, success, message, nil)
			})
		}
		s.Progress = func(ctx context.Context, phase app.Phase) error {
			return writeState(ctx, string(phase), "", "")
		}
		err := s.Privacy.Err(work(s))
		status, success, message := "success", model.ISO(time.Now()), ""
		if err != nil {
			status, success, message = "error", "", err.Error()
		}
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		return errors.Join(err, writeState(finish, status, success, message))
	})
}
