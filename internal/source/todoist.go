package source

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/netclient"
)

type Todoist struct {
	HTTP            netclient.Client
	BaseURL         string
	Token           string
	HistoryDaysFree int
}

func (t Todoist) get(ctx context.Context, path string, params url.Values, result any) error {
	if strings.TrimSpace(t.Token) == "" {
		return errors.New("Todoist 已启用但 TODOIST_API_TOKEN 未配置")
	}
	_, err := t.HTTP.JSON(ctx, http.MethodGet, strings.TrimRight(t.BaseURL, "/")+"/"+path, params,
		http.Header{"Authorization": {"Bearer " + t.Token}}, nil, result)
	return err
}

func identifier(value any) string {
	switch id := value.(type) {
	case string:
		return id
	case json.Number:
		return id.String()
	default:
		return ""
	}
}

func (t Todoist) Check(ctx context.Context) (model.Object, error) {
	result := model.Object{"enabled": true, "token_configured": strings.TrimSpace(t.Token) != ""}
	var user model.Object
	if err := t.get(ctx, "user", nil, &user); err != nil {
		return result, err
	}
	result["account_access"] = identifier(user["id"]) != ""
	return result, nil
}

func (t Todoist) pages(ctx context.Context, path string, query url.Values) ([]model.Object, error) {
	if query == nil {
		query = make(url.Values)
	}
	seen := map[string]bool{}
	items := []model.Object{}
	for {
		var page struct {
			Results []model.Object `json:"results"`
			Next    string         `json:"next_cursor"`
		}
		if err := t.get(ctx, path, query, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Results...)
		if page.Next == "" {
			return items, nil
		}
		if seen[page.Next] {
			return nil, errors.New("Todoist 返回重复分页游标")
		}
		seen[page.Next] = true
		query.Set("cursor", page.Next)
	}
}

func (t Todoist) Collect(ctx context.Context, window Window) (Batch, error) {
	var user model.Object
	if err := t.get(ctx, "user", nil, &user); err != nil {
		return Batch{}, err
	}
	userID := identifier(user["id"])
	if userID == "" {
		return Batch{}, errors.New("Todoist 用户缺少 ID")
	}
	projectList, err := t.pages(ctx, "projects", nil)
	if err != nil {
		return Batch{}, err
	}
	projects := map[string]string{}
	for _, project := range projectList {
		projects[identifier(project["id"])] = project.Text("name")
	}
	effective := window.Since
	details := model.Object{
		"account_tier": "paid", "history_limit_days": nil, "history_gap": false,
		"warning":         "活动历史可能受保留期限限制；空结果不证明没有完成活动",
		"requested_since": model.ISO(window.Since),
	}
	if !user.Bool("is_premium") {
		cutoff := window.Until.AddDate(0, 0, -t.HistoryDaysFree)
		details["account_tier"], details["history_limit_days"], details["history_gap"] = "free", t.HistoryDaysFree, window.Since.Before(cutoff)
		if safe := cutoff.Add(time.Minute); safe.After(effective) {
			effective = safe
		}
	}
	details["effective_since"] = model.ISO(effective)
	items, err := t.pages(ctx, "activities", url.Values{
		"object_event_types": {`["item:completed"]`}, "initiator_id": {userID},
		"date_from": {model.UTC(effective)}, "date_to": {model.UTC(window.Until)},
		"annotate_parents": {"true"}, "limit": {"200"},
	})
	if err != nil {
		return Batch{}, err
	}
	tasks := map[string]model.Object{}
	batch := Batch{Details: details}
	for _, item := range items {
		if item.Text("event_type") != "completed" || item.Text("object_type") != "item" || identifier(item["initiator_id"]) != userID {
			continue
		}
		rawTime := item.Text("event_date")
		at, err := model.ParseTime(rawTime, time.UTC)
		if err != nil {
			return Batch{}, errors.New("Todoist 完成时间无效")
		}
		if at.Before(window.Since) || at.After(window.Until) {
			continue
		}
		id, completionID := identifier(item["object_id"]), identifier(item["id"])
		if id == "" || completionID == "" {
			return Batch{}, errors.New("Todoist 完成记录缺少稳定 ID")
		}
		extra := item.Object("extra_data")
		_, hasDescription := extra["description"]
		task, cached := tasks[id]
		if !hasDescription && !cached {
			err := t.get(ctx, "tasks/"+url.PathEscape(id), nil, &task)
			if err != nil && !errors.Is(err, netclient.StatusError(404)) {
				return Batch{}, err
			}
			tasks[id] = task
		}
		_, snapshotHasDescription := task["description"]
		projectID := identifier(item["parent_project_id"])
		project, ok := projects[projectID]
		if !ok {
			project = projectID
		}
		event := model.NewEvent(model.StableID("todoist", userID, id, model.ISO(at.Truncate(time.Millisecond))),
			"todoist", id, "task_completed", extra.StringOr("content", task.StringOr("content", "已完成任务")), at)
		event.Evidence, event.Confidence = "completed", 0.9
		event.Description = extra.StringOr("description", task.Text("description"))
		event.Updated, event.URL, event.Raw = event.Timestamp, "https://app.todoist.com/app/task/"+id, item
		event.Metadata = model.Object{
			"completion_event_id": completionID, "project": project, "project_id": projectID,
			"original_timestamp": rawTime, "attendance_confirmed": false,
			"description_snapshot": !hasDescription, "description_available": hasDescription || snapshotHasDescription,
		}
		batch.Events = append(batch.Events, event)
	}
	details["fetched_completion_events"] = len(batch.Events)
	return batch, nil
}
