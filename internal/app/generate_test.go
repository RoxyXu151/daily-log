package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daily-log/internal/export"
	"daily-log/internal/journal"
	"daily-log/internal/model"
	"daily-log/internal/store"
)

func TestGenerateRetriesAfterAIError(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "journal.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 11, 24, 10, 0, 0, 0, model.Shanghai)
	const day = "2026-11-24"
	event := model.NewEvent("task", "todoist", "task", "task_completed", "原始待办标题", at)
	if err := db.Transaction(ctx, func(tx *store.Tx) error {
		if _, err := tx.UpsertEvent(ctx, event, at); err != nil {
			return err
		}
		return tx.SaveJournal(ctx, model.Record{Day: day, InputHash: "legacy", Status: "已生成",
			Payload: model.Object{"input_hash": "legacy", "上午": event.Title}})
	}); err != nil {
		t.Fatal(err)
	}
	s := Service{DB: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	before, err := db.Journal(ctx, day)
	if err != nil || before.Payload.Text("上午") != journal.NoItemsText {
		t.Fatalf("legacy body = %+v, %v", before, err)
	}
	// Without a model there is nothing to organize with: the run says so.
	if _, err := s.Generate(ctx, at, day, false); err == nil || !strings.Contains(err.Error(), "未配置模型") {
		t.Fatalf("missing model should stop generation: %v", err)
	}
	s.Digest = func(context.Context, string, []model.Event, []string) (model.DailyDigest, error) {
		return model.DailyDigest{}, errors.New("model temporarily unavailable")
	}
	if _, err := s.Generate(ctx, at, day, false); err == nil || !strings.Contains(err.Error(), "AI 整理失败") {
		t.Fatalf("a model failure must be reported, not papered over: %v", err)
	}
	pending, err := db.BrowseJournals(ctx, model.BrowseQuery{Day: day}, 1)
	if err != nil || len(pending) != 1 || len(pending[0].Topics) != 0 || pending[0].EventCount != 1 {
		t.Fatalf("pending journal = %+v, %v", pending, err)
	}
	dirty, err := db.Days(ctx, false)
	if err != nil || len(dirty) != 1 || dirty[0] != day {
		t.Fatalf("days to retry = %v, %v", dirty, err)
	}
	s.Digest = func(_ context.Context, _ string, events []model.Event, _ []string) (model.DailyDigest, error) {
		if len(events) != 1 || events[0].ID != event.ID {
			t.Fatalf("retry received the wrong evidence: %+v", events)
		}
		return model.DailyDigest{Summary: "整理了报销材料。", WorkItems: []model.JournalTopic{{
			Title: "整理报销材料", Summary: "核对发票和付款凭证。", Period: "上午", Evidence: []string{event.ID},
		}}}, nil
	}
	retry, err := s.Generate(ctx, at.Add(time.Minute), day, false)
	if err != nil || retry[day] != "generated" {
		t.Fatalf("retried generation = %v, %v", retry, err)
	}
	after, err := db.BrowseJournals(ctx, model.BrowseQuery{Day: day}, 1)
	if err != nil || len(after) != 1 || len(after[0].Topics) != 1 ||
		after[0].Morning != "整理报销材料 · 核对发票和付款凭证。" {
		t.Fatalf("retried journal = %+v, %v", after, err)
	}
	directory := t.TempDir()
	if _, err := (export.Markdown{Directory: directory, Records: db}).Write(ctx); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(directory, day+".md"))
	if err != nil || !strings.Contains(string(body), after[0].Morning) || strings.Contains(string(body), event.Title) {
		t.Fatalf("exported journal = %q, %v", body, err)
	}
}
