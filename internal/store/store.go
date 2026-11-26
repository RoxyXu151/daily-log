package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"os"
	"strings"
	"time"
	"unicode"

	"daily-log/internal/model"
	"daily-log/internal/privatefs"

	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type DB struct {
	conn *sql.DB
}

type Tx struct {
	conn *sql.Tx
}

func Lock(ctx context.Context, dbPath string) (func() error, error) {
	path := dbPath + ".lock"
	file, err := privatefs.Open(path, os.O_RDWR|os.O_CREATE|os.O_APPEND)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && info.Size() == 0 {
		_, err = file.WriteString("0")
	}
	if err = errors.Join(err, file.Close()); err != nil {
		return nil, err
	}
	lock := flock.New(path)
	locked, err := lock.TryLockContext(ctx, 200*time.Millisecond)
	if err != nil {
		lock.Close()
		return nil, err
	}
	if !locked {
		lock.Close()
		return nil, errors.New("未能取得数据库进程锁")
	}
	return lock.Close, nil
}

func Open(path string) (*DB, error) {
	file, err := privatefs.Open(path, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err = privatefs.Restrict(path + suffix); err != nil {
			return nil, err
		}
	}
	// Protect literal paths containing URL metacharacters. PRAGMAs apply to
	// every connection, including a connection recreated after cancellation.
	dsn := sqliteURI(path, "_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)")
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	db := &DB{conn: conn}
	if err := db.Transaction(context.Background(), func(tx *Tx) error {
		if _, err := tx.conn.Exec(schema); err != nil {
			return err
		}
		if err := tx.ensureColumn("journal_digests", "digest_version", "INTEGER NOT NULL DEFAULT 1"); err != nil {
			return err
		}
		return tx.ensureColumn("journal_digests", "category_summaries", "TEXT NOT NULL DEFAULT '[]'")
	}); err != nil {
		conn.Close()
		return nil, err
	}
	return db, nil
}

func (tx *Tx) ensureColumn(table, column, definition string) error {
	rows, err := tx.conn.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, kind string
		var notnull, primary int
		var fallback any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &fallback, &primary); err != nil {
			rows.Close()
			return err
		}
		found = found || name == column
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.conn.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + definition)
	return err
}

func (db *DB) Close() error { return db.conn.Close() }

