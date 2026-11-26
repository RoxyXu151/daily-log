package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"daily-log/internal/model"
)

// Journal proposals contain the canonical journal fields. Spreadsheet sync
// suggestions share the target format and carry spreadsheet columns instead.
const journalProposals = ` FROM suggestions s
	JOIN journals j ON s.target='每日活动:'||j.day
	LEFT JOIN journal_reviews r ON r.suggestion_id=s.id
	WHERE r.suggestion_id IS NULL AND CASE WHEN json_valid(s.payload) THEN
		json_extract(s.payload,'$.date')=j.day
		AND json_type(s.payload,'$.input_hash')='text'
		AND json_type(s.payload,'$.evidence')='array'
	ELSE 0 END`

func (db *DB) PendingJournalSuggestions(ctx context.Context, day string) ([]model.Suggestion, error) {
	rows, err := db.conn.QueryContext(ctx, "SELECT s.id,coalesce(s.reason,''),coalesce(s.created_at,''),s.payload"+journalProposals+
		" AND j.day=? ORDER BY s.id DESC", day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.Suggestion
	for rows.Next() {
		var item model.Suggestion
		var payload string
		if err := rows.Scan(&item.ID, &item.Reason, &item.CreatedAt, &payload); err != nil {
			return nil, err
		}
		if err := model.Decode([]byte(payload), &item.Payload); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (db *DB) PendingJournalReviewCount(ctx context.Context) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, "SELECT count(*)"+journalProposals).Scan(&count)
	return count, err
}

func (db *DB) PendingReviewDays(ctx context.Context, offset, limit int) ([]model.ReviewDay, error) {
	rows, err := db.conn.QueryContext(ctx, "SELECT j.day,count(*),coalesce(max(s.created_at),'')"+journalProposals+
		" GROUP BY j.day ORDER BY max(s.id) DESC LIMIT ? OFFSET ?", limit, max(0, offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var days []model.ReviewDay
	for rows.Next() {
		var day model.ReviewDay
		if err := rows.Scan(&day.Day, &day.Count, &day.Latest); err != nil {
			return nil, err
		}
		days = append(days, day)
	}
	return days, rows.Err()
}

func (tx *Tx) ResolveJournalSuggestion(ctx context.Context, id int64, day, decision string, at time.Time) (model.Object, error) {
	var payload string
	if err := tx.conn.QueryRowContext(ctx, "SELECT s.payload"+journalProposals+" AND s.id=? AND j.day=?", id, day).Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = model.ErrReviewUnavailable
		}
		return nil, err
	}
	result, err := tx.conn.ExecContext(ctx, `INSERT OR IGNORE INTO journal_reviews VALUES (?,?,?)`, id, decision, model.ISO(at))
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, model.ErrReviewUnavailable
	}
	var proposal model.Object
	err = model.Decode([]byte(payload), &proposal)
	return proposal, err
}
