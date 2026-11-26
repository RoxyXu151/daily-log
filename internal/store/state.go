package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"daily-log/internal/model"
)

type State struct {
	Status       string
	LastSuccess  string
	ErrorMessage string
	details      string
}

func (db *DB) State(ctx context.Context, source string) (State, error) {
	var state State
	err := db.conn.QueryRowContext(ctx, `SELECT status,coalesce(last_successful_sync,''),coalesce(error_message,''),coalesce(details,'{}')
		FROM sync_state WHERE source=?`, source).Scan(&state.Status, &state.LastSuccess, &state.ErrorMessage, &state.details)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return state, err
}

func (s State) Details() (model.Object, error) {
	if s.details == "" {
		return nil, nil
	}
	var details model.Object
	err := model.Decode([]byte(s.details), &details)
	return details, err
}

func (tx *Tx) SetState(ctx context.Context, source, status, success, message string, details model.Object) error {
	if details == nil {
		details = model.Object{}
	}
	payload, err := model.CanonicalJSON(details)
	if err != nil {
		return err
	}
	_, err = tx.conn.ExecContext(ctx, `INSERT INTO sync_state(source,last_successful_sync,status,error_message,details)
		VALUES (?,nullif(?,''),?,nullif(?,''),?)
		ON CONFLICT(source) DO UPDATE SET
		last_successful_sync=coalesce(excluded.last_successful_sync,sync_state.last_successful_sync),
		status=excluded.status,error_message=excluded.error_message,details=excluded.details`,
		source, success, status, message, payload)
	return err
}

func (tx *Tx) RecordCoverage(ctx context.Context, source string, since, until, effective time.Time, ok bool) error {
	for day := model.Day(since); !day.After(model.Day(until)); day = day.AddDate(0, 0, 1) {
		key := day.Format(model.DateLayout)
		end := day.AddDate(0, 0, 1)
		var old, observed string
		err := tx.conn.QueryRowContext(ctx, "SELECT state,observed_through FROM source_coverage WHERE day=? AND source=?", key, source).
			Scan(&old, &observed)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if old == "ok" {
			at, err := model.ParseTime(observed, time.UTC)
			if err != nil {
				return err
			}
			if !at.Before(end) {
				continue
			}
		}
		state := "ok"
		if !ok {
			state = "unavailable"
		} else if effective.After(day) {
			state = "limited"
		}
		through := until
		if end.Before(through) {
			through = end
		}
		if _, err := tx.conn.ExecContext(ctx, "INSERT OR REPLACE INTO source_coverage VALUES (?,?,?,?)", key, source, state, model.ISO(through)); err != nil {
			return err
		}
		if old != state {
			if err := tx.MarkDirty(ctx, key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (db *DB) Coverage(ctx context.Context, day string, enabled map[string]bool) (map[string]string, error) {
	rows, err := db.conn.QueryContext(ctx, "SELECT source,state FROM source_coverage WHERE day=?", day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var source, state string
		if err := rows.Scan(&source, &state); err != nil {
			return nil, err
		}
		if enabled[source] {
			result[source] = state
		}
	}
	return result, rows.Err()
}

func (db *DB) Job(ctx context.Context, name string) (time.Time, error) {
	var stamp string
	err := db.conn.QueryRowContext(ctx, "SELECT last_success FROM jobs WHERE name=?", name).Scan(&stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return model.ParseTime(stamp, time.UTC)
}

func (tx *Tx) CompleteJob(ctx context.Context, name string, at time.Time, details any) error {
	payload, err := model.CanonicalJSON(details)
	if err != nil {
		return err
	}
	_, err = tx.conn.ExecContext(ctx, "INSERT OR REPLACE INTO jobs VALUES (?,?,?)", name, model.ISO(at), payload)
	return err
}

func (db *DB) ObservedCoverage(ctx context.Context, day, source string) (string, string, error) {
	var state, through string
	err := db.conn.QueryRowContext(ctx, `SELECT state,observed_through FROM source_coverage WHERE day=? AND source=?`, day, source).
		Scan(&state, &through)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return state, through, err
}
