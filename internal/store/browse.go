package store

import (
	"context"

	"daily-log/internal/model"
)

func (db *DB) Activity(ctx context.Context, id string) (model.Event, error) {
	var event model.Event
	var payload string
	if err := db.conn.QueryRowContext(ctx, "SELECT payload FROM events WHERE event_id=?", id).Scan(&payload); err != nil {
		return event, err
	}
	err := model.Decode([]byte(payload), &event)
	return event, err
}

func (db *DB) RecentEvents(ctx context.Context, limit int) ([]model.Event, error) {
	return payloads[model.Event](ctx, db.conn, `SELECT payload FROM events
		WHERE coalesce(json_extract(payload, '$.metadata.cancelled'), 0)=0
		ORDER BY timestamp DESC,event_id DESC LIMIT ?`, limit)
}

func (db *DB) BrowseEvents(ctx context.Context, filter model.BrowseQuery, limit int) ([]model.Event, error) {
	order := " ORDER BY timestamp DESC,event_id DESC"
	if filter.Ascending {
		order = " ORDER BY timestamp,event_id"
	}
	return payloads[model.Event](ctx, db.conn, `SELECT payload FROM events
		WHERE (?='' OR day=?) AND (?='' OR source=?)
		AND (?='' OR json_extract(payload,'$.category')=?)
		AND (?='' OR instr(lower(coalesce(json_extract(payload,'$.title'),'') || ' ' ||
		coalesce(json_extract(payload,'$.description'),'')),lower(?))>0)`+order+" LIMIT ? OFFSET ?",
		filter.Day, filter.Day, filter.Source, filter.Source, filter.Category, filter.Category,
		filter.Keyword, filter.Keyword, limit, max(0, filter.Offset))
}

func (db *DB) BrowseSessions(ctx context.Context, filter model.BrowseQuery, limit int) ([]model.Session, error) {
	order := " ORDER BY day DESC,session_id DESC"
	if filter.Ascending {
		order = " ORDER BY day,session_id"
	}
	return payloads[model.Session](ctx, db.conn, `SELECT payload FROM reading_sessions
		WHERE (?='' OR day=?) AND (?='' OR instr(lower(coalesce(json_extract(payload,'$.paper_title'),'')),lower(?))>0)`+
		order+" LIMIT ? OFFSET ?", filter.Day, filter.Day, filter.Keyword, filter.Keyword, limit, max(0, filter.Offset))
}
