package journal

import (
	"maps"
	"strings"

	"daily-log/internal/model"
)

type Draft struct {
	day      string
	events   []model.Event
	coverage map[string]string
	Hash     string
}

func Prepare(day string, all []model.Event, sessions []model.Session, ai map[string]any, coverage map[string]string) (Draft, error) {
	events := selectEvents(all)
	active := []model.Session{}
	for _, session := range sessions {
		if session.Active {
			active = append(active, session)
		}
	}
	if ai == nil {
		ai = map[string]any{}
	}
	input := model.Object{"events": events, "sessions": active, "rules_version": 8, "ai": ai}
	if len(coverage) > 0 {
		input["coverage"] = coverage
	}
	hash, err := model.Fingerprint(input)
	return Draft{day: day, events: events, coverage: coverage, Hash: hash}, err
}

// NoItemsText is what a day says when nothing has been organized into it:
// there is no rule-assembled stand-in for an AI digest any more, so an
// unorganized day is simply empty.
const NoItemsText = "暂无事项。"

// retiredPendingText is the placeholder the removed rule mode wrote into
// days that had no digest; on read it reads as empty, like today's output.
const retiredPendingText = "事项待 AI 整理。"

func PreservesUserText(record model.Record) bool {
	return record.Status == "已确认" || record.Payload.Bool("manually_edited") || record.Payload.Text("manual") != ""
}

// CurrentRecord hides body text assembled by the removed rule mode: a day
// whose stored text predates the AI digest reads as empty rather than as
// work the model never confirmed.
func CurrentRecord(record model.Record) model.Record {
	if record.InputHash == "" || record.Payload.Text("input_hash") != record.InputHash ||
		record.Payload.Bool("ai_work_items_only") || PreservesUserText(record) {
		return retirePendingText(record)
	}
	record.Payload = maps.Clone(record.Payload)
	for _, slot := range slots {
		record.Payload[slot] = NoItemsText
	}
	record.Payload["achievements"] = NoItemsText
	return retirePendingText(record)
}

// retirePendingText rewrites the old placeholder in place, leaving any real
// text untouched, so a day written before the rule mode was removed still
// reads correctly.
func retirePendingText(record model.Record) model.Record {
	if record.Payload == nil {
		return record
	}
	changed := false
	for _, key := range append(append([]string{}, slots[:]...), "achievements") {
		if record.Payload.Text(key) == retiredPendingText {
			if !changed {
				record.Payload = maps.Clone(record.Payload)
				changed = true
			}
			record.Payload[key] = NoItemsText
		}
	}
	return record
}

func (d Draft) EvidenceEvents() []model.Event {
	return append([]model.Event(nil), d.events...)
}

func (d Draft) Render(items []model.JournalTopic) model.Object {
	buckets := map[string][]string{}
	achievements := []string{}
	evidence := []model.Object{}
	var todoistCount, zoteroCount int
	for _, event := range d.events {
		evidence = append(evidence, model.Object{"event_id": event.ID, "source": event.Source, "url": event.URL, "type": event.Evidence})
		if event.Source == "todoist" && event.Kind == "task_completed" {
			todoistCount++
		}
		if event.Source == "zotero" && event.Kind == "annotation_created" {
			zoteroCount++
		}
	}
	for _, item := range items {
		buckets[item.Period] = append(buckets[item.Period], item.Title+" · "+item.Summary)
		if item.Completed > 0 {
			achievements = append(achievements, item.Title)
		}
	}
	payload := model.Object{
		"date": d.day, "status": "已生成", "manual": "", "evidence": evidence, "input_hash": d.Hash,
		"ai_work_items_only": true,
		"minutes":            model.Object{model.Research: nil, model.Course: nil, model.Career: nil, model.Life: nil},
		"todoist_count":      todoistCount, "zotero_count": zoteroCount,
	}
	empty, noAchievements := NoItemsText, "暂无可核实的成果记录。"
	for _, slot := range slots {
		payload[slot] = empty
		if lines := buckets[slot]; len(lines) > 0 {
			payload[slot] = strings.Join(lines, "\n")
		}
	}
	payload["achievements"] = noAchievements
	if len(achievements) > 0 {
		payload["achievements"] = strings.Join(unique(achievements), "\n")
	}
	if len(d.coverage) > 0 {
		payload["coverage"] = d.coverage
	}
	return payload
}
