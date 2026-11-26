package runtimeapp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"daily-log/internal/app"
	"daily-log/internal/config"
	"daily-log/internal/model"
	"daily-log/internal/schedule"
	"daily-log/internal/workspace"
)

var journalEventTypes = []workspace.EventType{
	{ID: "annotation_created", Label: "Zotero 新建标注"},
	{ID: "annotation_modified", Label: "Zotero 修改标注"},
	{ID: "note_created", Label: "Zotero 新建笔记"},
	{ID: "note_modified", Label: "Zotero 修改笔记"},
	{ID: "task_completed", Label: "Todoist 完成任务"},
	{ID: "course_checked", Label: "Todoist 课程勾选"},
	{ID: "conversation_activity", Label: "Codex 对话活动"},
	{ID: "calendar_schedule", Label: "日历安排"},
}

func (w Workspace) Schedule(ctx context.Context) (workspace.ScheduleSettings, error) {
	result := workspace.ScheduleSettings{EventTypes: slices.Clone(journalEventTypes)}
	err := w.withService(ctx, false, func(s app.Service, i Integrations) error {
		cfg := i.config.Schedule()
		now := time.Now()
		result.ScheduleInput = workspace.ScheduleInput{Clock: cfg.Clock, LookbackDays: cfg.LookbackDays, Excluded: cfg.Excluded}
		result.Next = model.ISO(model.NextDailyRun(now, cfg.Clock))
		result.LastInstalled, result.AppliedClock = cfg.LastInstalled, cfg.AppliedClock
		events, err := s.DB.Events(ctx, model.Day(now).Format(model.DateLayout))
		if err != nil {
			return err
		}
		counts := map[string]int{}
		for _, event := range events {
			if !event.Metadata.Bool("cancelled") {
				counts[event.Kind]++
			}
		}
		for i := range result.EventTypes {
			result.EventTypes[i].Today = counts[result.EventTypes[i].ID]
		}
		return nil
	})
	return result, err
}

func (w Workspace) SaveSchedule(ctx context.Context, input workspace.ScheduleInput) error {
	if !model.ValidDailyClock(input.Clock) {
		return errors.New("定时时间须使用 HH:MM（北京时间）")
	}
	if input.LookbackDays < 3 || input.LookbackDays > 365 {
		return errors.New("补采范围须在 3 到 365 天之间")
	}
	for _, id := range input.Excluded {
		if !slices.ContainsFunc(journalEventTypes, func(item workspace.EventType) bool { return id == item.ID }) {
			return fmt.Errorf("未知事件类型 %q", id)
		}
	}
	input.Excluded = slices.Compact(slices.Sorted(slices.Values(input.Excluded)))
	return w.updateSettings(ctx, func(cfg config.Config, settings *config.Settings) error {
		old := cfg.Schedule()
		settings.Schedule = &config.Schedule{
			Clock: input.Clock, LookbackDays: input.LookbackDays, Excluded: input.Excluded,
			LastInstalled: old.LastInstalled, AppliedClock: old.AppliedClock,
		}
		return nil
	})
}

func (w Workspace) InstallSchedule(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return w.updateSettings(ctx, func(cfg config.Config, settings *config.Settings) error {
		prefs := cfg.Schedule()
		if err := schedule.Install(ctx, schedule.Job{Binary: w.SchedulerBinary, Config: cfg.Path, Clock: prefs.Clock}); err != nil {
			return err
		}
		prefs.LastInstalled, prefs.AppliedClock = model.ISO(time.Now()), prefs.Clock
		settings.Schedule = &prefs
		return nil
	})
}
