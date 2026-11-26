package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/store"
)

func TestEditProtectsDraftAndRejectsStaleSave(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "journal.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := Service{DB: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	at := time.Date(2026, 11, 10, 23, 0, 0, 0, model.Shanghai)
	day := at.Format(model.DateLayout)
	empty, err := s.ReadJournal(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.EditJournal(ctx, at, day, empty.Revision, model.JournalText{Morning: "我手写的正文"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditJournal(ctx, at, day, empty.Revision, model.JournalText{}, true); !errors.Is(err, model.ErrJournalConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	event := model.NewEvent("todo-1", "todoist", "task-1", "task_completed", "新增证据", at.Add(-time.Hour))
	if err := db.Transaction(ctx, func(tx *store.Tx) error {
		_, err := tx.UpsertEvent(ctx, event, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := s.Generate(ctx, at, day, false)
	if err != nil || result[day] != "manual_preserved" {
		t.Fatalf("generation = %v, %v", result, err)
	}
	view, err := s.ReadJournal(ctx, day)
	if err != nil || view.Record.Payload.Text("上午") != "我手写的正文" || view.Revision != saved.Revision {
		t.Fatalf("draft changed: %+v, %v", view, err)
	}
	edited, err := s.EditJournal(ctx, at.Add(time.Minute), day, view.Revision, model.JournalTextFrom(view.Record.Payload), false)
	if err != nil || edited.Record.Status != "已生成" || edited.Revision == view.Revision {
		t.Fatalf("edited = %+v, %v", edited, err)
	}
	suggestions, err := db.PendingJournalSuggestions(ctx, day)
	if err != nil || len(suggestions) != 0 {
		t.Fatalf("confirmation suggestions were still created: %v, %v", suggestions, err)
	}
	history, err := db.JournalHistory(ctx, day, 10)
	if err != nil || len(history) != 1 || history[0].Action != "edit" || history[0].Text.Morning != "我手写的正文" {
		t.Fatalf("revision history = %+v, %v", history, err)
	}
}
