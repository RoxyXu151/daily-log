package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCategoryShortTravels: the 简称 saved for a custom category is the one
// label both pages show — the manager names it, the journal legend uses it.
func TestCategoryShortTravels(t *testing.T) {
	fixture := fixtureData()
	s := &screen{
		page: "categories", backend: fixture, dispatch: func(func()) {},
		categories: fixture.categories,
		appearance: appearance{Mode: "浅色", Accent: "默认"},
	}
	s.resetCategoryEdits()
	v := ui.NewTester(s.view, 1140, 1200)
	v.Frame()
	if !v.HasText("在日记总表中显示为「写作」。") {
		t.Fatal("the manager should name the short label it saves")
	}

	s.page = "days"
	s.table = tableView{selected: -1, journals: mustJournals(t, fixture), journalLoaded: true}
	s.applyCategories(s.categories)
	v.Frame()
	if !v.HasText("写作") {
		t.Fatal("the journal legend should show the saved short label")
	}
	if v.HasText("论文写作") {
		t.Error("the journal legend should not fall back to the full name")
	}
}
