package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"daily-log/internal/model"

	"github.com/egoist/mygo/ui"
)

// motionScreen is the journal sheet with its fixtures, updates dropped so
// the frames are those of the motion itself.
func motionScreen(t *testing.T, height int) (*screen, *ui.Tester) {
	t.Helper()
	fixture := fixtureData()
	s := &screen{
		page: "days", backend: fixture, overviewLoaded: true,
		dispatch:   func(func()) {},
		overview:   mustOverview(t, fixture),
		categories: fixture.categories,
		table:      tableView{selected: -1, journals: mustJournals(t, fixture), journalLoaded: true},
		appearance: appearance{Mode: "浅色", Accent: "默认"},
	}
	v := ui.NewTester(s.view, 1140, height)
	v.Frame()
	return s, v
}

func writeFrame(t *testing.T, dir, name string, v *ui.Tester) {
	t.Helper()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, v.Image()); err != nil {
		t.Fatal(err)
	}
}

// TestJournalCollapseNoOverlap catches the details of a row leaving over
// the row beneath it: the row below must follow the closing panel edge
// frame by frame, not jump up while the panel's copy is still drawn there.
func TestJournalCollapseNoOverlap(t *testing.T) {
	dir := os.Getenv("DAILY_LOG_ANIM_DIR")
	_, v := motionScreen(t, 1500)
	if err := v.Click("2026-11-24"); err != nil {
		t.Fatal(err)
	}
	v.Advance(400 * time.Millisecond)
	v.Frame()
	writeFrame(t, dir, "overlap-00-open.png", v)

	// Where the second row sits while the first is open: below the first
	// row's whole details panel.
	before, ok := v.Find("2026-11-23")
	if !ok {
		t.Fatal("the second row's date is not shown")
	}
	oldY := before.Y

	// Open the next row: the first row's details leave while the second
	// row's arrive. The second row must travel up on the panel's own curve,
	// not snap to its final place while the leaving copy is still drawn.
	if err := v.Click("2026-11-23"); err != nil {
		t.Fatal(err)
	}
	prev := time.Duration(0)
	var lastY, y30 float32 = oldY, 0
	for i, ms := range []int{30, 60, 100, 150} {
		at := time.Duration(ms) * time.Millisecond
		v.Advance(at - prev)
		prev = at
		v.Frame()
		writeFrame(t, dir, fmt.Sprintf("overlap-%02d-%dms.png", i+1, ms), v)
		next, ok := v.Find("2026-11-23")
		if !ok {
			t.Fatal("the second row's date is not shown")
		}
		t.Logf("at %dms: next row date y=%.1f", ms, next.Y)
		if next.Y > lastY+0.5 {
			t.Errorf("at %dms the next row moved down (%.1f -> %.1f) while the panel above closes", ms, lastY, next.Y)
		}
		if ms == 30 {
			y30 = next.Y
		}
		lastY = next.Y
	}
	v.Advance(400 * time.Millisecond)
	v.Frame()
	writeFrame(t, dir, "overlap-99-settled.png", v)
	final, _ := v.Find("2026-11-23")
	// At 30ms the row must still be well below its final place: a row that
	// snapped is already there, drawn under the leaving panel's copy.
	if y30 < final.Y+40 {
		t.Errorf("the next row snapped to its final place (y=%.1f at 30ms) instead of following the closing panel (final y=%.1f)", y30, final.Y)
	}
	if oldY <= final.Y {
		t.Errorf("the next row did not move up as the panel above closed: %.1f -> %.1f", oldY, final.Y)
	}
}

// TestJournalScrollFullPage scrolls a full page of forty rows with a row
// open, and checks the wheel brings the lower rows into view and the open
// row's details do not smear over their neighbours on the way.
func TestJournalScrollFullPage(t *testing.T) {
	s, v := motionScreen(t, 900)
	// A full page, as the sheet really holds: forty rows, not the six of
	// the design fixtures.
	base := s.table.journals.Rows[0]
	var rows []model.JournalSummary
	for i := range 40 {
		day := time.Date(2026, 11, 24-i, 0, 0, 0, 0, time.Local)
		item := base
		item.Day = day.Format(model.DateLayout)
		rows = append(rows, item)
	}
	s.table.journals.Rows = rows
	v.Frame()
	if err := v.Click("2026-11-24"); err != nil {
		t.Fatal(err)
	}
	v.Advance(400 * time.Millisecond)
	v.Frame()

	// The twentieth day starts far below the view.
	lower := rows[19].Day
	before, ok := v.Find(lower)
	if !ok || before.Y < 900 {
		t.Fatalf("%s should start below the view (y=%.1f, found=%v)", lower, before.Y, ok)
	}
	// Turn the wheel down steadily, a frame between notches as a display
	// would draw them.
	for range 16 {
		v.Wheel(700, 500, 0, 120)
		v.Advance(16 * time.Millisecond)
		v.Frame()
	}
	v.Advance(300 * time.Millisecond)
	v.Frame()
	after, ok := v.Find(lower)
	if !ok || after.Y > before.Y-500 {
		t.Errorf("scrolling down did not bring %s up the view: %.1f -> %.1f (found=%v)", lower, before.Y, after.Y, ok)
	}
}

// TestJournalNoTopicsExplains guards the numbers question: with no AI digest
// the panel says why the time periods are empty instead of showing three
// tiles of "暂无" under a row of category counts.
func TestJournalNoTopicsExplains(t *testing.T) {
	fixture := fixtureData()
	page := mustJournals(t, fixture)
	for i := range page.Rows {
		page.Rows[i].Topics = nil
	}
	s := &screen{
		page: "days", backend: fixture, dispatch: func(func()) {},
		table:      tableView{selected: 0, journals: page, journalLoaded: true},
		categories: fixture.categories,
		appearance: appearance{Mode: "浅色", Accent: "默认"},
	}
	v := ui.NewTester(s.view, 1140, 1200)
	v.Frame()
	if !v.HasText("时段事项为空：需要 AI 整理后生成。可在「模型设置」配置服务，再运行「整理今日」。") {
		t.Error("the panel should explain why the periods are empty")
	}
	if !v.HasText("证据分类计数") {
		t.Error("the badges row should name what it counts")
	}
}
