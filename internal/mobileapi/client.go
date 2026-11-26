package mobileapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/workspace"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

var _ workspace.Backend = Client{}

func call[T any](ctx context.Context, c Client, route string, value any) (T, error) {
	var result T
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	body, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/api/"+route, bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, ErrDisconnected
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return result, ErrSession
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return result, err
	}
	if response.StatusCode >= 400 {
		var p Problem
		if err := json.Unmarshal(data, &p); err == nil {
			if p.Code == "journal_conflict" {
				return result, model.ErrJournalConflict
			}
			if p.Code == "review_unavailable" {
				return result, model.ErrReviewUnavailable
			}
			if p.Message != "" {
				return result, errors.New(p.Message)
			}
		}
		return result, fmt.Errorf("采集主机返回 HTTP %d", response.StatusCode)
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

func command(ctx context.Context, c Client, route string, value any) error {
	_, err := call[struct{}](ctx, c, route, value)
	return err
}

func (c Client) Overview(ctx context.Context) (workspace.Overview, error) {
	return call[workspace.Overview](ctx, c, "overview", nil)
}

func (c Client) Activity(ctx context.Context, id string) (workspace.Evidence, error) {
	return call[workspace.Evidence](ctx, c, "activity", id)
}

func (c Client) SetEventCategory(ctx context.Context, id, category string) error {
	return command(ctx, c, "set-category", CategoryChange{ID: id, Category: category})
}

func (c Client) Categories(ctx context.Context) ([]workspace.Category, error) {
	return call[[]workspace.Category](ctx, c, "categories", nil)
}

func (c Client) SaveCategories(ctx context.Context, items []workspace.CategoryInput) error {
	return command(ctx, c, "save-categories", items)
}

func (c Client) Journals(ctx context.Context, q model.BrowseQuery) (workspace.Page[model.JournalSummary], error) {
	return call[workspace.Page[model.JournalSummary]](ctx, c, "journals", q)
}

func (c Client) Activities(ctx context.Context, q model.BrowseQuery) (workspace.Page[workspace.Evidence], error) {
	return call[workspace.Page[workspace.Evidence]](ctx, c, "activities", q)
}

func (c Client) Reading(ctx context.Context, q model.BrowseQuery) (workspace.Page[model.Session], error) {
	return call[workspace.Page[model.Session]](ctx, c, "reading", q)
}

func (c Client) Journal(ctx context.Context, day string) (workspace.Journal, error) {
	return call[workspace.Journal](ctx, c, "journal", day)
}

func (c Client) Save(ctx context.Context, day, revision string, text model.JournalText, confirm bool) (model.JournalView, error) {
	return call[model.JournalView](ctx, c, "save", SaveRequest{day, revision, text, confirm})
}

func (c Client) Run(ctx context.Context) error { return command(ctx, c, "run", nil) }
func (c Client) Recover(ctx context.Context) error {
	return command(ctx, c, "recover", nil)
}
func (c Client) Logout(ctx context.Context) error { return command(ctx, c, "logout", nil) }

func (c Client) Export(ctx context.Context, target string) error {
	return command(ctx, c, "export", target)
}

func (c Client) Connections(ctx context.Context) ([]workspace.Connection, error) {
	return call[[]workspace.Connection](ctx, c, "connections", nil)
}

func (c Client) SaveConnection(ctx context.Context, v workspace.ConnectionInput) error {
	return command(ctx, c, "save-connection", v)
}

func (c Client) RemoveConnection(ctx context.Context, id string) error {
	return command(ctx, c, "remove-connection", id)
}

func (c Client) TestConnection(ctx context.Context, v workspace.ConnectionInput) (string, error) {
	return call[string](ctx, c, "test-connection", v)
}

func (c Client) Models(ctx context.Context) (workspace.ModelSettings, error) {
	return call[workspace.ModelSettings](ctx, c, "models", nil)
}

func (c Client) SaveModel(ctx context.Context, v workspace.ProfileInput) (string, error) {
	return call[string](ctx, c, "save-model", v)
}

func (c Client) TestModel(ctx context.Context, v workspace.ProfileInput) error {
	return command(ctx, c, "test-model", v)
}

func (c Client) Regenerate(ctx context.Context, days int) error {
	return command(ctx, c, "regenerate", days)
}

func (c Client) ListModels(ctx context.Context, v workspace.ProfileInput) ([]string, error) {
	return call[[]string](ctx, c, "list-models", v)
}

func (c Client) RemoveModel(ctx context.Context, id string) error {
	return command(ctx, c, "remove-model", id)
}

func (c Client) SetDefaultModel(ctx context.Context, id string) error {
	return command(ctx, c, "set-default-model", ModelChoice{ID: id})
}

func (c Client) SetJournalModelFailureMode(ctx context.Context, stop bool) error {
	return command(ctx, c, "journal-model-failure-mode", stop)
}

func (c Client) Schedule(ctx context.Context) (workspace.ScheduleSettings, error) {
	return call[workspace.ScheduleSettings](ctx, c, "schedule", nil)
}

func (c Client) SaveSchedule(ctx context.Context, v workspace.ScheduleInput) error {
	return command(ctx, c, "save-schedule", v)
}

func (c Client) InstallSchedule(context.Context) error {
	return errors.New("请在采集主机的桌面界面安装系统任务")
}
