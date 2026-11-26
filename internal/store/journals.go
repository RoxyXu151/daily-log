package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"daily-log/internal/journal"
	"daily-log/internal/model"
)

type rowScanner interface {
	Scan(...any) error
}

func scanJournal(row rowScanner) (model.Record, error) {
	var record model.Record
	var payload string
	if err := row.Scan(&record.Day, &payload, &record.InputHash, &record.Status); err != nil {
		return record, err
	}
	return record, model.Decode([]byte(payload), &record.Payload)
}

func (db *DB) Journal(ctx context.Context, day string) (model.Record, error) {
	record, err := scanJournal(db.conn.QueryRowContext(ctx, "SELECT day,payload,input_hash,status FROM journals WHERE day=?", day))
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return journal.CurrentRecord(record), err
}

func (db *DB) Journals(ctx context.Context) ([]model.Record, error) {
	rows, err := db.conn.QueryContext(ctx, "SELECT day,payload,input_hash,status FROM journals ORDER BY day")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	journals := []model.Record{}
	for rows.Next() {
		record, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		journals = append(journals, journal.CurrentRecord(record))
	}
	return journals, rows.Err()
}

func (tx *Tx) SaveJournal(ctx context.Context, record model.Record) error {
	payload, err := model.CanonicalJSON(record.Payload)
	if err != nil {
		return err
	}
	_, err = tx.conn.ExecContext(ctx, "INSERT OR REPLACE INTO journals VALUES (?,?,?,?)", record.Day, payload, record.InputHash, record.Status)
	return err
}

func (tx *Tx) ClearDirty(ctx context.Context, day string) error {
	_, err := tx.conn.ExecContext(ctx, "DELETE FROM dirty_days WHERE day=?", day)
	return err
}

func (tx *Tx) Suggest(ctx context.Context, target, reason string, payload model.Object, at time.Time) error {
	text, err := model.CanonicalJSON(payload)
	if err != nil {
		return err
	}
	_, err = tx.conn.ExecContext(ctx, "INSERT OR IGNORE INTO suggestions(target,reason,payload,created_at) VALUES (?,?,?,?)", target, reason, text, model.ISO(at))
	return err
}

func (db *DB) Suggestions(ctx context.Context) ([]model.Object, error) {
	items, err := db.objects(ctx, "SELECT s.* FROM suggestions s LEFT JOIN journal_reviews r ON s.id=r.suggestion_id WHERE r.suggestion_id IS NULL ORDER BY s.id")
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		var payload model.Object
		if err := model.Decode([]byte(item.Text("payload")), &payload); err != nil {
			return nil, err
		}
		item["payload"] = payload
	}
	return items, nil
}