func (db *DB) Transaction(ctx context.Context, work func(*Tx) error) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = work(&Tx{conn: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func list[T any](ctx context.Context, conn queryer, query string, args ...any) ([]T, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []T{}
	for rows.Next() {
		var value T
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func payloads[T any](ctx context.Context, conn queryer, query string, args ...any) ([]T, error) {
	rows, err := list[string](ctx, conn, query, args...)
	if err != nil {
		return nil, err
	}
	result := make([]T, len(rows))
	for i, row := range rows {
		if err := model.Decode([]byte(row), &result[i]); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (db *DB) Events(ctx context.Context, day string) ([]model.Event, error) {
	return payloads[model.Event](ctx, db.conn, "SELECT payload FROM events WHERE day=? ORDER BY timestamp,event_id", day)
}

func (tx *Tx) SourceEvents(ctx context.Context, source string) ([]model.Event, error) {
	return payloads[model.Event](ctx, tx.conn, "SELECT payload FROM events WHERE source=? ORDER BY timestamp,event_id", source)
}

func (tx *Tx) UpsertEvent(ctx context.Context, event model.Event, at time.Time) (bool, error) {
	t, err := model.ParseTime(event.Timestamp, time.UTC)
	if err != nil || event.ID == "" {
		return false, errors.New("事件缺少有效时间或稳定 ID")
	}
	if err := tx.applyCategoryAlias(ctx, &event); err != nil {
		return false, err
	}
	if err := tx.applyCategoryOverride(ctx, &event); err != nil {
		return false, err
	}
	payload, err := model.CanonicalJSON(event)
	if err != nil {
		return false, err
	}
	hash := model.FingerprintJSON(payload)
	var oldHash, oldPayload, oldDay string
	err = tx.conn.QueryRowContext(ctx, "SELECT fingerprint,payload,day FROM events WHERE event_id=?", event.ID).
		Scan(&oldHash, &oldPayload, &oldDay)
	if err == nil {
		if oldHash == hash {
			return false, nil
		}
		if _, err = tx.conn.ExecContext(ctx, "INSERT INTO event_revisions(event_id,payload,replaced_at) VALUES (?,?,?)", event.ID, oldPayload, model.ISO(at)); err != nil {
			return false, err
		}
		if err := tx.MarkDirty(ctx, oldDay); err != nil {
			return false, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	day := model.Day(t).Format(model.DateLayout)
	if _, err = tx.conn.ExecContext(ctx, "INSERT OR REPLACE INTO events VALUES (?,?,?,?,?,?)", event.ID, event.Source, day, event.Timestamp, payload, hash); err != nil {
		return false, err
	}
	return true, tx.MarkDirty(ctx, day)
}

func (tx *Tx) applyCategoryAlias(ctx context.Context, event *model.Event) error {
	seen := map[string]bool{}
	for event.Category != "" && !seen[event.Category] {
		seen[event.Category] = true
		var target string
		err := tx.conn.QueryRowContext(ctx, "SELECT target FROM category_aliases WHERE source=?", event.Category).Scan(&target)
		if errors.Is(err, sql.ErrNoRows) || target == "" || target == event.Category {
			return nil
		}
		if err != nil {
			return err
		}
		event.Category = target
	}
	return nil
}

func (tx *Tx) applyCategoryOverride(ctx context.Context, event *model.Event) error {
	var category string
	err := tx.conn.QueryRowContext(ctx, "SELECT category FROM event_category_overrides WHERE event_id=?", event.ID).Scan(&category)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !validCategory(category) {
		return errors.New("保存的活动分类无效")
	}
	if event.Metadata == nil {
		event.Metadata = model.Object{}
	}
	event.Category = category
	event.Metadata["category_manual"] = true
	return nil
}

func (tx *Tx) SetEventCategory(ctx context.Context, id, category string, at time.Time) error {
	if !validCategory(category) {
		return errors.New("请选择有效的活动分类")
	}
	var day, encoded string
	if err := tx.conn.QueryRowContext(ctx, "SELECT day,payload FROM events WHERE event_id=?", id).
		Scan(&day, &encoded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("找不到活动记录")
		}
		return err
	}
	var event model.Event
	if err := model.Decode([]byte(encoded), &event); err != nil {
		return err
	}
	if event.Category == category && event.Metadata.Bool("category_manual") {
		return nil
	}
	if _, err := tx.conn.ExecContext(ctx,
		"INSERT INTO event_revisions(event_id,payload,replaced_at) VALUES (?,?,?)",
		id, encoded, model.ISO(at)); err != nil {
		return err
	}
	if event.Metadata == nil {
		event.Metadata = model.Object{}
	}
	event.Category = category
	event.Metadata["category_manual"] = true
	payload, err := model.CanonicalJSON(event)
	if err != nil {
		return err
	}
	hash := model.FingerprintJSON(payload)
	if _, err = tx.conn.ExecContext(ctx,
		"UPDATE events SET payload=?,fingerprint=? WHERE event_id=?",
		payload, hash, id); err != nil {
		return err
	}
	if _, err = tx.conn.ExecContext(ctx,
		`INSERT INTO event_category_overrides(event_id,category,updated_at) VALUES (?,?,?)
		ON CONFLICT(event_id) DO UPDATE SET category=excluded.category,updated_at=excluded.updated_at`,
		id, category, model.ISO(at)); err != nil {
		return err
	}
	return tx.MarkDirty(ctx, day)
}

func validCategory(category string) bool {
	category = strings.TrimSpace(category)
	return category != "" && len([]rune(category)) <= 32 &&
		strings.IndexFunc(category, unicode.IsControl) < 0
}

func (tx *Tx) MarkDirty(ctx context.Context, day string) error {
	_, err := tx.conn.ExecContext(ctx, "INSERT OR IGNORE INTO dirty_days VALUES (?)", day)
	return err
}

func (db *DB) Days(ctx context.Context, history bool) ([]string, error) {
	query := "SELECT day FROM dirty_days"
	if history {
		query += " UNION SELECT day FROM events UNION SELECT day FROM journals"
	}
	return list[string](ctx, db.conn, query)
}

func (db *DB) Sessions(ctx context.Context, day string) ([]model.Session, error) {
	return payloads[model.Session](ctx, db.conn, "SELECT payload FROM reading_sessions WHERE day=? ORDER BY rowid", day)
}

func (tx *Tx) ReplaceSessions(ctx context.Context, sessions []model.Session) error {
	old, err := payloads[model.Session](ctx, tx.conn, "SELECT payload FROM reading_sessions")
	if err != nil {
		return err
	}
	incoming := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		incoming[session.ID] = true
		if err := tx.saveSession(ctx, session); err != nil {
			return err
		}
	}
	for _, session := range old {
		if session.Active && !incoming[session.ID] {
			session.Active = false
			if err := tx.saveSession(ctx, session); err != nil {
				return err
			}
		}
	}
	return nil
}

func (tx *Tx) saveSession(ctx context.Context, s model.Session) error {
	payload, err := model.CanonicalJSON(s)
	if err != nil {
		return err
	}
	stamp, err := model.ParseTime(s.First, time.UTC)
	if err != nil {
		return err
	}
	day := model.Day(stamp).Format(model.DateLayout)
	result, err := tx.conn.ExecContext(ctx, `INSERT INTO reading_sessions VALUES (?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET day=excluded.day,payload=excluded.payload
		WHERE reading_sessions.payload<>excluded.payload`, s.ID, day, payload)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err == nil && changed != 0 {
		err = tx.MarkDirty(ctx, day)
	}
	return err
}
