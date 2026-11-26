package app

import (
	"context"
	"errors"
	"time"

	"daily-log/internal/journal"
	"daily-log/internal/model"
	"daily-log/internal/store"
)

func viewJournal(day string, record model.Record) (model.JournalView, error) {
	if record.Payload == nil {
		return model.JournalView{Record: model.Record{Day: day}, Revision: "new"}, nil
	}
	record = journal.CurrentRecord(record)
	revision, err := model.Fingerprint(record)
	return model.JournalView{Record: record, Revision: revision}, err
}

func (s Service) ReadJournal(ctx context.Context, day string) (model.JournalView, error) {
	if _, err := model.ParseDay(day); err != nil {
		return model.JournalView{}, err
	}
	record, err := s.DB.Journal(ctx, day)
	if err != nil {
		return model.JournalView{}, err
	}
	return viewJournal(day, record)
}

func (s Service) EditJournal(ctx context.Context, at time.Time, day, expected string, text model.JournalText, confirm bool) (model.JournalView, error) {
	if expected == "" {
		return model.JournalView{}, errors.New("缺少当前日记版本")
	}
	return s.mutateJournal(ctx, at, day, expected, &text, "", confirm, 0)
}

func (s Service) Confirm(ctx context.Context, at time.Time, day, note string) error {
	_, err := s.mutateJournal(ctx, at, day, "", nil, note, true, 0)
	return err
}

func (s Service) mutateJournal(ctx context.Context, at time.Time, day, expected string, text *model.JournalText, note string, _ bool, suggestion int64) (model.JournalView, error) {
	if _, err := model.ParseDay(day); err != nil {
		return model.JournalView{}, err
	}
	var view model.JournalView
	err := s.DB.Transaction(ctx, func(tx *store.Tx) error {
		record, err := tx.Journal(ctx, day)
		if err != nil {
			return err
		}
		current, err := viewJournal(day, record)
		if err != nil {
			return err
		}
		if expected != "" && expected != current.Revision {
			return model.ErrJournalConflict
		}
		action, status := "edit", "已生成"
		if suggestion != 0 {
			action = "accept_suggestion"
		}
		if record.Payload == nil {
			if text == nil || suggestion != 0 {
				return errors.New("该日期没有可确认的日记")
			}
			draft, err := journal.Prepare(day, nil, nil, nil, nil)
			if err != nil {
				return err
			}
			payload := draft.Render(nil)
			delete(payload, "input_hash")
			record = model.Record{Day: day, Payload: payload}
		} else if err := tx.ArchiveJournal(ctx, record, action, at); err != nil {
			return err
		}
		if text != nil {
			text.Apply(record.Payload)
			record.Payload["manually_edited"] = true
		} else {
			record = journal.CurrentRecord(record)
			record.Payload["manual"] = note
		}
		if suggestion != 0 {
			proposal, err := tx.ResolveJournalSuggestion(ctx, suggestion, day, "accepted", at)
			if err != nil {
				return err
			}
			mergeEvidence(record.Payload, proposal)
		}
		record.Status = status
		record.Payload["status"], record.Payload["updated_at"] = status, model.ISO(at)
		if err := tx.SaveJournal(ctx, record); err != nil {
			return err
		}
		view, err = viewJournal(day, record)
		return err
	})
	return view, err
}

func (s Service) AcceptSuggestion(ctx context.Context, at time.Time, day, expected string, id int64, text model.JournalText) (model.JournalView, error) {
	if id <= 0 || expected == "" {
		return model.JournalView{}, errors.New("缺少建议或日记版本")
	}
	return s.mutateJournal(ctx, at, day, expected, &text, "", true, id)
}

func (s Service) DismissSuggestion(ctx context.Context, at time.Time, day string, id int64) error {
	return s.DB.Transaction(ctx, func(tx *store.Tx) error {
		_, err := tx.ResolveJournalSuggestion(ctx, id, day, "dismissed", at)
		return err
	})
}

func mergeEvidence(payload, proposal model.Object) {
	evidence := payload.List("evidence")
	seen := map[string]bool{}
	for _, raw := range evidence {
		item, _ := raw.(map[string]any)
		seen[model.Object(item).Text("event_id")] = true
	}
	for _, raw := range proposal.List("evidence") {
		item, _ := raw.(map[string]any)
		id := model.Object(item).Text("event_id")
		if id != "" && !seen[id] {
			evidence = append(evidence, raw)
			seen[id] = true
		}
	}
	if len(evidence) > 0 {
		payload["evidence"] = evidence
	}
}