func (db *DB) Status(ctx context.Context) (model.Object, error) {
	result := model.Object{}
	var err error
	result["sources"], err = db.objects(ctx, "SELECT * FROM sync_state")
	if err != nil {
		return nil, err
	}
	result["jobs"], err = db.objects(ctx, "SELECT * FROM jobs")
	if err != nil {
		return nil, err
	}
	var events, suggestions int
	err = db.conn.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM events),
		(SELECT count(*) FROM suggestions s LEFT JOIN journal_reviews r ON r.suggestion_id=s.id WHERE r.suggestion_id IS NULL)`).
		Scan(&events, &suggestions)
	if err != nil {
		return nil, err
	}
	result["events"], result["suggestions"] = events, suggestions
	return result, nil
}

func (db *DB) DayEventCount(ctx context.Context, day string) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, "SELECT count(*) FROM events WHERE day=?", day).Scan(&count)
	return count, err
}

func (db *DB) objects(ctx context.Context, query string) ([]model.Object, error) {
	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []model.Object{}
	for rows.Next() {
		values := make([]any, len(keys))
		dest := make([]any, len(keys))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		item := make(model.Object, len(keys))
		for i, key := range keys {
			item[key] = values[i]
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (tx *Tx) Journal(ctx context.Context, day string) (model.Record, error) {
	record, err := scanJournal(tx.conn.QueryRowContext(ctx, "SELECT day,payload,input_hash,status FROM journals WHERE day=?", day))
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return record, err
}

func (tx *Tx) ArchiveJournal(ctx context.Context, record model.Record, action string, at time.Time) error {
	payload, err := model.CanonicalJSON(record.Payload)
	if err != nil {
		return err
	}
	_, err = tx.conn.ExecContext(ctx, `INSERT INTO journal_versions(day,payload,input_hash,status,action,replaced_at)
		VALUES (?,?,?,?,?,?)`, record.Day, payload, record.InputHash, record.Status, action, model.ISO(at))
	return err
}

type DaySummary struct {
	Summary, SourceHash string
}

type DayDigest struct {
	Summary, SourceHash string
	CategorySummaries   []model.CategoryDigest
	WorkItems           []model.JournalTopic
	Version             int
	Generated           bool
}

const journalDigestVersion = 5

func (db *DB) DaySummary(ctx context.Context, day string) (DaySummary, error) {
	var result DaySummary
	err := db.conn.QueryRowContext(ctx, "SELECT summary,source_hash FROM journal_summaries WHERE day=?", day).
		Scan(&result.Summary, &result.SourceHash)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return result, err
}

func (db *DB) DayDigest(ctx context.Context, day string) (DayDigest, error) {
	var result DayDigest
	var encoded, categoryEncoded string
	var generated int
	err := db.conn.QueryRowContext(ctx, `SELECT coalesce(d.summary,s.summary,''),coalesce(d.source_hash,s.source_hash,''),
		coalesce(d.digest_version,1),coalesce(d.category_summaries,'[]'),coalesce(d.work_items,'[]'),
		CASE WHEN d.day IS NULL THEN 0 ELSE 1 END
		FROM (SELECT ? AS day) q
		LEFT JOIN journal_digests d ON d.day=q.day AND d.digest_version=?
			AND json_type(d.category_summaries)='array' AND json_type(d.work_items)='array'
		LEFT JOIN journal_summaries s ON s.day=q.day`, day, journalDigestVersion).
		Scan(&result.Summary, &result.SourceHash, &result.Version, &categoryEncoded, &encoded, &generated)
	if err != nil {
		return result, err
	}
	result.Generated = generated != 0
	if encoded != "" {
		if err := model.Decode([]byte(encoded), &result.WorkItems); err != nil {
			return result, err
		}
	}
	if categoryEncoded != "" {
		if err := model.Decode([]byte(categoryEncoded), &result.CategorySummaries); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (tx *Tx) SaveDaySummary(ctx context.Context, day, summary, sourceHash string, at time.Time) error {
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	_, err := tx.conn.ExecContext(ctx, `INSERT INTO journal_summaries(day,summary,source_hash,updated_at)
		VALUES (?,?,?,?)
		ON CONFLICT(day) DO UPDATE SET summary=excluded.summary,source_hash=excluded.source_hash,updated_at=excluded.updated_at`,
		day, strings.TrimSpace(summary), sourceHash, model.ISO(at))
	return err
}

func (tx *Tx) DeleteDaySummary(ctx context.Context, day string) error {
	if _, err := tx.conn.ExecContext(ctx, "DELETE FROM journal_summaries WHERE day=?", day); err != nil {
		return err
	}
	_, err := tx.conn.ExecContext(ctx, "DELETE FROM journal_digests WHERE day=?", day)
	return err
}

func (tx *Tx) SaveDayDigest(ctx context.Context, day, summary, sourceHash string, categories []model.CategoryDigest, items []model.JournalTopic, at time.Time) error {
	if categories == nil {
		categories = []model.CategoryDigest{}
	}
	if items == nil {
		items = []model.JournalTopic{}
	}
	categoryPayload, err := model.CanonicalJSON(categories)
	if err != nil {
		return err
	}
	encoded, err := model.CanonicalJSON(items)
	if err != nil {
		return err
	}
	_, err = tx.conn.ExecContext(ctx, `INSERT INTO journal_digests(day,summary,source_hash,digest_version,category_summaries,work_items,updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(day) DO UPDATE SET summary=excluded.summary,source_hash=excluded.source_hash,
		digest_version=excluded.digest_version,category_summaries=excluded.category_summaries,
		work_items=excluded.work_items,updated_at=excluded.updated_at`,
		day, strings.TrimSpace(summary), sourceHash, journalDigestVersion, categoryPayload, encoded, model.ISO(at))
	return err
}

func (db *DB) BrowseJournals(ctx context.Context, filter model.BrowseQuery, limit int) ([]model.JournalSummary, error) {
	order := " ORDER BY j.day DESC"
	if filter.Ascending {
		order = " ORDER BY j.day"
	}
	rows, err := db.conn.QueryContext(ctx, `SELECT j.day,coalesce(j.status,''),coalesce(j.input_hash,''),j.payload,
		coalesce(d.summary,''),coalesce(d.category_summaries,'[]'),coalesce(d.work_items,'[]'),
		CASE WHEN d.day IS NULL THEN 0 ELSE 1 END
		FROM journals j
		LEFT JOIN journal_digests d ON d.day=j.day AND d.digest_version=?
			AND json_type(d.category_summaries)='array' AND json_type(d.work_items)='array'
			AND (d.source_hash=j.input_hash OR coalesce(json_extract(j.payload,'$.ai_work_items_only'),0)=0
				OR json_extract(j.payload,'$.manually_edited')=1 OR json_extract(j.payload,'$.manual')<>''
				OR j.status='已确认')
		WHERE (?='' OR j.day=?) AND (?='' OR j.status=?)
		AND (?='' OR EXISTS(
			SELECT 1 FROM events e
			WHERE e.day=j.day AND json_extract(e.payload,'$.category')=?
		))`+order+" LIMIT ? OFFSET ?",
		journalDigestVersion, filter.Day, filter.Day, filter.Status, filter.Status, filter.Category, filter.Category,
		limit, max(0, filter.Offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.JournalSummary
	byDigest := map[string]bool{}
	for rows.Next() {
		var item model.JournalSummary
		var hash, encoded, storedSummary, storedCategories, storedItems string
		var digestGenerated int
		if err := rows.Scan(&item.Day, &item.Status, &hash, &encoded, &storedSummary, &storedCategories, &storedItems, &digestGenerated); err != nil {
			return nil, err
		}
		var payload model.Object
		if err := model.Decode([]byte(encoded), &payload); err != nil {
			return nil, err
		}
		record := journal.CurrentRecord(model.Record{Day: item.Day, Status: item.Status, InputHash: hash, Payload: payload})
		summarizeJournal(&item, record.Payload, storedSummary)
		if digestGenerated != 0 && storedItems != "" {
			if err := model.Decode([]byte(storedCategories), &item.CategoryDigests); err != nil {
				return nil, err
			}
			if err := model.Decode([]byte(storedItems), &item.Topics); err != nil {
				return nil, err
			}
			byDigest[item.Day] = true
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return result, nil
	}
	days := make([]string, len(result))
	for i := range result {
		days[i] = result[i].Day
	}
	encodedDays, err := model.CanonicalJSON(days)
	if err != nil {
		return nil, err
	}
	eventRows, err := db.conn.QueryContext(ctx, `SELECT day,payload FROM events
		WHERE day IN (SELECT value FROM json_each(?))
		AND coalesce(json_extract(payload,'$.metadata.cancelled'),0)=0
		ORDER BY day,timestamp,event_id`, encodedDays)
	if err != nil {
		return nil, err
	}
	defer eventRows.Close()
	byDay := make(map[string]*model.JournalSummary, len(result))
	for i := range result {
		byDay[result[i].Day] = &result[i]
	}
	eventsByDay := make(map[string][]model.Event, len(result))
	for eventRows.Next() {
		var day string
		var encoded string
		if err := eventRows.Scan(&day, &encoded); err != nil {
			return nil, err
		}
		var event model.Event
		if err := model.Decode([]byte(encoded), &event); err != nil {
			return nil, err
		}
		summary := byDay[day]
		if summary == nil {
			continue
		}
		eventsByDay[day] = append(eventsByDay[day], event)
		if event.Category != "" && !byDigest[day] && event.Source != "codex" && event.Source != "chatgpt" {
			addJournalCategory(summary, event.Category).Count++
		}
		if event.Source == "todoist" && event.Kind == "task_completed" {
			summary.Completed++
		}
	}
	if err := eventRows.Err(); err != nil {
		return nil, err
	}
	for i := range result {
		summarizeTopics(&result[i], eventsByDay[result[i].Day], byDigest[result[i].Day])
		if byDigest[result[i].Day] {
			summarizeWorkItemCategories(&result[i])
		}
	}
	return result, nil
}

func summarizeWorkItemCategories(summary *model.JournalSummary) {
	for i := range summary.Categories {
		summary.Categories[i].Count = 0
	}
	for _, item := range summary.Topics {
		if item.Category != "" {
			addJournalCategory(summary, item.Category).Count++
		}
	}
}

func summarizeTopics(summary *model.JournalSummary, events []model.Event, digestGenerated bool) {
	if !digestGenerated {
		summary.Summary = journal.NoItemsText
	}
	summary.TopicCount = len(summary.Topics)
	summary.EventCount = len(events)
	sources := map[string]bool{}
	var start, end string
	for _, event := range events {
		sources[event.Source] = true
		if start == "" || event.Timestamp < start {
			start = event.Timestamp
		}
		if end == "" || event.Timestamp > end {
			end = event.Timestamp
		}
		if event.Source == "codex" || event.Source == "chatgpt" {
			summary.ConversationCount++
		}
	}
	summary.Sources = slices.Sorted(maps.Keys(sources))
	summary.ActiveStart, summary.ActiveEnd = start, end
}

func summarizeJournal(summary *model.JournalSummary, payload model.Object, storedSummary string) {
	if day, err := model.ParseDay(summary.Day); err == nil {
		weekdays := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
		summary.Weekday = weekdays[day.Weekday()]
	}
	text := model.JournalTextFrom(payload)
	summary.Morning, summary.Afternoon, summary.Evening = text.Morning, text.Afternoon, text.Evening
	summary.Summary = storedSummary
	minutes := payload.Object("minutes")
	for _, name := range slices.Sorted(maps.Keys(minutes)) {
		if minutesValue := knownMinutes(minutes[name]); minutesValue != nil {
			addJournalCategory(summary, name).Minutes = minutesValue
		}
	}
}

func addJournalCategory(summary *model.JournalSummary, name string) *model.JournalCategory {
	if i := slices.IndexFunc(summary.Categories, func(category model.JournalCategory) bool {
		return category.Name == name
	}); i >= 0 {
		return &summary.Categories[i]
	}
	summary.Categories = append(summary.Categories, model.JournalCategory{Name: name})
	return &summary.Categories[len(summary.Categories)-1]
}

func knownMinutes(value any) *float64 {
	var number float64
	switch value := value.(type) {
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return nil
		}
		number = parsed
	case float64:
		number = value
	case model.Float:
		number = float64(value)
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil
		}
		number = parsed
	default:
		return nil
	}
	if number < 0 {
		return nil
	}
	return &number
}

func (db *DB) JournalHistory(ctx context.Context, day string, limit int) ([]model.JournalVersion, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT id,status,action,replaced_at,payload
		FROM journal_versions WHERE day=? ORDER BY id DESC LIMIT ?`, day, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []model.JournalVersion
	for rows.Next() {
		var item model.JournalVersion
		var encoded string
		if err := rows.Scan(&item.ID, &item.Status, &item.Action, &item.ReplacedAt, &encoded); err != nil {
			return nil, err
		}
		var payload model.Object
		if err := model.Decode([]byte(encoded), &payload); err != nil {
			return nil, err
		}
		item.Text = model.JournalTextFrom(payload)
		history = append(history, item)
	}
	return history, rows.Err()
}
