package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"daily-log/internal/model"
)

func TestBackupAndRepeatLegacyUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "旧版 & test.sqlite3")
	legacy, err := sql.Open("sqlite", sqliteURI(path, "_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)"))
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	const day = "2025-03-17"
	_, err = legacy.Exec(`
		CREATE TABLE events (
			event_id TEXT PRIMARY KEY, source TEXT NOT NULL, day TEXT NOT NULL,
			timestamp TEXT NOT NULL, payload TEXT NOT NULL, fingerprint TEXT NOT NULL
		);
		CREATE TABLE journals (day TEXT PRIMARY KEY, payload TEXT, input_hash TEXT, status TEXT);
		CREATE TABLE suggestions (
			id INTEGER PRIMARY KEY, target TEXT, reason TEXT, payload TEXT,
			created_at TEXT, UNIQUE(target, payload)
		);
		INSERT INTO events VALUES ('event-1','zotero','2025-03-17','2025-03-17T10:00:00+08:00',
			'{"event_id":"event-1","source":"zotero","title":"标注"}','event-hash');
		INSERT INTO journals VALUES ('2025-03-17',
			'{"上午":"已核对的正文","evidence":[{"event_id":"event-1"}]}','journal-hash','已确认');
		INSERT INTO suggestions VALUES (7,'每日活动:2025-03-17','补采',
			'{"date":"2025-03-17","input_hash":"new-hash","evidence":[{"event_id":"event-1"}]}','2025-03-18');
		INSERT INTO suggestions VALUES (8,'每日活动:2025-03-17','表格人工编辑',
			'{"上午（06-12）":"表格补充"}','2025-03-18');`)
	if err != nil {
		t.Fatal(err)
	}
	assertLegacySchema := func(conn *sql.DB) {
		t.Helper()
		var count int
		err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name='journal_versions'").Scan(&count)
		if err != nil || count != 0 {
			t.Fatalf("unexpected upgrade before opening the snapshot: count=%d, err=%v", count, err)
		}
	}
	pathCopy, err := Backup(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	assertLegacySchema(legacy)
	snapshot, err := sql.Open("sqlite", sqliteURI(pathCopy, "mode=ro"))
	if err != nil {
		t.Fatal(err)
	}
	assertLegacySchema(snapshot)
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		db, err := Open(pathCopy)
		if err != nil {
			t.Fatal(err)
		}
		journals, journalErr := db.Journals(ctx)
		events, eventErr := db.Events(ctx, day)
		suggestions, suggestionErr := db.PendingJournalSuggestions(ctx, day)
		allSuggestions, allSuggestionErr := db.Suggestions(ctx)
		reviewDays, reviewErr := db.PendingReviewDays(ctx, 0, 10)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if journalErr != nil || len(journals) != 1 || eventErr != nil || len(events) != 1 ||
			suggestionErr != nil || len(suggestions) != 1 || suggestions[0].ID != 7 ||
			allSuggestionErr != nil || len(allSuggestions) != 2 ||
			reviewErr != nil || len(reviewDays) != 1 || reviewDays[0].Count != 1 {
			t.Fatalf("upgrade %d: journals=%v/%v, events=%v/%v, suggestions=%v/%v",
				i+1, journals, journalErr, events, eventErr, suggestions, suggestionErr)
		}
		record := journals[0]
		evidence := record.Payload.List("evidence")
		if record.Status != "已确认" || record.InputHash != "journal-hash" ||
			model.JournalTextFrom(record.Payload).Morning != "已核对的正文" ||
			len(evidence) != 1 || model.Object(evidence[0].(map[string]any)).Text("event_id") != events[0].ID {
			t.Fatalf("upgrade %d changed the confirmed journal or its evidence: %+v", i+1, record)
		}
	}
	assertLegacySchema(legacy)
}
