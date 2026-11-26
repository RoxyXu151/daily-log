package mobilehost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"daily-log/internal/mobileapi"
	"daily-log/internal/model"
	"daily-log/internal/workspace"
)

func routes(backend workspace.Backend) *http.ServeMux {
	mux := http.NewServeMux()
	read(mux, "overview", backend.Overview)
	endpoint(mux, "journals", backend.Journals)
	endpoint(mux, "activities", backend.Activities)
	endpoint(mux, "activity", backend.Activity)
	write(mux, "set-category", func(ctx context.Context, v mobileapi.CategoryChange) error {
		return backend.SetEventCategory(ctx, v.ID, v.Category)
	})
	read(mux, "categories", backend.Categories)
	write(mux, "save-categories", backend.SaveCategories)
	endpoint(mux, "reading", backend.Reading)
	endpoint(mux, "journal", backend.Journal)
	endpoint(mux, "save", func(ctx context.Context, v mobileapi.SaveRequest) (model.JournalView, error) {
		return backend.Save(ctx, v.Day, v.Revision, v.Text, v.Confirm)
	})
	action(mux, "run", backend.Run)
	action(mux, "recover", backend.Recover)
	write(mux, "regenerate", backend.Regenerate)
	write(mux, "export", backend.Export)
	read(mux, "schedule", backend.Schedule)
	write(mux, "save-schedule", backend.SaveSchedule)
	read(mux, "connections", func(ctx context.Context) ([]workspace.Connection, error) {
		list, err := backend.Connections(ctx)
		for i := range list {
			for j := range list[i].Fields {
				field := &list[i].Fields[j]
				if !canChange(list[i].ID, field.Key) {
					field.ReadOnly, field.Value = true, ""
				}
			}
		}
		return list, err
	})
	write(mux, "save-connection", func(ctx context.Context, v workspace.ConnectionInput) error {
		if !mobileConnection(v) {
			return errDesktopOnly
		}
		return backend.SaveConnection(ctx, v)
	})
	endpoint(mux, "test-connection", func(ctx context.Context, v workspace.ConnectionInput) (string, error) {
		if !mobileConnection(v) {
			return "", errDesktopOnly
		}
		return backend.TestConnection(ctx, v)
	})
	write(mux, "remove-connection", backend.RemoveConnection)
	read(mux, "models", backend.Models)
	endpoint(mux, "save-model", backend.SaveModel)
	write(mux, "test-model", backend.TestModel)
	endpoint(mux, "list-models", backend.ListModels)
	write(mux, "remove-model", backend.RemoveModel)
	write(mux, "journal-model-failure-mode", backend.SetJournalModelFailureMode)
	write(mux, "set-default-model", func(ctx context.Context, v mobileapi.ModelChoice) error {
		return backend.SetDefaultModel(ctx, v.ID)
	})
	return mux
}

func canChange(source, key string) bool {
	switch source {
	case "zotero":
		return key == "library" || key == "session_gap_minutes"
	case "todoist":
		return key == "course_projects" || key == "token"
	case "calendar":
		return key == "calendar_ids"
	case "codex":
		return key == "include_thread_ids" || key == "exclude_thread_ids" || key == "start_date"
	default:
		return false
	}
}

func mobileConnection(v workspace.ConnectionInput) bool {
	for key := range v.Values {
		if !canChange(v.ID, key) {
			return false
		}
	}
	for key := range v.Secrets {
		if !canChange(v.ID, key) {
			return false
		}
	}
	return true
}

func endpoint[Input, Output any](mux *http.ServeMux, name string, fn func(context.Context, Input) (Output, error)) {
	mux.HandleFunc("POST /api/"+name, func(w http.ResponseWriter, r *http.Request) {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "application/json" {
			problem(w, http.StatusUnsupportedMediaType, "content_type", "请求须为 JSON")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var v Input
		var extra any
		if err := decoder.Decode(&v); err != nil || decoder.Decode(&extra) != io.EOF {
			problem(w, http.StatusBadRequest, "invalid_json", "请求内容无效或过长")
			return
		}
		data, err := fn(r.Context(), v)
		if err != nil {
			writeError(w, err)
			return
		}
		jsonResponse(w, http.StatusOK, data)
	})
}

func read[Output any](mux *http.ServeMux, name string, fn func(context.Context) (Output, error)) {
	endpoint(mux, name, func(ctx context.Context, _ struct{}) (Output, error) { return fn(ctx) })
}

func write[Input any](mux *http.ServeMux, name string, fn func(context.Context, Input) error) {
	endpoint(mux, name, func(ctx context.Context, v Input) (struct{}, error) { return struct{}{}, fn(ctx, v) })
}

func action(mux *http.ServeMux, name string, fn func(context.Context) error) {
	read(mux, name, func(ctx context.Context) (struct{}, error) { return struct{}{}, fn(ctx) })
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrJournalConflict):
		problem(w, http.StatusConflict, "journal_conflict", err.Error())
	case errors.Is(err, model.ErrReviewUnavailable):
		problem(w, http.StatusConflict, "review_unavailable", err.Error())
	case errors.Is(err, errDesktopOnly):
		problem(w, http.StatusForbidden, "desktop_only", err.Error())
	default:
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			problem(w, http.StatusBadRequest, "operation", "采集主机无法访问相关文件，请检查桌面端配置")
			return
		}
		message := strings.SplitN(err.Error(), "\n", 2)[0]
		if strings.Contains(message, "无法解析配置") {
			message = "采集主机的配置文件有误，请在桌面端检查"
		}
		problem(w, http.StatusBadRequest, "operation", message)
	}
}

func problem(w http.ResponseWriter, status int, code, message string) {
	jsonResponse(w, status, mobileapi.Problem{Code: code, Message: message})
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
