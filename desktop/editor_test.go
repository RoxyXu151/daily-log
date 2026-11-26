package main

import (
	"context"
	"testing"

	"daily-log/internal/model"

	"github.com/egoist/mygo/ui"
)

// TestEditorEvidenceTabIsDistinct checks that on a wide page 正文 and
// 采集证据 are different views: the text keeps the evidence beside it as a
// reference, while 采集证据 reads the evidence full-width on its own. They
// used to render the same two-panel page.
func TestEditorEvidenceTabIsDistinct(t *testing.T) {
	fixture := fixtureData()
	j, err := fixture.Journal(context.Background(), "2026-11-24")
	if err != nil {
		t.Fatal(err)
	}
	s := &screen{
		page: "editor", backend: fixture, overviewLoaded: true,
		categories: fixture.categories,
		appearance: appearance{Mode: "浅色", Accent: "默认"},
	}
	s.current = j
	s.fields = model.JournalTextFrom(j.View.Record.Payload)
	s.saved = s.fields
	view := ui.NewTester(s.view, 1140, 800)

	// The period input is unique to the body (the page subtitle also says
	// 上午, so match the field's own label, not any text containing it).
	const bodyField = "编辑上午"

	// 正文: the period fields and the evidence side by side.
	s.panel = ""
	view.Frame()
	if _, ok := view.Find(bodyField); !ok {
		t.Fatal("正文 should show the period fields")
	}
	if !view.HasText("当前版本关联的证据") {
		t.Fatal("正文 should keep the evidence beside the text")
	}

	// 采集证据: the evidence full-width, without the period fields.
	s.panel = "evidence"
	view.Frame()
	if !view.HasText("当前版本关联的证据") {
		t.Fatal("采集证据 should show the evidence")
	}
	if _, ok := view.Find(bodyField); ok {
		t.Error("采集证据 renders the same page as 正文 (period fields present); the tabs are redundant")
	}
}
