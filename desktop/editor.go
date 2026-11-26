package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"daily-log/internal/model"
	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

func (s *screen) openJournal(day string) {
	day = strings.TrimSpace(day)
	if s.current.View.Revision != "" && s.fields != s.saved {
		s.page = "editor"
		s.failed, s.message = true, "请先保存当前修改，或点击「放弃未保存修改」"
		return
	}
	s.perform("读取日记与证据…", func() (func(), error) {
		current, err := s.backend.Journal(s.ctx, day)
		if err != nil {
			return nil, err
		}
		return func() {
			s.current = current
			s.fields = model.JournalTextFrom(current.View.Record.Payload)
			s.saved = s.fields
			s.page, s.panel, s.editorSide = "editor", "", "evidence"
			s.conflict = model.JournalView{}
		}, nil
	})
}

func (s *screen) save() {
	day, revision, fields := s.current.View.Record.Day, s.current.View.Revision, s.fields
	s.perform("保存日记…", func() (func(), error) {
		view, err := s.backend.Save(s.ctx, day, revision, fields, false)
		if errors.Is(err, model.ErrJournalConflict) {
			latest, loadErr := s.backend.Journal(s.ctx, day)
			return func() {
				if loadErr == nil {
					s.conflict, s.panel = latest.View, "conflict"
				}
			}, err
		}
		if err != nil {
			return nil, err
		}
		overview, err := s.backend.Overview(s.ctx)
		latest, historyErr := s.backend.Journal(s.ctx, day)
		return func() {
			s.current.View, s.saved = view, fields
			if historyErr == nil {
				s.current.History, s.current.Evidence = latest.History, latest.Evidence
			}
			s.conflict = model.JournalView{}
			if err == nil {
				s.overview = overview
				s.overviewLoaded = true
				s.notify("已保存日记")
			} else {
				s.failed = true
				s.message = "已保存日记，工作台刷新失败：" + err.Error()
			}
		}, nil
	})
}

// editorSideWidth is the room the day's evidence keeps beside its text:
// wide enough for a title and the line under it to be read in one breath.
const editorSideWidth = float32(330)

// editorPanelFrame is design.go's card drawn around the room it is given
// rather than around its own subject: a card usually hugs what it has to
// say, while the editor's panels hold the day itself, so they keep the
// card's room, corner, hairline and settling colors and add the growing
// the page asks of them.
func editorPanelFrame(c *ui.Context, width float32, body func()) {
	p := paletteFor(c.Theme())
	panel := ui.Column(c).MinHeight(0).Padding(spaceXL).Gap(spaceM).
		Radius(radiusCard).Border(1, p.hairline).Background(p.surface)
	edgeLit(panel, p).Transition(surfaceFade)
	if width > 0 {
		panel.Width(width).Shrink(0)
	} else {
		panel.Grow(1).Shrink(1).MinWidth(0)
	}
	panel.Children(body)
}

func (s *screen) editor(c *ui.Context) {
	width, _ := c.Size()
	wide := width >= 1040
	p := paletteFor(c.Theme())
	dirty := s.fields != s.saved
	// The day is the page's name, and nothing about the draft belongs on a
	// line of its own below it: the state of the unsaved work and the way
	// into the other panels sit at the day's side while there is room, and
	// the way alone takes a line beneath it once there is not.
	pageHeaderWith(c, "日记", s.current.View.Record.Day,
		"按上午、下午、晚上记录，也可以在本人补充里写下额外想法。", func() {
			ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Shrink(0).Children(func() {
				if s.current.View.Record.Payload.Bool("manually_edited") {
					statusPill(c, "人工编辑", toneAccent, "manual-edit")
				}
				if dirty {
					statusPill(c, "有未保存修改", toneWarn, "dirty")
				}
				ui.Textf(c, "%d 条证据", len(s.current.Evidence)).
					FontSize(sizeMeta).FontFeatures("tnum").TextColor(p.muted)
			})
			if wide {
				s.editorSwitch(c, wide)
			}
		})
	if !wide {
		actionRow(c, func() { s.editorSwitch(c, wide) })
	}
	if wide && s.panel == "" {
		ui.Row(c).Grow(1).MinHeight(0).Gap(spaceL).AlignItems(ui.Stretch).Children(func() {
			body := c.Key("editor-body")
			editorPanelFrame(body, 0, func() { s.editorPanel(body, "") })
			side := c.Key("editor-side")
			editorPanelFrame(side, editorSideWidth, func() {
				panel := s.editorSide
				if panel == "" {
					panel = "evidence"
				}
				s.editorPanel(side, panel)
			})
		})
	} else {
		editorPanelFrame(c, 0, func() { s.editorPanel(c, s.panel) })
	}
	// What is to be done with the day is offered at its foot, on the same
	// kind of row every other page offers its work from.
	toolbar(c, func() {
		if pillButton(c, "save", "保存日记", iconSpark, true, s.busy || s.jobRunning || !dirty).Clicked() {
			s.save()
		}
		if dirty && pillButton(c, "discard", "放弃未保存修改", nil, false, s.busy).Clicked() {
			s.fields = s.saved
		}
		if pillButton(c, "reload", "读取已保存版本", iconRefresh, false, s.busy || dirty).Clicked() {
			s.openJournal(s.current.View.Record.Day)
		}
	})
}

