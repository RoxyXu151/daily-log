package app

import (
	"context"
	"time"

	"daily-log/internal/journal"
	"daily-log/internal/model"
	"daily-log/internal/source"
	"daily-log/internal/store"
)

func (s Service) Collect(ctx context.Context, at time.Time, options source.Window) (Report, error) {
	if err := s.stage(ctx, Collecting); err != nil {
		return nil, err
	}
	report := Report{}
	for _, input := range s.Inputs {
		details, err := s.collectInput(ctx, input, at, options)
		if err != nil {
			return report, err
		}
		report[input.Name] = details
	}
	err := s.DB.Transaction(ctx, func(tx *store.Tx) error {
		if err := s.refreshSessions(ctx, tx); err != nil {
			return err
		}
		if err := tx.CompleteJob(ctx, "collect_attempt", at, report); err != nil {
			return err
		}
		if s.RequiredOK(report) {
			return tx.CompleteJob(ctx, "collect", at, report)
		}
		return nil
	})
	return report, err
}

func (s Service) refreshSessions(ctx context.Context, tx *store.Tx) error {
	events, err := tx.SourceEvents(ctx, "zotero")
	if err != nil {
		return err
	}
	sessions, err := journal.Sessions(events, s.SessionGap)
	if err != nil {
		return err
	}
	if err := tx.ReplaceSessions(ctx, sessions); err != nil {
		return err
	}
	return nil
}

func (s Service) collectInput(ctx context.Context, input Input, at time.Time, options source.Window) (model.Object, error) {
	state, err := s.DB.State(ctx, input.Name)
	if err != nil {
		return nil, err
	}
	since := model.Day(at).AddDate(0, 0, -s.Lookback)
	if state.LastSuccess != "" {
		last, err := model.ParseTime(state.LastSuccess, time.UTC)
		if err != nil {
			return nil, err
		}
		if lower := model.Day(last).AddDate(0, 0, -s.Lookback); lower.Before(since) {
			since = lower
		}
	}
	if !options.Since.IsZero() && options.Since.Before(since) {
		since = options.Since
	}
	if input.Start.After(since) {
		since = input.Start
	}
	requestedSince, requestedUntil := model.ISO(since), model.ISO(at)
	if err := s.DB.Transaction(ctx, func(tx *store.Tx) error {
		return tx.SetState(ctx, input.Name, "running", "", "", model.Object{
			"requested_since": requestedSince, "requested_until": requestedUntil,
		})
	}); err != nil {
		return nil, err
	}
	batch, collectErr := input.Collect(ctx, source.Window{Since: since, Until: at, History: options.History})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	details := batch.Details
	if details == nil {
		details = model.Object{}
	}
	effective := since
	if collectErr == nil && details.Text("effective_since") != "" {
		effective, collectErr = model.ParseTime(details.Text("effective_since"), time.UTC)
	}
	success, status, message := "", "error", ""
	switch {
	case collectErr != nil:
		message = s.Privacy.Text(collectErr.Error())
		details = model.Object{"ok": false, "error": message}
	case details.Bool("incomplete"):
		message = "部分来源不可用，已保留可用记录及补采水位"
		details["ok"] = false
	default:
		success, status = model.ISO(at), "success"
		details["ok"] = true
	}
	details["requested_since"], details["requested_until"] = requestedSince, requestedUntil
	err = s.DB.Transaction(ctx, func(tx *store.Tx) error {
		if collectErr == nil {
			changed, err := s.persistEvents(ctx, tx, input.Name, batch, at)
			if err != nil {
				return err
			}
			details["returned"], details["changed"] = len(batch.Events), changed
			details["effective_since"] = model.ISO(effective)
		}
		details = s.Privacy.Object(details)
		if err := tx.RecordCoverage(ctx, input.Name, since, at, effective, status == "success"); err != nil {
			return err
		}
		return tx.SetState(ctx, input.Name, status, success, message, details)
	})
	if message != "" {
		s.Logger.Warn("采集未完成", "source", input.Name, "error", message)
	} else if err == nil {
		s.Logger.Info("采集成功", "source", input.Name, "returned", details["returned"], "changed", details["changed"])
	}
	return details, err
}

func (s Service) persistEvents(ctx context.Context, tx *store.Tx, name string, batch source.Batch, at time.Time) (int, error) {
	changed := 0
	for _, event := range batch.Events {
		event.Category = journal.Category(event)
		written, err := tx.UpsertEvent(ctx, event, at)
		if err != nil {
			return 0, err
		}
		if written {
			changed++
		}
	}
	reconcile := batch.Reconciler(name)
	if reconcile == nil {
		return changed, nil
	}
	old, err := tx.SourceEvents(ctx, name)
	if err != nil {
		return 0, err
	}
	for _, event := range old {
		event, cancelled := reconcile(event)
		if !cancelled {
			continue
		}
		written, err := tx.UpsertEvent(ctx, event, at)
		if err != nil {
			return 0, err
		}
		if written {
			changed++
		}
	}
	return changed, nil
}
