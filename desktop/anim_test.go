package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestAnimationFrames writes the frames of the app's three signature moves
// — a page changing, a row opening, a notice arriving — so that the motion
// can be looked at as a strip rather than argued about from its numbers.
func TestAnimationFrames(t *testing.T) {
	dir := os.Getenv("DAILY_LOG_ANIM_DIR")
	if dir == "" {
		t.Skip("set DAILY_LOG_ANIM_DIR to write frames")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := fixtureData()
	newScreen := func() *screen {
		// Updates are dropped: the frames worth looking at are those of the
		// change itself, before any answer has come to replace it.
		drop := func(func()) {}
		return &screen{
			page: "", backend: fixture, overviewLoaded: true, dispatch: drop,
			overview: mustOverview(t, fixture), categories: fixture.categories,
			table:      tableView{selected: -1, journals: mustJournals(t, fixture), journalLoaded: true},
			appearance: appearance{Mode: "浅色", Accent: "默认"},
		}
	}
	write := func(name string, img image.Image) {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}

	// A page changing under a sidebar click.
	s := newScreen()
	view := ui.NewTester(s.view, 1140, 800)
	view.Frame()
	if err := view.Click("日记列表"); err != nil {
		t.Fatal(err)
	}
	for i, at := range []time.Duration{0, 40 * time.Millisecond, 90 * time.Millisecond, 180 * time.Millisecond} {
		view.Advance(at - frameDelta(i, []time.Duration{0, 40, 90, 180}))
		view.Frame()
		write(fmt.Sprintf("page-%02d-%dms.png", i, at/time.Millisecond), view.Image())
	}

	// A row of the sheet opening.
	s = newScreen()
	s.page, s.table.journalLoaded = "days", true
	view = ui.NewTester(s.view, 1140, 900)
	view.Frame()
	if err := view.Click("2026-11-24"); err != nil {
		t.Fatal(err)
	}
	for i, at := range []time.Duration{0, 50 * time.Millisecond, 110 * time.Millisecond, 200 * time.Millisecond} {
		view.Advance(at - frameDelta(i, []time.Duration{0, 50, 110, 200}))
		view.Frame()
		write(fmt.Sprintf("row-%02d-%dms.png", i, at/time.Millisecond), view.Image())
	}

	// A notice arriving, and the page not moving under it.
	s = newScreen()
	view = ui.NewTester(s.view, 1140, 800)
	view.Frame()
	before := rowY(view)
	s.failed, s.message = true, "无法连接采集主机：令牌已过期"
	view.Frame()
	for i, at := range []time.Duration{0, 60 * time.Millisecond, 140 * time.Millisecond, 300 * time.Millisecond} {
		view.Advance(at - frameDelta(i, []time.Duration{0, 60, 140, 300}))
		view.Frame()
		write(fmt.Sprintf("notice-%02d-%dms.png", i, at/time.Millisecond), view.Image())
	}
	if after := rowY(view); after != before {
		t.Errorf("the page moved under a notice: %v then %v", before, after)
	}

	// The same, asked for less motion.
	s = newScreen()
	view = ui.NewTesterWith(s.view, 1140, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	view.Frame()
	if err := view.Click("日记列表"); err != nil {
		t.Fatal(err)
	}
	view.Frame()
	write("still-after-page.png", view.Image())
}

// frameDelta is how far the clock has to move to reach the i-th frame.
func frameDelta(i int, at []time.Duration) time.Duration {
	if i == 0 {
		return 0
	}
	return at[i-1]
}

// rowY is where the first card of the工作台 sits, so that a notice moving
// the page can be caught rather than asserted.
func rowY(view *ui.Tester) float32 {
	r, ok := view.Find("今日日记")
	if !ok {
		return -1
	}
	return r.Y
}
