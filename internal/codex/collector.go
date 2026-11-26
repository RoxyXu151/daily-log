package codex

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"daily-log/internal/ai"
	"daily-log/internal/model"
	"daily-log/internal/source"
)

type Host struct {
	Name    string   `yaml:"name"`
	Command []string `yaml:"command"`
}

type CategoryRule struct {
	Category string `yaml:"category"`
	Pattern  string `yaml:"pattern"`
}

type Collector struct {
	Hosts     []Host
	Timeout   time.Duration
	Cache     string
	UseAI     bool
	AI        ai.Client
	Rules     []CategoryRule
	Overrides map[string]string
}

type message struct {
	ID        string   `json:"id"`
	Turn      string   `json:"turn_id"`
	Role      string   `json:"role"`
	Text      string   `json:"text"`
	Timestamp string   `json:"timestamp"`
	Tasks     []string `json:"todoist_task_ids"`
}

type scope struct{ thread, day, slot string }

type group struct {
	scope
	title    string
	messages []message
}

func (c Collector) Collect(ctx context.Context, window source.Window) (source.Batch, error) {
	if len(c.Hosts) == 0 {
		return source.Batch{}, errors.New("Codex 已启用但没有配置主机")
	}
	policy, err := compilePolicy(c.Rules, c.Overrides)
	if err != nil {
		return source.Batch{}, err
	}
	batch := source.Batch{}
	scopes := []any{}
	failures, reports := model.Object{}, model.Object{}
	index := map[string]int{}
	for _, host := range c.Hosts {
		groups, count, err := c.read(ctx, host, window)
		if err == nil {
			var events []model.Event
			events, err = c.extract(ctx, host.Name, groups, policy)
			if err == nil {
				for _, event := range events {
					if i, ok := index[event.ID]; ok {
						batch.Events[i] = event
					} else {
						index[event.ID] = len(batch.Events)
						batch.Events = append(batch.Events, event)
					}
				}
				for _, group := range groups {
					scopes = append(scopes, []any{host.Name, group.thread, group.day, group.slot})
				}
				reports[host.Name] = model.Object{"messages": count, "activities": len(events)}
			}
		}
		if err != nil {
			failures[host.Name] = err.Error()
		}
	}
	batch.Details = model.Object{
		"hosts": reports, "errors": failures, "incomplete": len(failures) > 0,
		"completed_scopes": scopes,
		"method":           "Codex 只读会话；按回复时间归档完整轮次；只记录讨论事实",
	}
	return batch, nil
}