// editorSwitch is the way into the panels besides the text: one strip when
// the page has room for it, one control when it has none.
func (s *screen) editorSwitch(c *ui.Context, wide bool) {
	tabs := []designTab{{"", "正文"}, {"preview", "预览"}, {"evidence", "采集证据"}, {"history", "历史版本"}}
	if s.conflict.Revision != "" {
		tabs = append(tabs, designTab{"conflict", "版本冲突"})
	}
	if compactEditor(c) {
		labels := make([]string, len(tabs))
		var selected string
		for i, tab := range tabs {
			labels[i] = tab.label
			if s.panel == tab.id {
				selected = tab.label
			}
		}
		if ui.Select(c.Key("compact-editor-panel"), &selected, labels).Label("日记页面").Width(166).Changed() {
			if at := slices.Index(labels, selected); at >= 0 {
				s.panel = tabs[at].id
			}
		}
		return
	}
	// Every tab names the main panel. On a wide page the text keeps the
	// evidence beside it as a reference, while 采集证据 opens the evidence
	// full-width to read on its own — so the two are different views, not
	// the same page twice.
	segmentedTabs(c, tabs, s.panel, func(id string) {
		s.panel = id
	})
}

func (s *screen) editorPanel(c *ui.Context, panel string) {
	p := paletteFor(c.Theme())
	// Switching panels fades the new one in; the one leaving takes no
	// place and no pointer, and is gone.
	ui.Scroll(c.Key(panel)).Grow(1).MinHeight(0).Transition(fadeMotion()).Children(func() {
		ui.Column(c).Gap(spaceM).Children(func() {
			switch panel {
			case "preview":
				markdown.View(c, fmt.Sprintf("## 上午\n\n%s\n\n## 下午\n\n%s\n\n## 晚上\n\n%s\n\n## 本人补充\n\n%s",
					s.fields.Morning, s.fields.Afternoon, s.fields.Evening, s.fields.Note), nil)
			case "evidence":
				s.evidence(c)
			case "conflict":
				s.showConflict(c)
			case "history":
				s.history(c)
			default:
				// The tab already says where you are, so the text begins
				// at once: each period is its name over what happened in
				// it, and the four of them keep one rhythm down the page.
				for _, field := range []struct {
					label string
					text  *string
				}{{"上午", &s.fields.Morning}, {"下午", &s.fields.Afternoon}, {"晚上", &s.fields.Evening}, {"本人补充", &s.fields.Note}} {
					ui.Column(c.Key(field.label + "-field")).Gap(spaceXS).Children(func() {
						ui.Text(c.Key(field.label+"-title"), field.label).
							FontSize(sizeBody).FontWeight(weightSemibold).TextColor(p.muted)
						ui.TextArea(c.Key(field.label), field.text).Height(104).Label("编辑" + field.label).Disabled(s.busy)
					})
				}
			}
		})
	})
}

func compactEditor(c *ui.Context) bool {
	width, height := c.Size()
	return width < 720 && height < 540
}

