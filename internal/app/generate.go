package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"daily-log/internal/journal"
	"daily-log/internal/model"
	"daily-log/internal/store"
)

func (s Service) Generate(ctx context.Context, at time.Time, day string, all bool) (map[string]string, error) {
	if err := s.stage(ctx, Generating); err != nil {
		return nil, err
	}
	days, err := s.days(ctx, at, day, all)
	if err != nil {
		return nil, err
	}
	enabled := map[string]bool{}
	for _, input := range s.Inputs {
		enabled[input.Name] = true
	}
	result := make(map[string]string, len(days))
	for _, day := range days {
		status, err := s.generateDay(ctx, at, day, enabled)
		if err != nil {
			return result, err
		}
		result[day] = status
	}
	return result, s.DB.Transaction(ctx, func(tx *store.Tx) error {
		return tx.CompleteJob(ctx, "generate", at, result)
	})
}

// GenerateRange regenerates a fixed window of recent days, dirty or not:
// 整理最近一周 means "redo the week", not "redo only what changed".
func (s Service) GenerateRange(ctx context.Context, at time.Time, days int) (map[string]string, error) {
	if err := s.stage(ctx, Generating); err != nil {
		return nil, err
	}
	if days < 1 {
		days = 1
	}
	enabled := map[string]bool{}
	for _, input := range s.Inputs {
		enabled[input.Name] = true
	}
	result := make(map[string]string, days)
	today := model.Day(at)
	for day := today.AddDate(0, 0, -(days - 1)); !day.After(today); day = day.AddDate(0, 0, 1) {
		name := day.Format(model.DateLayout)
		status, err := s.generateDay(ctx, at, name, enabled)
		if err != nil {
			return result, err
		}
		result[name] = status
	}
	return result, s.DB.Transaction(ctx, func(tx *store.Tx) error {
		return tx.CompleteJob(ctx, "generate", at, result)
	})
}

func (s Service) days(ctx context.Context, at time.Time, specific string, all bool) ([]string, error) {
	if specific != "" {
		return []string{specific}, nil
	}
	dirty, err := s.DB.Days(ctx, all)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, day := range dirty {
		set[day] = true
	}
	today := model.Day(at)
	start := today.AddDate(0, 0, -s.Lookback)
	last, err := s.DB.Job(ctx, "generate")
	if err != nil {
		return nil, err
	}
	if !last.IsZero() && model.Day(last).Before(start) {
		start = model.Day(last)
	}
	for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
		set[day.Format(model.DateLayout)] = true
	}
	return slices.Sorted(maps.Keys(set)), nil
}

func (s Service) generateDay(ctx context.Context, at time.Time, day string, enabled map[string]bool) (string, error) {
	if _, err := model.ParseDay(day); err != nil {
		return "", err
	}
	events, err := s.DB.Events(ctx, day)
	if err != nil {
		return "", err
	}
	sessions, err := s.DB.Sessions(ctx, day)
	if err != nil {
		return "", err
	}
	coverage, err := s.DB.Coverage(ctx, day, enabled)
	if err != nil {
		return "", err
	}
	if len(s.ExcludedEventTypes) != 0 {
		events = slices.DeleteFunc(events, func(event model.Event) bool {
			return slices.Contains(s.ExcludedEventTypes, event.Kind)
		})
		if slices.Contains(s.ExcludedEventTypes, "annotation_created") {
			sessions = nil
		}
	}
	draft, err := journal.Prepare(day, events, sessions, s.AIConfig, coverage)
	if err != nil {
		return "", err
	}
	evidence := draft.EvidenceEvents()
	if len(evidence) == 0 {
		// Nothing to organize that day: the summary row goes and the day
		// stops being dirty. No text is invented for an empty day.
		return "unchanged", s.DB.Transaction(ctx, func(tx *store.Tx) error {
			if err := tx.DeleteDaySummary(ctx, day); err != nil {
				return err
			}
			return tx.ClearDirty(ctx, day)
		})
	}
	// A day the user wrote or confirmed keeps its text: keeping it needs no
	// model, so this is decided before the model is required.
	old, err := s.DB.Journal(ctx, day)
	if err != nil {
		return "", err
	}
	if journal.PreservesUserText(old) {
		return "manual_preserved", s.DB.Transaction(ctx, func(tx *store.Tx) error {
			return tx.ClearDirty(ctx, day)
		})
	}
	// Organizing a day means asking the model what happened in it: the
	// rule-assembled stand-in is gone, so without a model there is nothing
	// to write and the run says so plainly.
	if s.Digest == nil {
		return "", errors.New("未配置模型：请先在「模型设置」添加并启用一个模型")
	}
	cached, err := s.DB.DayDigest(ctx, day)
	if err != nil {
		return "", err
	}
	digest := model.DailyDigest{}
	generated := false
	if cached.Generated && cached.SourceHash == draft.Hash {
		digest.WorkItems = cached.WorkItems
	} else {
		digest, err = s.Digest(ctx, day, evidence, s.Categories)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			// The day keeps whatever text it had and stays dirty, so the
			// next run retries it; the failure is reported rather than
			// papered over with placeholder prose.
			if markErr := s.DB.Transaction(ctx, func(tx *store.Tx) error {
				return tx.MarkDirty(ctx, day)
			}); markErr != nil {
				return "", markErr
			}
			return "", fmt.Errorf("AI 整理失败，已留待重试：%w", s.Privacy.Err(err))
		}
		generated = true
	}
	status := "unchanged"
	err = s.DB.Transaction(ctx, func(tx *store.Tx) error {
		if generated {
			if err := tx.SaveDayDigest(ctx, day, digest.Summary, draft.Hash,
				digest.CategorySummaries, digest.WorkItems, at); err != nil {
				return err
			}
		}
		if old.InputHash != draft.Hash || generated {
			payload := draft.Render(digest.WorkItems)
			payload["updated_at"] = model.ISO(at)
			status = "generated"
			if err := tx.SaveJournal(ctx, model.Record{Day: day, Payload: payload, InputHash: draft.Hash, Status: "已生成"}); err != nil {
				return err
			}
		}
		return tx.ClearDirty(ctx, day)
	})
	return status, err
}
