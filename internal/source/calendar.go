package source

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/netclient"
)

type Calendar struct {
	Client func(context.Context) (netclient.Client, error)
	IDs    []string
}

func (c Calendar) Collect(ctx context.Context, window Window) (Batch, error) {
	httpClient, err := c.Client(ctx)
	if err != nil {
		return Batch{}, err
	}
	cancelled := []any{}
	batch := Batch{}
	for _, id := range c.IDs {
		endpoint := "https://www.googleapis.com/calendar/v3/calendars/" + url.PathEscape(id) + "/events"
		query := url.Values{
			"timeMin": {model.ISO(window.Since)}, "timeMax": {model.ISO(window.Until)},
			"singleEvents": {"true"}, "showDeleted": {"true"}, "maxResults": {"2500"},
		}
		seen := map[string]bool{}
		for {
			var page struct {
				Items         []model.Object
				NextPageToken string
			}
			if _, err := httpClient.JSON(ctx, http.MethodGet, endpoint, query, nil, nil, &page); err != nil {
				return Batch{}, err
			}
			for _, item := range page.Items {
				if item.Text("id") == "" {
					return Batch{}, errors.New("日历日程缺少稳定 ID")
				}
				if item.Text("status") == "cancelled" {
					cancelled = append(cancelled, map[string]any{"calendar_id": id, "id": item.Text("id")})
				} else if !declined(item) {
					events, err := calendarEvents(item, id, window)
					if err != nil {
						return Batch{}, err
					}
					batch.Events = append(batch.Events, events...)
				}
			}
			next := page.NextPageToken
			if next == "" {
				break
			}
			if seen[next] {
				return Batch{}, errors.New("Google 日历返回重复分页游标")
			}
			seen[next] = true
			query.Set("pageToken", next)
		}
	}
	batch.Details = model.Object{"cancelled": cancelled, "events": len(batch.Events)}
	return batch, nil
}

func declined(item model.Object) bool {
	for _, entry := range item.List("attendees") {
		fields, _ := entry.(map[string]any)
		attendee := model.Object(fields)
		if attendee.Bool("self") && attendee.Text("responseStatus") == "declined" {
			return true
		}
	}
	return false
}

func calendarTime(fields model.Object) (string, time.Time, error) {
	zone, err := time.LoadLocation(fields.StringOr("timeZone", "Asia/Shanghai"))
	if err != nil {
		return "", time.Time{}, errors.New("日历日程时区无效")
	}
	raw := fields.StringOr("dateTime", fields.Text("date"))
	at, err := model.ParseTime(raw, zone)
	if err != nil {
		return "", time.Time{}, errors.New("日历日程时间无效")
	}
	return raw, at, nil
}

var todoistReference = regexp.MustCompile(`(?i)(?:todoist\.com/(?:showTask\?id=|app/task/)|todoist[_-]?task[_-]?id['"\s:=]+)([\w-]+)`)

func calendarEvents(item model.Object, calendarID string, window Window) ([]model.Event, error) {
	start, end := item.Object("start"), item.Object("end")
	rawStart, first, err := calendarTime(start)
	if err != nil {
		return nil, err
	}
	rawEnd, last, err := calendarTime(end)
	if err != nil {
		return nil, err
	}
	updated, err := model.ParseTime(item.StringOr("updated", rawStart), model.Shanghai)
	if err != nil {
		return nil, errors.New("日历日程更新时间无效")
	}
	reference := item.Text("description")
	for _, key := range []string{"source", "extendedProperties"} {
		text, err := model.CanonicalJSON(item[key])
		if err != nil {
			return nil, err
		}
		reference += " " + text
	}
	var todoist any
	if match := todoistReference.FindStringSubmatch(reference); len(match) > 0 {
		todoist = match[1]
	}
	allDay := start.Text("date") != ""
	day := model.Day(first)
	if since := model.Day(window.Since); since.After(day) {
		day = since
	}
	events := []model.Event{}
	for ; day.Before(last) && day.Before(window.Until); day = day.AddDate(0, 0, 1) {
		begin, finish := day, day.AddDate(0, 0, 1)
		if first.After(begin) {
			begin = first
		}
		if last.Before(finish) {
			finish = last
		}
		if !finish.After(window.Since) || !begin.Before(window.Until) {
			continue
		}
		id := item.Text("id")
		event := model.NewEvent(model.StableID("calendar", calendarID, id, day.Format(model.DateLayout)), "calendar",
			id, "calendar_schedule", item.StringOr("summary", "未命名日程"), begin)
		from, to := event.Timestamp, model.ISO(finish)
		event.Start, event.End = &from, &to
		event.Description, event.URL, event.Updated = item.Text("description"), item.Text("htmlLink"), model.ISO(updated)
		event.Evidence, event.Confidence, event.Raw = "schedule", 0.4, item
		event.Metadata = model.Object{
			"calendar_id": calendarID, "all_day": allDay, "todoist_task_id": todoist,
			"original_timestamp": rawStart, "original_end_time": rawEnd, "attendance_confirmed": false,
		}
		events = append(events, event)
	}
	return events, nil
}
