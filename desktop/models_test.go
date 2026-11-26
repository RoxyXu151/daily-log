package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestModelRowDisclosure checks the sources-page behavior on models: the row
// opens its editor in place, the chevron closes it again.
func TestModelRowDisclosure(t *testing.T) {
	fixture := fixtureData()
	s := &screen{
		page: "models", backend: fixture, dispatch: func(func()) {},
		models: mustModels(t, fixture), appearance: appearance{Mode: "浅色", Accent: "默认"},
	}
	v := ui.NewTester(s.view, 1140, 1400)
	v.Frame()
	if v.HasText("配置名称") {
		t.Fatal("no row should start open")
	}
	profile := s.models.Profiles[1]
	if err := v.Click(profile.Name + " · " + profile.Model); err != nil {
		t.Fatal(err)
	}
	v.Advance(300 * time.Millisecond)
	v.Frame()
	if !v.HasText("配置名称") || !s.profile.open || s.profile.profile.ID != profile.ID {
		t.Fatal("clicking the row should open its editor in place")
	}
	if s.profile.preset != presetFor(profile.BaseURL) {
		t.Errorf("preset should match the saved address, got %q", s.profile.preset)
	}
	// The row toggles: clicking it again closes the editor.
	if err := v.Click(profile.Name + " · " + profile.Model); err != nil {
		t.Fatal(err)
	}
	v.Advance(300 * time.Millisecond)
	v.Frame()
	if s.profile.open {
		t.Error("the chevron should close the row again")
	}
}

// TestFetchModelChoices fills 模型名称 from the service's own catalog: the
// button asks the backend and the answer becomes a picker.
func TestFetchModelChoices(t *testing.T) {
	fixture := fixtureData()
	done := make(chan struct{})
	s := &screen{
		page: "models", backend: fixture, dispatch: func(f func()) { f(); close(done) },
		models: mustModels(t, fixture), appearance: appearance{Mode: "浅色", Accent: "默认"},
	}
	v := ui.NewTester(s.view, 1140, 1400)
	v.Frame()
	profile := s.models.Profiles[0]
	if err := v.Click(profile.Name + " · " + profile.Model); err != nil {
		t.Fatal(err)
	}
	v.Advance(300 * time.Millisecond)
	v.Frame()
	if err := v.Click("获取模型列表"); err != nil {
		t.Fatal(err)
	}
	<-done
	v.Advance(100 * time.Millisecond)
	v.Frame()
	if len(s.modelChoices) != 2 {
		t.Fatalf("the fetched list should reach the form, got %v", s.modelChoices)
	}
	if !v.HasText("从官方列表选择") {
		t.Error("the fetched models should be offered as a picker")
	}
}
