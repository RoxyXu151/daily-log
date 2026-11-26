package store

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"daily-log/internal/model"
)

func TestBrowseJournalsBuildsSheetSummary(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "daily-log.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	day := "2026-11-11"
	at, err := model.ParseDay(day)
	if err != nil {
		t.Fatal(err)
	}
	event := model.NewEvent("task-1", "todoist", "task-1", "task_completed", "完成实验报告", at.Add(9*time.Hour))
	event.Category, event.Evidence = model.Research, "completed"
	if err := db.Transaction(ctx, func(tx *Tx) error {
		if _, err := tx.UpsertEvent(ctx, event, at.Add(10*time.Hour)); err != nil {
			return err
		}
		return tx.SaveJournal(ctx, model.Record{
			Day: day, Status: "已确认", InputHash: "hash",
			Payload: model.Object{
				"date": day, "上午": "整理实验记录", "下午": "撰写报告", "晚上": "复核结论",
				"minutes": model.Object{model.Research: model.Float(45)},
			},
		})
	}); err != nil {
		t.Fatal(err)
	}
	page, err := db.BrowseJournals(ctx, model.BrowseQuery{Category: model.Research}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 {
		t.Fatalf("journals = %d", len(page))
	}
	got := page[0]
	if got.Weekday != "周三" || got.Morning != "整理实验记录" || got.Completed != 1 {
		t.Fatalf("summary = %+v", got)
	}
	if len(got.Categories) != 1 || got.Categories[0].Name != model.Research ||
		got.Categories[0].Count != 1 || got.Categories[0].Minutes == nil ||
		*got.Categories[0].Minutes != 45 {
		t.Fatalf("categories = %+v", got.Categories)
	}
}

func TestManualCategoryUpdatesTableAndSurvivesRecollection(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "daily-log.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	day := "2026-11-11"
	at, err := model.ParseDay(day)
	if err != nil {
		t.Fatal(err)
	}
	event := model.NewEvent("task-1", "todoist", "task-1", "task_completed", "整理课程材料", at.Add(9*time.Hour))
	event.Category = model.Research
	if err := db.Transaction(ctx, func(tx *Tx) error {
		if _, err := tx.UpsertEvent(ctx, event, at.Add(10*time.Hour)); err != nil {
			return err
		}
		if err := tx.SaveJournal(ctx, model.Record{
			Day: day, Status: "待确认", InputHash: "hash",
			Payload: model.Object{"date": day, "上午": "整理课程材料"},
		}); err != nil {
			return err
		}
		if err := tx.SetEventCategory(ctx, event.ID, model.Course, at.Add(11*time.Hour)); err != nil {
			return err
		}
		_, err := tx.UpsertEvent(ctx, event, at.Add(12*time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	page, err := db.BrowseJournals(ctx, model.BrowseQuery{Category: model.Course}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 {
		t.Fatalf("course filter returned %d journals", len(page))
	}
	counts := map[string]int{}
	for _, category := range page[0].Categories {
		counts[category.Name] = category.Count
	}
	if counts[model.Course] != 1 || counts[model.Research] != 0 {
		t.Fatalf("manual category did not update counts: %+v", counts)
	}
}

func TestReclassifyCategoriesMovesHistoryAndFutureEvents(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "daily-log.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	day := "2026-11-11"
	at, err := model.ParseDay(day)
	if err != nil {
		t.Fatal(err)
	}
	event := model.NewEvent("paper-1", "zotero", "paper-1", "annotation_created", "阅读论文", at.Add(9*time.Hour))
	event.Category = model.Research
	const renamed = "文献研究"
	if err := db.Transaction(ctx, func(tx *Tx) error {
		if _, err := tx.UpsertEvent(ctx, event, at.Add(10*time.Hour)); err != nil {
			return err
		}
		if err := tx.SaveJournal(ctx, model.Record{
			Day: day, Status: "待确认", InputHash: "hash",
			Payload: model.Object{"date": day, "上午": "阅读论文", "minutes": model.Object{model.Research: model.Float(30)}},
		}); err != nil {
			return err
		}
		if err := tx.ReclassifyCategories(ctx, map[string]string{model.Research: renamed}, at.Add(11*time.Hour)); err != nil {
			return err
		}
		_, err := tx.UpsertEvent(ctx, event, at.Add(12*time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	page, err := db.BrowseJournals(ctx, model.BrowseQuery{Category: renamed}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || len(page[0].Categories) != 1 ||
		page[0].Categories[0].Name != renamed || page[0].Categories[0].Count != 1 ||
		page[0].Categories[0].Minutes == nil || *page[0].Categories[0].Minutes != 30 {
		t.Fatalf("renamed category did not move history: %+v", page)
	}
	counts, err := db.CategoryCounts(ctx)
	if err != nil || counts[renamed] != 1 || counts[model.Research] != 0 {
		t.Fatalf("category counts = %+v, %v", counts, err)
	}
}

func TestDigestCategoryDistributionCountsWorkItemsNotRawEvents(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "daily-log.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const day = "2026-11-11"
	at, err := model.ParseDay(day)
	if err != nil {
		t.Fatal(err)
	}
	items := []model.JournalTopic{
		{ID: "item-1", Title: "梳理开发方案", Category: model.Research, Period: "上午", Evidence: []string{"event-1", "event-2"}},
		{ID: "item-2", Title: "调整界面布局", Category: model.Course, Period: "下午", Evidence: []string{"event-3"}},
	}
	if err := db.Transaction(ctx, func(tx *Tx) error {
		for i, category := range []string{model.Life, model.Life, model.Research, model.Life, model.Life} {
			event := model.NewEvent("event-"+strconv.Itoa(i+1), "codex", "thread", "conversation_activity", "讨论", at.Add(time.Duration(i)*time.Minute))
			event.Category = category
			if _, err := tx.UpsertEvent(ctx, event, at.Add(time.Hour)); err != nil {
				return err
			}
		}
		if err := tx.SaveJournal(ctx, model.Record{
			Day: day, Status: "待确认", InputHash: "hash",
			Payload: model.Object{"date": day, "上午": "摘要"},
		}); err != nil {
			return err
		}
		return tx.SaveDayDigest(ctx, day, "AI 概要", "hash", []model.CategoryDigest{
			{Category: model.Research, Summary: "完成研究方案梳理", ItemCount: 1},
			{Category: model.Course, Summary: "调整课程材料", ItemCount: 1},
		}, items, at.Add(2*time.Hour))
	}); err != nil {
		t.Fatal(err)
	}
	page, err := db.BrowseJournals(ctx, model.BrowseQuery{Day: day}, 1)
	if err != nil || len(page) != 1 {
		t.Fatalf("journal page = %+v, %v", page, err)
	}
	counts := map[string]int{}
	total := 0
	for _, category := range page[0].Categories {
		counts[category.Name] = category.Count
		total += category.Count
	}
	if counts[model.Research] != 1 || counts[model.Course] != 1 || counts[model.Life] != 0 ||
		total != page[0].TopicCount {
		t.Fatalf("digest categories = %+v, topics=%d", counts, page[0].TopicCount)
	}
	if _, err := db.conn.ExecContext(ctx, "UPDATE journal_digests SET digest_version=4 WHERE day=?", day); err != nil {
		t.Fatal(err)
	}
	digest, err := db.DayDigest(ctx, day)
	if err != nil || digest.Generated {
		t.Fatalf("legacy digest was reusable: %+v, %v", digest, err)
	}
	page, err = db.BrowseJournals(ctx, model.BrowseQuery{Day: day}, 1)
	if err != nil || len(page) != 1 || len(page[0].Topics) != 0 || page[0].EventCount != 5 {
		t.Fatalf("legacy digest leaked into the journal: %+v, %v", page, err)
	}
}