func (s *screen) evidence(c *ui.Context) {
	payload := s.current.View.Record.Payload
	coverage := payload.Object("coverage")
	for _, source := range slices.Sorted(maps.Keys(coverage)) {
		switch coverage.Text(source) {
		case "ok":
		case "limited":
			ui.Text(c.Key(source), source+" · 仅覆盖部分时段").FontSize(sizeMeta).
				TextColor(c.Theme().Warning).LineHeight(leadBody)
		default:
			ui.Text(c.Key(source), source+" · 采集覆盖不足").FontSize(sizeMeta).
				TextColor(c.Theme().Warning).LineHeight(leadBody)
		}
	}
	links := map[string]model.Object{}
	for _, raw := range payload.List("evidence") {
		item, _ := raw.(map[string]any)
		link := model.Object(item)
		if id := link.Text("event_id"); id != "" {
			links[id] = link
		}
	}
	var linked, other []workspace.Evidence
	for _, item := range s.current.Evidence {
		if _, exists := links[item.ID]; exists {
			linked = append(linked, item)
			delete(links, item.ID)
		} else {
			other = append(other, item)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(links)) {
		link := links[id]
		linked = append(linked, workspace.Evidence{
			ID: id, Reference: id, Title: "保留的历史引用", Source: link.Text("source"), Kind: link.Text("type"),
		})
	}
	s.evidenceGroup(c.Key("linked"), "当前版本关联的证据", linked)
	s.evidenceGroup(c.Key("other"), "同日其他活动", other)
}

// evidenceGroup is one list of the day's evidence. The panel it sits in is
// already a surface, so its members rest on that surface as tiles rather
// than drawing another box with a line round it.
func (s *screen) evidenceGroup(c *ui.Context, title string, items []workspace.Evidence) {
	p := paletteFor(c.Theme())
	sectionLabel(c, title)
	if len(items) == 0 {
		ui.Text(c, "暂无记录").FontSize(sizeMeta).TextColor(p.muted)
	}
	for _, item := range items {
		tile(c.Key(item.ID), func() {
			ui.Text(c, item.Title).FontSize(sizeCardTitle).FontWeight(weightBold).
				LetterSpacing(trackingCard).LineHeight(leadTight)
			ui.Text(c, evidenceCaption(item)).FontSize(sizeMeta).FontFeatures("tnum").
				TextColor(p.muted).LineHeight(leadBody)
			actionRow(c, func() {
				if pillButton(c, "evidence-"+item.ID, "查看活动证据", iconArrow, false, s.busy).Clicked() {
					s.showActivity(item)
				}
			})
		})
	}
}

func (s *screen) showConflict(c *ui.Context) {
	p := paletteFor(c.Theme())
	latest := model.JournalTextFrom(s.conflict.Record.Payload)
	ui.Text(c, "另一处已保存更新。可以逐段选用已保存内容，再保留当前修改继续编辑。").
		FontSize(sizeMeta).TextColor(p.muted).LineHeight(leadBody)
	for _, field := range []struct {
		title   string
		current *string
		latest  string
	}{
		{"上午", &s.fields.Morning, latest.Morning}, {"下午", &s.fields.Afternoon, latest.Afternoon},
		{"晚上", &s.fields.Evening, latest.Evening}, {"本人补充", &s.fields.Note, latest.Note},
	} {
		if *field.current != field.latest {
			sunken(c.Key(field.title), func() {
				factRow(c, field.title, func() {
					// The choice keeps a row of its own: a button left
					// alone in a row with room to spare takes all of it,
					// and a bar is not a button.
					actionRow(c, func() {
						if pillButton(c, "adopt-saved-"+field.title, "采用已保存的"+field.title, nil, false, s.busy).Clicked() {
							*field.current = field.latest
						}
					})
				})
				ui.Text(c, "当前输入 · "+*field.current).FontSize(sizeBody).Selectable().LineHeight(leadBody)
				ui.Text(c, "已保存 · "+field.latest).FontSize(sizeBody).Selectable().
					LineHeight(leadBody).TextColor(p.text)
			})
		}
	}
	actionRow(c, func() {
		if pillButton(c, "conflict-baseline", "以已保存版本为基线继续编辑", iconRefresh, true, s.busy).Clicked() {
			s.current.View, s.saved = s.conflict, latest
			s.conflict, s.panel = model.JournalView{}, ""
			s.failed = false
			s.notify("已更新编辑基线，请检查正文后保存")
		}
	})
}

func (s *screen) history(c *ui.Context) {
	p := paletteFor(c.Theme())
	if len(s.current.History) == 0 {
		emptyState(c, iconClock, "暂无历史版本", "每次保存修改后，旧版本会保留在这里。")
	}
	for _, item := range s.current.History {
		tile(c.Key(item.ID), func() {
			actionRow(c, func() {
				ui.Text(c, displayTime(item.ReplacedAt)).FontSize(sizeBody).FontWeight(weightSemibold).
					FontFeatures("tnum").TextColor(p.muted)
				statusPill(c, item.Status, toneNeutral, item.ID)
			})
			for _, field := range []struct{ title, body string }{
				{"上午", item.Text.Morning}, {"下午", item.Text.Afternoon},
				{"晚上", item.Text.Evening}, {"本人补充", item.Text.Note},
			} {
				if field.body != "" {
					ui.Column(c.Key(field.title)).Gap(spaceXS).Children(func() {
						ui.Text(c, field.title).FontSize(sizeMeta).FontWeight(weightSemibold).TextColor(p.muted)
						ui.Text(c, field.body).FontSize(sizeBody).Selectable().LineHeight(leadBody)
					})
				}
			}
		})
	}
}
