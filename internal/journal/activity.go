package journal

import (
	"daily-log/internal/model"
	"fmt"
	"time"
)

// ActivityCounts counts evidenced work items rather than annotation volume.
func ActivityCounts(events []model.Event) map[string]int {
	sets := map[string]map[string]bool{}
	for _, e := range selectEvents(events) {
		category := e.Category
		if category == "" {
			category = model.Life
		}
		key := ""
		at, err := model.ParseTime(e.Timestamp, time.UTC)
		if err != nil {
			continue
		}
		day := model.Day(at).Format(model.DateLayout)
		switch {
		case e.Source == "codex":
			if e.Evidence == "direct" && e.Metadata.Bool("conversation_only") && e.Metadata.Bool("important") {
				key = "conversation:" + e.ObjectID + ":" + day + ":" + topic(firstTopic(e))
			}
		case e.Source == "zotero":
			if e.Kind == "annotation_created" {
				category = model.Research
				paper := e.Metadata.Text("paper_id")
				if paper == "" {
					paper = topic(e.Title)
				}
				key = "paper:" + day + ":" + paper
			}
		case e.Kind == "course_checked" || (e.Source == "calendar" && len(courseSlots(e.Title)) > 0):
			category = model.Course
			key = "course:" + day + ":" + topic(courseName(e.Title))
		case e.Kind == "task_completed" || e.Evidence == "manual":
			key = "task:" + e.ID
		}
		if key != "" {
			if sets[category] == nil {
				sets[category] = map[string]bool{}
			}
			sets[category][key] = true
		}
	}
	result := map[string]int{}
	for c, s := range sets {
		result[c] = len(s)
	}
	return result
}
func firstTopic(e model.Event) string {
	if t := e.Metadata.Text("topic"); t != "" {
		return t
	}
	return e.Title
}

// LineCategory supplies the same taxonomy to tabular presentation.
func LineCategory(line string, events []model.Event) string {
	for _, e := range events {
		if e.Title != "" && len(line) >= len(e.Title) && line[:len(e.Title)] == e.Title {
			return e.Category
		}
	}
	for _, c := range []struct{ prefix, category string }{{"科研 ", model.Research}, {"课程 ", model.Course}, {"职业 ", model.Career}, {"生活 ", model.Life}, {"课程安排：", model.Course}, {"阅读《", model.Research}, {"整理《", model.Research}} {
		if len(line) >= len(c.prefix) && line[:len(c.prefix)] == c.prefix {
			return c.category
		}
	}
	return Category(model.Event{Title: fmt.Sprint(line)})
}
