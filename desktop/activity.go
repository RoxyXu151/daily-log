package main

import (
	"net/url"
	"slices"
	"strings"

	"daily-log/internal/model"
	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

func (s *screen) recentActivity(c *ui.Context) {
	p := paletteFor(c.Theme())
	card(c, "近期活动", func() {
		if len(s.overview.Recent) == 0 {
			emptyState(c, iconActivity, "暂无活动", "今天采集到的记录会出现在这里。")
		}
		for _, item := range s.overview.Recent {
			// A row of this list is the rest of the app's rows: what
			// happened, then under it where it came from and when.
			tile(c.Key(item.ID), func() {
				title := []rune(item.Title)
				if len(title) == 0 {
					title = []rune("未命名活动")
				} else if len(title) > 30 {
					title = append(title[:30], '…')
				}
				if plainButton(c, item.ID+"-open", string(title), s.busy).Clicked() {
					s.perform("读取活动证据…", func() (func(), error) {
						detail, err := s.backend.Activity(s.ctx, item.ID)
						return func() {
							if err == nil {
								s.showActivity(detail)
							}
						}, err
					})
				}
				ui.Text(c, item.Source+" · "+displayTime(item.Timestamp)).FontSize(sizeMeta).
					FontFeatures("tnum").TextColor(p.muted).PaddingStart(spaceS)
			})
		}
		actionRow(c, func() {
			if pillButton(c, "open-activity", "打开活动流水", iconArrow, false, s.busy).Clicked() {
				s.loadTable("activity", model.BrowseQuery{})
			}
		})
	})
}

func (s *screen) showActivity(item workspace.Evidence) {
	s.activityBack, s.activityDetail, s.page = s.page, item, "activity-detail"
}

func (s *screen) setActivityCategory(id, category string) {
	applied, refreshJournals := s.table.applied, len(s.table.journals.Rows) > 0
	s.perform("正在保存活动分类…", func() (func(), error) {
		if err := s.backend.SetEventCategory(s.ctx, id, category); err != nil {
			return nil, err
		}
		updated, err := s.backend.Activity(s.ctx, id)
		if err != nil {
			return nil, err
		}
		var journals workspace.Page[model.JournalSummary]
		if refreshJournals {
			journals, err = s.backend.Journals(s.ctx, applied)
			if err != nil {
				return nil, err
			}
		}
		return func() {
			s.activityDetail = updated
			for i := range s.table.activities.Rows {
				if s.table.activities.Rows[i].ID == id {
					s.table.activities.Rows[i] = updated
				}
			}
			for i := range s.current.Evidence {
				if s.current.Evidence[i].ID == id {
					s.current.Evidence[i] = updated
				}
			}
			if refreshJournals {
				s.table.journals = journals
			}
			s.notify("活动分类已更新；后续重新采集会保留这次手工设置")
		}, nil
	})
}

func (s *screen) activityScreen(c *ui.Context) {
	label := "返回工作台"
	switch s.activityBack {
	case "editor":
		label = "返回日记"
	case "days":
		label = "返回日记总表"
	}
	// The way out of a piece of evidence is said with its name, the way
	// every other page says what it can do: a reader who has finished
	// reading looks for it where the page began, not above it.
	pageHeaderWith(c, "EVIDENCE", "活动证据",
		"来自来源的原始记录，保留完整上下文供日记引用。", func() {
			if pillButton(c, "activity-back", label, iconArrow, false, false).Clicked() {
				s.page = s.activityBack
			}
		})
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		s.evidenceCard(c, s.activityDetail)
	})
}

func evidenceCaption(item workspace.Evidence) string {
	kinds := map[string]string{"schedule": "日程计划", "completed": "已完成", "inferred": "推断", "manual": "手动提供", "direct": "直接记录"}
	kind := kinds[item.Kind]
	if kind == "" {
		kind = item.Kind
	}
	parts := []string{item.Source, kind, item.EventType, displayTime(item.Timestamp)}
	return strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), "  ·  ")
}

func displayTime(value string) string {
	if at, err := model.ParseTime(value, model.Shanghai); err == nil {
		return at.Format("2006-01-02 15:04")
	}
	return value
}

func (s *screen) evidenceCard(c *ui.Context, item workspace.Evidence) {
	p := paletteFor(c.Theme())
	card(c, item.Title, func() {
		// Where the record came from is told in the pills, and the rest of
		// what they stand for finishes the line beside them.
		actionRow(c, func() {
			statusPill(c, item.Source, toneAccent, item.ID+"-source")
			if item.Category != "" {
				statusPill(c, item.Category, toneNeutral, item.ID+"-category")
			}
			ui.Text(c, evidenceCaption(item)).FontSize(sizeMeta).FontFeatures("tnum").
				TextColor(p.muted).LineHeight(leadBody).Grow(1).Shrink(1).MinWidth(0)
		})
		// The class this record has been given is one block of the card,
		// said from the card's own edge: the choices answer the words
		// above them, and the words about the answer belong under the
		// choices, not out where nothing names them.
		ui.Column(c).Gap(spaceXS).Children(func() {
			ui.Text(c, "手工分类").FontSize(sizeSmall).FontWeight(weightSemibold).
				LetterSpacing(trackingLabel).TextColor(p.muted)
			categories := s.categoryNames()
			selected := item.Category
			if !slices.Contains(categories, selected) {
				if len(categories) > 0 {
					selected = categories[0]
				} else {
					selected = model.Uncategorized
				}
			}
			actionRow(c, func() {
				for _, category := range categories {
					if choicePill(c, "manual-category-"+item.ID+"-"+category, categoryShort(category), category == selected, s.busy).Clicked() {
						s.setActivityCategory(item.ID, category)
					}
				}
			})
			ui.Text(c, "修改后会同步更新日记总表的分类计数。").FontSize(sizeMeta).TextColor(p.muted).
				LineHeight(leadBody)
		})
		if item.Reference != "" {
			ui.Text(c, "来源记录 "+item.Reference).FontSize(sizeMeta).Selectable().TextColor(p.muted)
		}
		if item.Description != "" {
			sunken(c, func() {
				ui.Text(c, item.Description).FontSize(sizeBody).LineHeight(leadLoose).Selectable()
			})
		}
		s.sourceLink(c, item.Source, item.URL)
	})
}

// sourceLink is where a record came from, said once: the address itself,
// and the way to it on the same line rather than on a line of its own
// under it, so the address reads as what the button opens.
func (s *screen) sourceLink(c *ui.Context, source, link string) {
	if link == "" {
		return
	}
	p := paletteFor(c.Theme())
	openable := s.openLink != nil && permittedSourceLink(source, link, s.remote)
	ui.Row(c).Gap(spaceM).AlignItems(ui.Center).Wrap().Children(func() {
		ui.Text(c, link).FontSize(sizeMeta).Selectable().TextColor(p.muted).
			Grow(1).Shrink(1).MinWidth(0).LineHeight(leadBody)
		if openable {
			if pillButton(c, "open-source", "打开原始记录", iconArrow, false, false).Clicked() {
				if err := s.openLink(link); err != nil {
					s.failed, s.message = true, "无法打开来源链接"
				}
			}
		}
	})
}

func permittedSourceLink(source, link string, remote bool) bool {
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return false
	}
	switch u.Scheme {
	case "https", "http":
		return true
	case "zotero":
		return !remote && source == "zotero" && (u.Host == "select" || u.Host == "open-pdf")
	case "codex":
		return !remote && source == "codex" && u.Host == "threads"
	default:
		return false
	}
}
