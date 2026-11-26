package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageExcludesPrivateFiles(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "stage")
	if err = stage(root, target); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.yaml", ".env", "data", "secrets", "outputs", "dist", ".venv", "pyproject.toml"} {
		if _, err = os.Stat(filepath.Join(target, name)); !os.IsNotExist(err) {
			t.Fatalf("private or obsolete file included: %s", name)
		}
	}
	files, err := all(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, ".py") {
			t.Fatalf("obsolete source included: %s", name)
		}
	}
	for _, name := range []string{"go.mod", "cmd/daily-log/main.go", "internal/resources/default.yaml", "desktop/go.mod"} {
		if _, err = os.Stat(filepath.Join(target, name)); err != nil {
			t.Fatalf("missing build input %s", name)
		}
	}
}
