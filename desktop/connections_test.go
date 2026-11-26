package main

import (
	"context"
	"testing"
	"time"

	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

// TestToggleSwitchDoesNotOpenCard flips a closed source's collection switch
// and checks the card stays closed: the switch decides collecting, not
// reading parameters, so it must not unfold the row under the pointer.
func TestToggleSwitchDoesNotOpenCard(t *testing.T) {
	fixture := fixtureData()
	conns, err := fixture.Connections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	s := screen{
		page: "connections", backend: fixture, ctx: context.Background(),
		connections: conns,
		dispatch:    func(f func()) { f(); close(done) },
	}
	item := s.connections[0]
	s.setConnectionEnabled(item, !item.Enabled)
	<-done
	if s.connection.selected.ID != "" {
		t.Errorf("flipping the switch opened card %q; it should stay closed", s.connection.selected.ID)
	}

	// With another card open, the switch leaves that one open too.
	done = make(chan struct{})
	s.dispatch = func(f func()) { f(); close(done) }
	s.selectConnection(s.connections[1])
	s.setConnectionEnabled(item, item.Enabled)
	<-done
	if s.connection.selected.ID != s.connections[1].ID {
		t.Errorf("flipping a closed card's switch changed the open card to %q", s.connection.selected.ID)
	}
}

func TestSourceDisclosureMovesFollowingCards(t *testing.T) {
	s := screen{
		page: "connections",
		connections: []workspace.Connection{
			{ID: "todoist", Label: "Todoist", Added: true},
			{ID: "codex", Label: "Codex", Added: true},
		},
	}
	view := ui.NewTester(s.view, 1100, 800)
	click := func(label string) {
		t.Helper()
		if err := view.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	nextCardY := func() float32 {
		t.Helper()
		card, ok := view.Find("Codex · 暂停")
		if !ok {
			t.Fatal("missing card below Todoist")
		}
		return card.Y
	}

	closed := nextCardY()
	click("Todoist · 暂停")
	view.Advance(70 * time.Millisecond)
	opening := nextCardY()
	view.Advance(disclosure)
	opened := nextCardY()
	if opening <= closed || opening >= opened {
		t.Fatalf("following card should move down during opening: %v, %v, %v", closed, opening, opened)
	}

	click("收起Todoist")
	view.Advance(70 * time.Millisecond)
	closing := nextCardY()
	view.Advance(disclosure)
	if closing <= closed || closing >= opened || nextCardY() != closed {
		t.Fatalf("following card should return during closing: %v, %v, %v", opened, closing, nextCardY())
	}
}

// TestAddSourceFromCatalog checks the 添加来源 panel: catalog sources stay
// out of the list until added, name their standard integration, and adding
// one opens its card for credentials.
func TestAddSourceFromCatalog(t *testing.T) {
	fixture := fixtureData()
	done := make(chan struct{})
	s := &screen{
		page: "connections", backend: fixture, dispatch: func(f func()) { f(); close(done) },
		connections: mustConnections(t, fixture), categories: fixture.categories,
		overviewLoaded: true, overview: mustOverview(t, fixture),
		appearance: appearance{Mode: "浅色", Accent: "默认"},
	}
	v := ui.NewTester(s.view, 1140, 1400)
	v.Frame()
	if v.HasText("GitHub · 暂停") {
		t.Fatal("a catalog source should not sit in the list before it is added")
	}
	if err := v.Click("添加来源"); err != nil {
		t.Fatal(err)
	}
	v.Advance(300 * time.Millisecond)
	v.Frame()
	if !v.HasText("官方 API") || !v.HasText("本机 API") || !v.HasText("本地文件") {
		t.Fatal("the catalog should name each source's standard integration")
	}
	if err := v.Click("添加"); err != nil {
		t.Fatal(err)
	}
	<-done
	v.Advance(300 * time.Millisecond)
	v.Frame()
	if s.connection.selected.ID != "github" {
		t.Errorf("adding should open the new card, got %q", s.connection.selected.ID)
	}
	if s.addCatalog {
		t.Error("the catalog should close once a source is added")
	}
}
