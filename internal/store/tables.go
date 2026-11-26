package store

import (
	"context"
	"daily-log/internal/model"
	"database/sql"
	"errors"
)

func (db *DB) AllEvents(ctx context.Context) ([]model.Event, error) {
	return payloads[model.Event](ctx, db.conn, "SELECT payload FROM events ORDER BY timestamp,event_id")
}
func (db *DB) AllSessions(ctx context.Context) ([]model.Session, error) {
	return payloads[model.Session](ctx, db.conn, "SELECT payload FROM reading_sessions ORDER BY day,session_id")
}
func (db *DB) SheetSnapshot(ctx context.Context, target string) ([]any, error) {
	var data string
	err := db.conn.QueryRowContext(ctx, "SELECT payload FROM sheet_snapshots WHERE target=?", target).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var row []any
	err = model.Decode([]byte(data), &row)
	return row, err
}
func (tx *Tx) SaveSheetSnapshot(ctx context.Context, target string, row []any) error {
	data, err := model.CanonicalJSON(row)
	if err != nil {
		return err
	}
	_, err = tx.conn.ExecContext(ctx, "INSERT OR REPLACE INTO sheet_snapshots(target,payload) VALUES (?,?)", target, data)
	return err
}