func (c Collector) read(ctx context.Context, host Host, window source.Window) ([]group, int, error) {
	client, err := openHistory(ctx, host.Command, c.Timeout)
	if err != nil {
		return nil, 0, err
	}
	defer client.close()
	keys := []scope{}
	labels := map[scope]string{}
	groups := map[scope][][2]message{}
	seen := map[[2]string]bool{}
	for _, archived := range []bool{false, true} {
		err := client.pages("thread/list", model.Object{
			"limit": 100, "sortKey": "updated_at", "sortDirection": "desc",
			"modelProviders": []string{}, "sourceKinds": []string{"vscode", "appServer"},
			"archived": archived, "useStateDbOnly": true,
		}, func(threads []model.Object) (bool, error) {
			for _, thread := range threads {
				threadID := thread.Text("id")
				if before(thread["updatedAt"], window.Since) || threadID == "" {
					continue
				}
				err := client.pages("thread/turns/list", model.Object{
					"threadId": threadID, "limit": 20, "sortDirection": "desc", "itemsView": "summary",
				}, func(turns []model.Object) (bool, error) {
					allOlder := len(turns) > 0
					for _, turn := range turns {
						allOlder = allOlder && turn["completedAt"] != nil && before(turn["completedAt"], window.Since)
						if turn.Text("status") != "completed" {
							continue
						}
						pair, stamp, ok := turnPair(turn)
						id := [2]string{threadID, turn.Text("id")}
						if !ok || seen[id] || stamp.Before(window.Since) || stamp.After(window.Until) {
							continue
						}
						seen[id] = true
						slot := "晚上"
						if stamp.Hour() < 12 {
							slot = "上午"
						} else if stamp.Hour() < 18 {
							slot = "下午"
						}
						key := scope{threadID, stamp.Format(model.DateLayout), slot}
						if _, exists := groups[key]; !exists {
							keys = append(keys, key)
							labels[key] = firstText(thread, "title", "name", "preview")
						}
						groups[key] = append(groups[key], pair)
					}
					return allOlder, nil
				})
				if err != nil {
					return false, err
				}
			}
			return len(threads) > 0 && before(threads[len(threads)-1]["updatedAt"], window.Since), nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	result := []group{}
	for _, key := range keys {
		pairs := groups[key]
		slices.SortStableFunc(pairs, func(a, b [2]message) int { return cmp.Compare(a[1].Timestamp, b[1].Timestamp) })
		chunk, size := []message{}, 0
		for _, pair := range pairs {
			cost := utf8.RuneCountInString(pair[0].Text) + utf8.RuneCountInString(pair[1].Text)
			if len(chunk) > 0 && (size+cost > 16000 || len(chunk) >= 16) {
				result = append(result, group{scope: key, title: labels[key], messages: chunk})
				chunk, size = []message{}, 0
			}
			chunk = append(chunk, pair[:]...)
			size += cost
		}
		if len(chunk) > 0 {
			result = append(result, group{scope: key, title: labels[key], messages: chunk})
		}
	}
	return result, 2 * len(seen), nil
}

func firstText(object model.Object, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(object.Text(key)); value != "" {
			return value
		}
	}
	return ""
}

func before(value any, at time.Time) bool {
	n, _ := value.(json.Number)
	seconds, _ := n.Float64()
	return seconds < float64(at.Unix())+float64(at.Nanosecond())/1e9
}

var taskLink = regexp.MustCompile(`https://app\.todoist\.com/app/task/([A-Za-z0-9]+)`)

func turnPair(turn model.Object) ([2]message, time.Time, bool) {
	users := []message{}
	var reply message
	var replyAt time.Time
	for _, value := range turn.List("items") {
		fields, _ := value.(map[string]any)
		item := model.Object(fields)
		var text, role string
		switch item.Text("type") {
		case "userMessage":
			role = "user"
			lines := []string{}
			for _, raw := range item.List("content") {
				fields, _ := raw.(map[string]any)
				content := model.Object(fields)
				if content.Text("type") == "text" {
					lines = append(lines, content.Text("text"))
				}
			}
			text = strings.Join(lines, "\n")
		case "agentMessage":
			if phase := item.Text("phase"); phase != "" && phase != "final_answer" {
				continue
			}
			role, text = "assistant", item.Text("text")
		default:
			continue
		}
		number, _ := item["completedAtMs"].(json.Number)
		if number == "" || number == "0" {
			number, _ = item["startedAtMs"].(json.Number)
		}
		ms, err := number.Int64()
		if err != nil {
			continue
		}
		tasks := []string{}
		for _, match := range taskLink.FindAllStringSubmatch(text, -1) {
			tasks = append(tasks, match[1])
		}
		id := item.Text("id")
		if id == "" {
			id = model.StableID(turn.Text("id"), role, text)
		}
		if text = clean(text); text == "" {
			continue
		}
		stamp := time.UnixMilli(ms).In(model.Shanghai)
		msg := message{id, turn.Text("id"), role, clip(text, 4000), model.ISO(stamp), tasks}
		if role == "user" {
			users = append(users, msg)
		} else if reply.ID == "" || stamp.After(replyAt) {
			reply, replyAt = msg, stamp
		}
	}
	var user message
	var userAt time.Time
	for _, candidate := range users {
		at, _ := model.ParseTime(candidate.Timestamp, time.UTC)
		if !at.After(replyAt) && (user.ID == "" || at.After(userAt)) {
			user, userAt = candidate, at
		}
	}
	return [2]message{user, reply}, replyAt, user.ID != "" && reply.ID != ""
}
