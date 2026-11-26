package store

import (
	"context"
	"maps"
	"time"

	"daily-log/internal/model"
)

func (db *DB) CategoryCounts(ctx context.Context) (map[string]int, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT coalesce(json_extract(payload,'$.category'),''),count(*)
		FROM events GROUP BY coalesce(json_extract(payload,'$.category'),'')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		if name == "" {
			name = model.Uncategorized
		}
		result[name] += count
	}
	return result, rows.Err()
}

func (tx *Tx) ReclassifyCategories(ctx context.Context, mapping map[string]string, at time.Time) error {
	if len(mapping) == 0 {
		return nil
	}
	changedDays := map[string]bool{}
	rows, err := tx.conn.QueryContext(ctx, "SELECT day,payload FROM events ORDER BY day,event_id")
	if err != nil {
		return err
	}
	type changedEvent struct {
		id, day, oldPayload, payload, fingerprint string
	}
	var changed []changedEvent
	for rows.Next() {
		var day, encoded string
		if err := rows.Scan(&day, &encoded); err != nil {
			rows.Close()
			return err
		}
		var event model.Event
		if err := model.Decode([]byte(encoded), &event); err != nil {
			rows.Close()
			return err
		}
		category, ok := mapping[event.Category]
		if !ok || category == event.Category {
			continue
		}
		event.Category = category
		payload, err := model.CanonicalJSON(event)
		if err != nil {
			rows.Close()
			return err
		}
		changed = append(changed, changedEvent{
			id: event.ID, day: day, oldPayload: encoded, payload: payload, fingerprint: model.FingerprintJSON(payload),
		})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, event := range changed {
		if _, err := tx.conn.ExecContext(ctx,
			"INSERT INTO event_revisions(event_id,payload,replaced_at) VALUES (?,?,?)",
			event.id, event.oldPayload, model.ISO(at)); err != nil {
			return err
		}
		if _, err := tx.conn.ExecContext(ctx,
			"UPDATE events SET payload=?,fingerprint=? WHERE event_id=?",
			event.payload, event.fingerprint, event.id); err != nil {
			return err
		}
		changedDays[event.day] = true
	}
	for previous, next := range mapping {
		if _, err := tx.conn.ExecContext(ctx,
			"UPDATE event_category_overrides SET category=? WHERE category=? AND category<>?",
			next, previous, next); err != nil {
			return err
		}
		if _, err := tx.conn.ExecContext(ctx,
			"UPDATE category_aliases SET target=? WHERE target=? AND target<>?",
			next, previous, next); err != nil {
			return err
		}
		if _, err := tx.conn.ExecContext(ctx,
			`INSERT INTO category_aliases(source,target) VALUES (?,?)
			ON CONFLICT(source) DO UPDATE SET target=excluded.target`,
			previous, next); err != nil {
			return err
		}
	}
	if err := tx.reclassifyJournalMinutes(ctx, mapping, &changedDays); err != nil {
		return err
	}
	for day := range changedDays {
		if err := tx.MarkDirty(ctx, day); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) reclassifyJournalMinutes(ctx context.Context, mapping map[string]string, changedDays *map[string]bool) error {
	rows, err := tx.conn.QueryContext(ctx, "SELECT day,payload FROM journals")
	if err != nil {
		return err
	}
	type changedJournal struct {
		day, payload string
	}
	var changed []changedJournal
	for rows.Next() {
		var day, encoded string
		if err := rows.Scan(&day, &encoded); err != nil {
			rows.Close()
			return err
		}
		var payload model.Object
		if err := model.Decode([]byte(encoded), &payload); err != nil {
			rows.Close()
			return err
		}
		minutes := payload.Object("minutes")
		if len(minutes) == 0 {
			continue
		}
		renamed := make(model.Object, len(minutes))
		for name, value := range minutes {
			if next := mapping[name]; next != "" {
				name = next
			}
			renamed[name] = value
		}
		if maps.Equal(minutes, renamed) {
			continue
		}
		payload["minutes"] = renamed
		encoded, err := model.CanonicalJSON(payload)
		if err != nil {
			rows.Close()
			return err
		}
		changed = append(changed, changedJournal{day: day, payload: encoded})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, journal := range changed {
		if _, err := tx.conn.ExecContext(ctx, "UPDATE journals SET payload=? WHERE day=?", journal.payload, journal.day); err != nil {
			return err
		}
		(*changedDays)[journal.day] = true
	}
	return nil
}
