package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"daily-log/internal/model"
)

func TestListModelsReadsOpenAICompatibleCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"},{"id":"deepseek-chat"}]}`)
	}))
	defer server.Close()
	client := Client{Config: model.Object{"enabled": true, "base_url": server.URL, "model": ""}, Token: "key"}
	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "deepseek-chat" || models[1] != "deepseek-reasoner" {
		t.Fatalf("unexpected model list: %v", models)
	}
}

func TestListModelsRefusesWithoutAddressOrKey(t *testing.T) {
	client := Client{Config: model.Object{"base_url": ""}, Token: ""}
	if _, err := client.ListModels(context.Background()); err == nil {
		t.Fatal("expected an error for a missing base address")
	}
	client = Client{Config: model.Object{"base_url": "https://api.example.org/v1"}}
	if _, err := client.ListModels(context.Background()); err == nil {
		t.Fatal("expected an error for a missing key on a non-anonymous service")
	}
}
