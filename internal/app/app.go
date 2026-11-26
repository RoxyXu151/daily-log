package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/source"
	"daily-log/internal/store"
)

type Input struct {
	Name     string
	Required bool
	Start    time.Time
	Collect  source.Collector
}

type Output struct {
	Name  string
	Write func(context.Context) (model.Object, error)
}

type Phase string

const (
	Collecting Phase = "collecting"
	Generating Phase = "generating"
	Exporting  Phase = "exporting"
)

type Service struct {
	DB                      *store.DB
	Inputs                  []Input
	Outputs                 []Output
	Lookback                int
	DailyClock              string
	ExcludedEventTypes      []string
	SessionGap              int
	Categories              []string
	AIConfig                map[string]any
	Digest                  func(context.Context, string, []model.Event, []string) (model.DailyDigest, error)
	StopOnJournalModelError bool
	Logger                  *slog.Logger
	Progress                func(context.Context, Phase) error
	Privacy                 model.Redactor
}

type Report map[string]model.Object

func (s Service) stage(ctx context.Context, phase Phase) error {
	if s.Progress != nil {
		return s.Progress(ctx, phase)
	}
	return nil
}

func (s Service) RequiredOK(report Report) bool {
	for _, input := range s.Inputs {
		if entry, present := report[input.Name]; present && input.Required && !entry.Bool("ok") {
			return false
		}
	}
	return true
}

func (s Service) Sync(ctx context.Context, at time.Time) (model.Object, error) {
	if err := s.stage(ctx, Exporting); err != nil {
		return nil, err
	}
	result := model.Object{}
	var failure error
	for _, output := range s.Outputs {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(failure, err)
		}
		data, err := output.Write(ctx)
		err = s.Privacy.Err(err)
		status, last, message := "success", model.ISO(at), ""
		if err != nil {
			data = nil
			status, last, message = "error", "", err.Error()
			failure = errors.Join(failure, fmt.Errorf("%s 同步失败：%w", output.Name, err))
			s.Logger.Error("导出失败", "output", output.Name, "error", err)
		} else {
			data = s.Privacy.Object(data)
			result[output.Name] = data
		}
		err = s.DB.Transaction(ctx, func(tx *store.Tx) error {
			return tx.SetState(ctx, output.Name, status, last, message, data)
		})
		failure = errors.Join(failure, err)
	}
	if len(s.Outputs) == 0 {
		result["skipped"] = "未启用表格输出；数据与日记已保存在 SQLite"
	}
	return result, failure
}

func (s Service) Ready(ctx context.Context, at time.Time, includeOptional bool) (bool, error) {
	due := model.DailyRunTime(at, s.DailyClock)
	if at.Before(due) {
		due = due.AddDate(0, 0, -1)
	}
	last, err := s.DB.Job(ctx, "collect")
	if err != nil || last.Before(due) {
		return false, err
	}
	for _, input := range s.Inputs {
		if input.Required || includeOptional {
			state, err := s.DB.State(ctx, input.Name)
			if err != nil || state.Status != "success" {
				return false, err
			}
		}
	}
	return true, nil
}

func (s Service) Recover(ctx context.Context, at time.Time, report Report) (model.Object, Report, error) {
	if report == nil {
		ready, err := s.Ready(ctx, at, true)
		if err != nil {
			return nil, nil, err
		}
		if !ready {
			report, err = s.Collect(ctx, at, source.Window{})
			if err != nil {
				return nil, report, err
			}
		}
	}
	if report == nil {
		report = Report{}
	}
	last, err := s.DB.Job(ctx, "generate")
	if err != nil {
		return nil, report, err
	}
	attempt, err := s.DB.Job(ctx, "collect_attempt")
	if err != nil {
		return nil, report, err
	}
	if attempt.IsZero() {
		attempt, err = s.DB.Job(ctx, "collect")
		if err != nil {
			return nil, report, err
		}
	}
	dirty, err := s.DB.Days(ctx, false)
	if err != nil {
		return nil, report, err
	}
	generation := map[string]string{"skipped": "没有待补整理内容"}
	if len(dirty) > 0 || last.IsZero() || last.Before(attempt) {
		generation, err = s.Generate(ctx, at, "", false)
		if err != nil {
			return nil, report, err
		}
	}
	outputs, err := s.Sync(ctx, at)
	return model.Object{"collection": report, "generation": generation, "outputs": outputs}, report, err
}
