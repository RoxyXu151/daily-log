package journal

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"daily-log/internal/model"
)

func Sessions(events []model.Event, gapMinutes int) ([]model.Session, error) {
	type annotation struct {
		event model.Event
		at    time.Time
	}
	type paperDay struct{ paper, day string }
	groups := map[paperDay][]annotation{}
	keys := []paperDay{}
	for _, event := range events {
		if event.Kind != "annotation_created" {
			continue
		}
		at, err := model.ParseTime(event.Timestamp, time.UTC)
		if err != nil {
			return nil, err
		}
		key := paperDay{event.Metadata.Text("paper_id"), at.Format(model.DateLayout)}
		if _, exists := groups[key]; !exists {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], annotation{event, at})
	}
	slices.SortFunc(keys, func(a, b paperDay) int { return cmp.Or(cmp.Compare(a.paper, b.paper), cmp.Compare(a.day, b.day)) })
	sessions := []model.Session{}
	for _, key := range keys {
		annotations := groups[key]
		for start := 0; start < len(annotations); {
			end := start + 1
			for end < len(annotations) && annotations[end].at.Sub(annotations[end-1].at) <= time.Duration(gapMinutes)*time.Minute {
				end++
			}
			first, last := annotations[start].event, annotations[end-1].event
			pages, descriptions := []string{}, []string{}
			for _, note := range annotations[start:end] {
				if page := note.event.Metadata.Text("page"); page != "" {
					pages = append(pages, page)
				}
				if note.event.Description != "" {
					descriptions = append(descriptions, note.event.Description)
				}
			}
			pages = unique(pages)
			slices.SortFunc(pages, func(a, b string) int {
				na, aOK := numericPage(a)
				nb, bOK := numericPage(b)
				if aOK && bOK {
					return cmp.Or(cmp.Compare(na, nb), cmp.Compare(a, b))
				}
				if aOK != bOK {
					if aOK {
						return -1
					}
					return 1
				}
				return cmp.Compare(a, b)
			})
			descriptions = unique(descriptions)
			session := model.Session{
				ID: model.StableID("reading", key.paper, first.ID), PaperID: key.paper,
				Title: first.Title, First: first.Timestamp, Last: last.Timestamp,
				Count: end - start, Pages: pages, Active: true, URL: first.URL,
				Method:  fmt.Sprintf("同论文同自然日相邻标注间隔≤%d分钟；仅标注跨度，非实际阅读时长", gapMinutes),
				Summary: truncate(strings.Join(descriptions[:min(3, len(descriptions))], "；"), 1200),
			}
			if end-start > 1 {
				span := model.Float(annotations[end-1].at.Sub(annotations[start].at).Minutes())
				session.Span = &span
			}
			sessions = append(sessions, session)
			start = end
		}
	}
	return sessions, nil
}
