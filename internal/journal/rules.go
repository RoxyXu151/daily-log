package journal

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"daily-log/internal/model"
)

var slots = [...]string{"上午", "下午", "晚上"}

var keywords = []struct {
	category string
	terms    []string
}{
	{model.Research, []string{"论文", "科研", "研究", "实验", "文献", "paper", "research", "代码", "开发", "项目"}},
	{model.Course, []string{"课程", "课表", "作业", "考试", "复习", "上课", "讲座", "course"}},
	{model.Career, []string{"求职", "实习", "面试", "简历", "招聘", "career", "interview"}},
}

func Category(event model.Event) string {
	if event.Source == "zotero" || event.Kind == "course_checked" {
		return event.Category
	}
	if event.Source == "codex" {
		switch event.Category {
		case model.Research, model.Course, model.Career, model.Life:
			return event.Category
		}
	}
	project := event.Metadata.Text("project")
	switch project {
	case model.Research, model.Course, model.Career:
		return project
	case "事务与生活":
		return model.Life
	}
	text := strings.ToLower(event.Title + " " + project)
	for _, rule := range keywords {
		for _, term := range rule.terms {
			if strings.Contains(text, term) {
				return rule.category
			}
		}
	}
	return model.Life
}

func period(stamp string) string {
	t, _ := model.ParseTime(stamp, time.UTC)
	switch {
	case t.Hour() < 12:
		return slots[0]
	case t.Hour() < 18:
		return slots[1]
	default:
		return slots[2]
	}
}

func topic(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), ""))
}

var (
	clock   = regexp.MustCompile(`(?:[01]?\d|2[0-3])[:：][0-5]\d(?::[0-5]\d)?`)
	lessons = regexp.MustCompile(`(?:第\s*)?(\d+(?:\s*[-–—~～至到、,，/]\s*\d+)*)\s*节`)
)

func plain(value string) string {
	var text strings.Builder
	last := 0
	for _, match := range clock.FindAllStringIndex(value, -1) {
		start, end := match[0], match[1]
		if start > 0 && value[start-1] >= '0' && value[start-1] <= '9' ||
			end < len(value) && value[end] >= '0' && value[end] <= '9' {
			continue
		}
		text.WriteString(value[last:start])
		last = end
	}
	text.WriteString(value[last:])
	return strings.Trim(strings.Join(strings.Fields(text.String()), " "), " ✓✔✅-–—：:")
}

func courseName(title string) string {
	if i := strings.IndexAny(title, "|｜"); i >= 0 {
		title = title[:i]
	}
	return plain(lessons.ReplaceAllString(title, ""))
}

func courseSlots(title string) []string {
	var selected [3]bool
	mark := func(first, last int) {
		if first < 1 || last < first || last > 13 {
			return
		}
		for n := first; n <= last; n++ {
			selected[(n-1)/5] = true
		}
	}
	for _, match := range lessons.FindAllStringSubmatch(title, -1) {
		expression := strings.Join(strings.Fields(match[1]), "")
		for _, token := range strings.FieldsFunc(expression, func(r rune) bool { return strings.ContainsRune("、,，/", r) }) {
			if i := strings.IndexAny(token, "-–—~～至到"); i >= 0 {
				bounds := strings.FieldsFunc(token, func(r rune) bool { return strings.ContainsRune("-–—~～至到", r) })
				if len(bounds) == 2 {
					first, _ := strconv.Atoi(bounds[0])
					last, _ := strconv.Atoi(bounds[1])
					mark(first, last)
				}
				continue
			}
			if n, err := strconv.Atoi(token); err == nil && n >= 1 && n <= 13 {
				mark(n, n)
				continue
			}
			for first := 1; first <= 13; first++ {
				digits := strconv.Itoa(first)
				for last := first + 1; last <= 13; last++ {
					digits += strconv.Itoa(last)
					if digits == token {
						mark(first, last)
					}
				}
			}
		}
	}
	result := []string{}
	for i, slot := range slots {
		if selected[i] {
			result = append(result, slot)
		}
	}
	return result
}

var priority = map[string]int{"manual": 1, "direct": 2, "completed": 3, "schedule": 4, "inferred": 5}

func evidencePriority(kind string) int {
	if rank := priority[kind]; rank != 0 {
		return rank
	}
	return 9
}

func selectEvents(events []model.Event) []model.Event {
	todoist := make(map[string]bool)
	byID := make(map[string]model.Event, len(events))
	for _, event := range events {
		if event.Source == "todoist" {
			todoist[event.ObjectID] = true
		}
		byID[event.ID] = event
	}
	kept := make([]model.Event, 0, len(events))
	for _, event := range byID {
		if event.Metadata.Bool("cancelled") {
			continue
		}
		if event.Source == "calendar" && todoist[event.Metadata.Text("todoist_task_id")] {
			continue
		}
		if event.Source == "chatgpt" || event.Source == "codex" {
			if !event.Metadata.Bool("important") || len([]rune(strings.TrimSpace(event.Description))) < 8 {
				continue
			}
		}
		if event.Source == "codex" && slices.ContainsFunc(event.Metadata.List("todoist_task_ids"), func(id any) bool {
			key, _ := id.(string)
			return todoist[key]
		}) {
			continue
		}
		kept = append(kept, event)
	}
	slices.SortFunc(kept, func(a, b model.Event) int {
		return cmp.Or(cmp.Compare(a.Timestamp, b.Timestamp), cmp.Compare(evidencePriority(a.Evidence), evidencePriority(b.Evidence)), cmp.Compare(a.ID, b.ID))
	})
	return kept
}

func unique(items []string) []string {
	seen := make(map[string]bool, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			result = append(result, item)
			seen[item] = true
		}
	}
	return result
}

func truncate(value string, size int) string {
	runes := []rune(value)
	if len(runes) > size {
		return string(runes[:size])
	}
	return value
}

func numericPage(value string) (int, bool) {
	if strings.IndexFunc(value, func(r rune) bool { return !unicode.IsDigit(r) }) >= 0 {
		return 0, false
	}
	number, err := strconv.Atoi(value)
	return number, err == nil
}
