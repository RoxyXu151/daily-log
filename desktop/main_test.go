//go:build !js

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"daily-log/internal/runtimeapp"
)

func TestDesktopStartsWithLocalConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daily-log", "config.yaml")
	if err := ensureLocalConfig(path); err != nil {
		t.Fatal(err)
	}
	backend := runtimeapp.Workspace{ConfigPath: path}
	if overview, err := backend.Overview(context.Background()); err != nil || overview.Today == "" {
		t.Fatalf("initial overview = %+v, %v", overview, err)
	}

	const existing = "# Existing user configuration\n"
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalConfig(path); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(path); err != nil || string(contents) != existing {
		t.Fatalf("existing configuration was changed: %q, %v", contents, err)
	}
}
