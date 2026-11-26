package main

import (
	"fmt"
	"strings"

	"daily-log/internal/model"
	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

type tableView struct {
	query      model.BrowseQuery
	applied    model.BrowseQuery
	journals   workspace.Page[model.JournalSummary]
	activities workspace.Page[workspace.Evidence]
	state      ui.ListState
	selected   int
	detail     bool
	// Whether the first page of each sheet has come: until it does, the
	// sheet shows its skeleton instead of an empty state.
	journalLoaded, activityLoaded bool
}

func (s *screen) loadTable(page string, filter model.BrowseQuery) {
	filter.Day, filter.Keyword = strings.TrimSpace(filter.Day), strings.TrimSpace(filter.Keyword)
	filter.Source, filter.Category = strings.TrimSpace(filter.Source), strings.TrimSpace(filter.Category)
	if filter.Day != "" {
		if _, err := model.ParseDay(filter.Day); err != nil {
			s.failed, s.message = true, "请使用 YYYY-MM-DD 格式输入日期"
			return
		}
	}
	query := filter
	if query.Category == "" {
		query.Category = allCategories
	}
	if query.Category == allCategories {
		filter.Category = ""
	}
	// The page changes at once and its skeleton shows while the query
	// runs, as mygo-music turns to a page before its data has come.
	s.page = page
	table := s.table
	table.query, table.applied, table.selected = query, filter, -1
	s.perform("查询表格…", func() (func(), error) {
		loading := table
		categories, err := s.backend.Categories(s.ctx)
		if err != nil {
			return nil, err
		}
		switch page {
		case "days":
			loading.journals, err = s.backend.Journals(s.ctx, filter)
			loading.journalLoaded = true
		case "activity":
			loading.activities, err = s.backend.Activities(s.ctx, filter)
			loading.activityLoaded = true
		}
		if err != nil {
			return nil, err
		}
		return func() {
			s.table = loading
			s.applyCategories(categories)
		}, nil
	})
}

func (s *screen) tableScreen(c *ui.Context) {
	if s.page == "days" {
		s.journalSheet(c)
		return
	}
	s.activityTable(c)
}

// The sheet's columns, once, so that a header and its rows are cut from
// the same measure and cannot drift apart.
const (
	journalDayWidth    = float32(112)
	journalCountWidth  = float32(120)
	journalCatsWidth   = float32(216)
	journalRecordWidth = float32(176)
	journalRowHeight   = float32(84)
	journalRowTall     = float32(104)
	journalInset       = float32(16)
	activityRowHeight  = float32(60)
)

func (s *screen) journalSheet(c *ui.Context) {
	t := &s.table
	p := paletteFor(c.Theme())
	pageHeader(c, "JOURNAL OVERVIEW", "日记总表", "按日期查看 AI 凝练的当天事项、分类与证据；展开后按上午、下午、晚上查看高价值工作。")
	toolbar(c, func() {
		date := ui.TextInput(c.Key("date"), &t.query.Day).Placeholder("YYYY-MM-DD").Label("日期").Width(132)
		ui.Select(c.Key("category"), &t.query.Category, s.categoryChoices()).Width(132).Label("活动分类")
		if pillButton(c, "query", "查询", iconSearch, true, s.busy).Clicked() || date.Submitted() && !s.busy {
			filter := t.query
			filter.Offset = 0
			s.loadTable("days", filter)
		}
		if pillButton(c, "open-day", "打开指定日期", nil, false,
			s.busy || strings.TrimSpace(t.query.Day) == "").Clicked() {
			s.openJournal(t.query.Day)
		}
	})
	categoryLegend(c, s.categoryNames())
	edgeLit(ui.Column(c).Grow(1).MinHeight(0).Radius(radiusSheet).Border(1, p.hairline).
		Background(p.surface).Clip(), p).
		Transition(surfaceFade).Children(func() {
		journalTableHeader(c)
		if s.busy && !t.journalLoaded {
			journalSkeletonRows(c)
			return
		}
		if len(t.journals.Rows) == 0 {
			emptyState(c, iconSearch, "暂无匹配日记", "调整日期、状态或分类后再试。")
			return
		}
		ui.Scroll(c.Key("journal-scroll")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Gap(0).Children(func() {
				for row, item := range t.journals.Rows {
					s.journalOverviewRow(c, row, item, p, row == len(t.journals.Rows)-1)
				}
			})
		})
	})
	s.tableFooter(c, "days", len(t.journals.Rows), t.journals.More, func() {
		if t.selected >= 0 && t.selected < len(t.journals.Rows) {
			s.openJournal(t.journals.Rows[t.selected].Day)
		}
	})
}

// journalShowsCounts is whether a row has room to be told in columns.
// Below it the day and what it held keep the width: the counts are said
// again inside the row as soon as it is opened, so a window too narrow
// for both loses the counts rather than the sentence.
func journalShowsCounts(width float32) bool { return width >= 720 }

func journalTableHeader(c *ui.Context) {
	p := paletteFor(c.Theme())
	width, _ := c.Size()
	ui.Row(c).Height(heightHeader).PaddingX(journalInset).Gap(spaceL).AlignItems(ui.Center).Background(p.raised).
		BorderWidth(0, 0, 1, 0).BorderColor(p.hairline).Children(func() {
		journalHeaderCell(c, "日期", journalDayWidth)
		journalHeaderCell(c, "今日概要", 0)
		if journalShowsCounts(width) {
			journalHeaderCell(c, "事项", journalCountWidth)
		}
		if width >= 1300 {
			journalHeaderCell(c, "分类分布", journalCatsWidth)
			journalHeaderCell(c, "记录", journalRecordWidth)
		}
		ui.Box(c).Width(spaceXL)
	})
}

func journalHeaderCell(c *ui.Context, label string, width float32) {
	text := ui.Text(c, label).FontSize(sizeSmall).FontWeight(weightSemibold).
		LetterSpacing(trackingLabel).TextColor(paletteFor(c.Theme()).muted).SingleLine()
	if width > 0 {
		text.Width(width).Shrink(0)
	} else {
		text.Grow(1).Shrink(1).MinWidth(0)
	}
}

func (s *screen) journalOverviewRow(c *ui.Context, row int, item model.JournalSummary, p palette, last bool) {
	width, _ := c.Size()
	expanded := s.table.selected == row
	rowHeight := journalRowHeight
	if len(item.CategoryDigests) > 0 {
		rowHeight = journalRowTall
	}
	// The row follows a panel opening or closing above it on the panel's
	// own curve (rowFollow), so it never snaps past the leaving copy. Its
	// hairline is its own bottom edge rather than a divider drawn between
	// rows, so the line travels with the row as it slides.
	line := ui.Column(c.Key("journal-row-" + item.Day)).Transition(rowFollow())
	if !last {
		line.BorderWidth(0, 0, 1, 0).BorderColor(p.hairline.Alpha(0.7))
	}
	line.Children(func() {
		header := ui.ButtonBase(c.Key("journal-overview-" + item.Day)).Label(item.Day).
			Height(rowHeight).PaddingX(journalInset).Gap(spaceL).AlignItems(ui.Center).Justify(ui.Start).
			FocusRing(false).Disabled(s.busy)
		// A row that is open keeps a little of the accent, and a row the
		// pointer is on keeps a little of the light: one tint at a time,
		// so the eye can tell which is which.
		bg := ui.Transparent
		switch {
		case expanded:
			bg = p.accent.Alpha(0.06)
		case header.Hovered():
			bg = p.hover
		}
		header.Background(header.AnimateColor("bg", bg, hoverFade))
		header.Children(func() {
			ui.Column(c).Width(journalDayWidth).Shrink(0).Gap(spaceHair).Children(func() {
				ui.Text(c, item.Day).FontSize(sizeBody).FontWeight(weightBold).
					FontFeatures("tnum").SingleLine()
				ui.Text(c, item.Weekday).FontSize(sizeSmall).TextColor(p.muted).SingleLine()
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(spaceXS).Children(func() {
				if len(item.CategoryDigests) > 0 {
					categoryDigestOverview(c, item.CategoryDigests)
				} else {
					ui.Text(c, shortenOverview(item.Summary, 160)).FontSize(sizeBody).
						LineHeight(leadMetric).TextColor(p.text).MaxLines(2)
				}
				ui.Textf(c, "%d 条证据 · %d 轮 AI 讨论", item.EventCount, item.ConversationCount).
					FontSize(sizeSmall).FontFeatures("tnum").TextColor(p.muted).SingleLine()
			})
			if journalShowsCounts(width) {
				ui.Column(c).Width(journalCountWidth).Shrink(0).Gap(spaceHair).Children(func() {
					ui.Textf(c, "%d 件事项", item.TopicCount).FontSize(sizeBody).FontWeight(weightBold).
						FontFeatures("tnum").TextColor(p.text).SingleLine()
					ui.Text(c, periodCountLabel(item)).FontSize(sizeSmall).FontFeatures("tnum").
						TextColor(p.muted).SingleLine()
				})
			}
			if width >= 1300 {
				ui.Column(c).Width(journalCatsWidth).Shrink(0).Gap(spaceHair).Children(func() {
					categorySummary(c, item.Categories, 4)
				})
				ui.Column(c).Width(journalRecordWidth).Shrink(0).Gap(spaceHair).Children(func() {
					ui.Textf(c, "完成 %d 项", item.Completed).FontSize(sizeSmall).FontWeight(weightSemibold).
						FontFeatures("tnum").SingleLine()
					ui.Textf(c, "来源 %s", sourceSummary(item.Sources)).FontSize(sizeSmall).
						TextColor(p.muted).SingleLine()
					ui.Text(c, activeSpan(item)).FontSize(sizeSmall).FontFeatures("tnum").
						TextColor(p.muted).SingleLine()
				})
			}
			// One chevron turning through ninety degrees, rather than two
			// icons trading places between frames.
			angle := float32(0)
			if expanded {
				angle = 90
			}
			chevron := ui.Icon(c, iconChevronRight).Size(15, 15).Shrink(0)
			chevron.TextColor(chevron.AnimateColor("fg", p.muted, hoverFade))
			chevron.Rotate(chevron.Animate("rotation", angle, disclosure))
		})
		if header.Clicked() {
			if expanded {
				s.table.selected = -1
			} else {
				s.table.selected = row
			}
		}
		if expanded {
			ui.Column(c.Key("journal-details-" + item.Day)).
				Transition(disclosureMotion()).Children(func() { s.journalDetails(c, item) })
		}
	})
}

// categoryDigestOverview is a day told in as many lines as it had work
// worth keeping. Each line is one line: a category's name, then what
// happened, cut with an ellipsis rather than broken mid-word — a table
// row that wraps is a row that changed height on the reader.
func categoryDigestOverview(c *ui.Context, digests []model.CategoryDigest) {
	p := paletteFor(c.Theme())
	ui.Column(c).Gap(spaceHair).Children(func() {
		for _, digest := range digests {
			color := categoryColor(digest.Category)
			ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(6, 6).Radius(radiusPill).Shrink(0).Background(color)
				ui.Text(c, categoryShort(digest.Category)).FontSize(sizeSmall).FontWeight(weightSemibold).
					TextColor(color).Shrink(0).SingleLine()
				ui.Text(c, digest.Summary).FontSize(sizeSmall).TextColor(p.muted).
					Grow(1).Shrink(1).MinWidth(0).SingleLine()
			})
		}
	})
}

func (s *screen) journalDetails(c *ui.Context, item model.JournalSummary) {
	p := paletteFor(c.Theme())
	// The details belong to the row that opened them, so they wear the
	// row's own tint, a little quieter: one selected block rather than a
	// block and a panel that happen to touch. Nothing inside it is drawn
	// with a border of its own to argue with the sheet.
	ui.Column(c).Padding(spaceL).Gap(spaceL).Background(p.accent.Alpha(0.03)).
		Transition(surfaceFade).Children(func() {
		ui.Row(c).Gap(spaceL).Wrap().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(spaceHair).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, "三时段事项").FontSize(sizeCardTitle).FontWeight(weightBold).LetterSpacing(trackingCard)
				ui.Text(c, "只展示有记录价值的工作与成果；逐轮对话仅作为证据保留。").
					FontSize(sizeMeta).TextColor(p.muted)
			})
			if pillButton(c, "edit-journal-"+item.Day, "编辑日记", iconPencil, true, s.busy).Clicked() {
				s.openJournal(item.Day)
			}
		})
		// The badges count evidence, not topics: saying so here keeps "科研
		// 10" beside three empty time periods from reading as a mistake.
		ui.Text(c, "证据分类计数").FontSize(sizeSmall).FontWeight(weightSemibold).
			LetterSpacing(trackingLabel).TextColor(p.faint)
		ui.Row(c).Wrap().Gap(spaceS).Children(func() {
			for _, category := range item.Categories {
				label := fmt.Sprintf("%s %d", categoryShort(category.Name), category.Count)
				if category.Minutes != nil {
					label += fmt.Sprintf(" · %g 分钟", *category.Minutes)
				}
				categoryBadge(c, category.Name, label, "sheet-category-"+item.Day+"-"+category.Name)
			}
		})
		if len(item.CategoryDigests) > 0 {
			ui.Grid(c).Columns(2).Gap(spaceS).Children(func() {
				for _, digest := range item.CategoryDigests {
					categoryDigestCard(c, digest)
				}
			})
		}
		divider(c)
		if len(item.Topics) == 0 {
			// Three tiles all saying "暂无" looks like a failure; one line
			// saying why is the honest version of the same emptiness.
			ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "时段事项为空：需要 AI 整理后生成。可在「模型设置」配置服务，再运行「整理今日」。").
					FontSize(sizeMeta).TextColor(p.muted).LineHeight(leadBody)
			})
			return
		}
		columns := 3
		if width, _ := c.Size(); width < 1300 {
			columns = 1
		}
		ui.Grid(c).Columns(columns).Gap(spaceM).AlignItems(ui.Start).Children(func() {
			for _, period := range []string{"上午", "下午", "晚上"} {
				s.periodTopics(c, item, period)
			}
		})
	})
}

// categoryDigestCard is one category's worth of a day, told in the color
// that means it everywhere else: the tint says which thread of the day
// this is, and nothing else about it needs to be drawn.
func categoryDigestCard(c *ui.Context, digest model.CategoryDigest) {
	color := categoryColor(digest.Category)
	ui.Column(c).Padding(spaceM).Gap(spaceXS).Radius(radiusTile).Background(color.Alpha(0.08)).
		Transition(surfaceFade).Children(func() {
		ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(7, 7).Radius(radiusPill).Shrink(0).Background(color)
			ui.Textf(c, "%s · %d 项", categoryShort(digest.Category), digest.ItemCount).
				FontSize(sizeSmall).FontWeight(weightSemibold).TextColor(color).
				FontFeatures("tnum").SingleLine()
		})
		ui.Text(c, digest.Summary).FontSize(sizeSmall).LineHeight(leadBody).
			TextColor(paletteFor(c.Theme()).text).MaxLines(2)
	})
}

func (s *screen) periodTopics(c *ui.Context, item model.JournalSummary, name string) {
	p := paletteFor(c.Theme())
	topics := topicsForPeriod(item.Topics, name)
	ui.Column(c).Padding(spaceM).Gap(spaceS).Radius(radiusTile).Background(p.sunken).
		Transition(surfaceFade).Children(func() {
		ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).FontSize(sizeBody).FontWeight(weightBold)
			statusPill(c, fmt.Sprintf("%d 项", len(topics)), toneNeutral, "topic-count-"+item.Day+"-"+name)
		})
		if len(topics) == 0 {
			ui.Text(c, "本时段暂无 AI 事项。").
				FontSize(sizeMeta).LineHeight(leadBody).TextColor(p.muted)
			return
		}
		for _, topic := range topics {
			s.topicCard(c, topic)
		}
	})
}

// topicCard is one piece of work: a line of its category's color at the
// side, then what it was. The line does the categorizing, so the card
// needs no border to say where it ends.
func (s *screen) topicCard(c *ui.Context, topic model.JournalTopic) {
	p := paletteFor(c.Theme())
	ui.Row(c).Radius(radiusRow).Background(p.tile).Clip().
		BorderWidth(0, 0, 0, 3).BorderColor(categoryColor(topic.Category)).
		Transition(surfaceFade).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Padding(spaceM).Gap(spaceXS).Children(func() {
			ui.Text(c, topic.Title).FontSize(sizeBody).FontWeight(weightSemibold).LineHeight(leadMetric)
			ui.Text(c, topic.Summary).FontSize(sizeMeta).LineHeight(leadBody).TextColor(p.text)
			ui.Row(c).Wrap().Gap(spaceS).AlignItems(ui.Center).Children(func() {
				if topic.Category != "" {
					categoryBadge(c, topic.Category, categoryShort(topic.Category), "topic-category-"+topic.ID)
				}
				ui.Textf(c, "%s · %d 条证据", sourceLabel(topic.Source), topic.EventCount).
					FontSize(sizeSmall).FontFeatures("tnum").TextColor(p.muted).SingleLine()
			})
			ui.Textf(c, "%s–%s", displayTime(topic.Start), displayTime(topic.End)).
				FontSize(sizeSmall).FontFeatures("tnum").TextColor(p.faint).SingleLine()
			if len(topic.Evidence) > 0 {
				actionRow(c, func() {
					if pillButton(c, "topic-evidence-"+topic.ID, "查看逐轮证据", iconArrow, false, false).Clicked() {
						s.openEvidence(topic.Evidence[0])
					}
				})
			}
		})
	})
}

// categoryBadge names a category and a count in the color that means it
// throughout the app. Dot and words keep room between them, so the badge
// reads as one word rather than two.
func categoryBadge(c *ui.Context, category, label, key string) {
	color := categoryColor(category)
	ui.Row(c.Key(key)).Height(24).PaddingX(spaceS).Gap(spaceXS).Radius(radiusPill).
		AlignItems(ui.Center).Shrink(0).Background(color.Alpha(0.1)).
		Border(1, color.Alpha(0.24)).Transition(surfaceFade).Children(func() {
		ui.Box(c).Size(6, 6).Radius(radiusPill).Shrink(0).Background(color)
		ui.Text(c, label).FontSize(sizeSmall).FontWeight(weightSemibold).TextColor(color).
			FontFeatures("tnum").SingleLine()
	})
}

func (s *screen) openEvidence(id string) {
	s.perform("读取活动证据…", func() (func(), error) {
		item, err := s.backend.Activity(s.ctx, id)
		return func() {
			if err == nil {
				s.showActivity(item)
			}
		}, err
	})
}

func categorySummary(c *ui.Context, categories []model.JournalCategory, limit int) {
	p := paletteFor(c.Theme())
	shown := 0
	for _, category := range categories {
		if category.Count == 0 {
			continue
		}
		if shown >= limit {
			break
		}
		ui.Row(c).Gap(spaceXS).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(7, 7).Radius(radiusPill).Background(categoryColor(category.Name))
			ui.Textf(c, "%s %d", categoryShort(category.Name), category.Count).
				FontSize(sizeSmall).FontFeatures("tnum").TextColor(p.muted).SingleLine()
		})
		shown++
	}
	if shown == 0 {
		ui.Text(c, "暂无分类活动").FontSize(sizeSmall).TextColor(p.faint)
	}
}

func topicsForPeriod(topics []model.JournalTopic, period string) []model.JournalTopic {
	result := make([]model.JournalTopic, 0)
	for _, topic := range topics {
		if topic.Period == period {
			result = append(result, topic)
		}
	}
	return result
}

func periodCountLabel(item model.JournalSummary) string {
	parts := []string{}
	for _, period := range []string{"上午", "下午", "晚上"} {
		if count := len(topicsForPeriod(item.Topics, period)); count > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", period, count))
		}
	}
	if len(parts) == 0 {
		return "暂无事项"
	}
	return strings.Join(parts, " · ")
}

func activeSpan(item model.JournalSummary) string {
	if item.ActiveStart == "" {
		return "时间 · 无记录"
	}
	start, end := displayTime(item.ActiveStart), displayTime(item.ActiveEnd)
	start = strings.TrimPrefix(start, item.Day+" ")
	end = strings.TrimPrefix(end, item.Day+" ")
	return "时间 " + start + "–" + end
}

func sourceSummary(sources []string) string {
	if len(sources) == 0 {
		return "—"
	}
	if len(sources) > 2 {
		return sourceLabel(sources[0]) + "、" + sourceLabel(sources[1]) + fmt.Sprintf(" 等 %d 个", len(sources))
	}
	labels := make([]string, len(sources))
	for i, source := range sources {
		labels[i] = sourceLabel(source)
	}
	return strings.Join(labels, "、")
}

func sourceLabel(source string) string {
	names := map[string]string{
		"codex": "Codex", "chatgpt": "ChatGPT", "todoist": "Todoist",
		"zotero": "Zotero", "calendar": "Google Calendar",
	}
	parts := strings.Split(source, "、")
	for i, part := range parts {
		if label := names[part]; label != "" {
			parts[i] = label
		}
	}
	return strings.Join(parts, "、")
}

func shortenOverview(value string, size int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > size {
		return string(runes[:size-1]) + "…"
	}
	return value
}

func (s *screen) activityTable(c *ui.Context) {
	t := &s.table
	p := paletteFor(c.Theme())
	pageHeader(c, "ACTIVITY", "活动流水", "检索原始活动与分类证据；双击行查看完整内容。")
	toolbar(c, func() {
		date := ui.TextInput(c.Key("date"), &t.query.Day).Placeholder("YYYY-MM-DD").Label("日期").Width(132)
		ui.TextInput(c.Key("keyword"), &t.query.Keyword).Placeholder("搜索关键词").Label("关键词").Width(170)
		ui.TextInput(c.Key("source"), &t.query.Source).Placeholder("来源标识").Label("来源").Width(112)
		ui.Select(c.Key("category"), &t.query.Category, s.categoryChoices()).Width(132).Label("活动分类")
		if pillButton(c, "query", "查询", iconSearch, true, s.busy).Clicked() || date.Submitted() && !s.busy {
			filter := t.query
			filter.Offset = 0
			s.loadTable("activity", filter)
		}
	})
	if t.detail && t.selected >= 0 && t.selected < len(t.activities.Rows) {
		// Reading one record and going back to the list: the way back
		// rides with the page's name, where a reader looks for it.
		pageHeaderWith(c, "ACTIVITY", "活动流水", "检索原始活动与分类证据；双击行查看完整内容。", func() {
			if pillButton(c, "back-table", "返回表格", nil, false, false).Clicked() {
				t.detail = false
			}
		})
		ui.Scroll(c.Key("evidence-scroll")).Grow(1).MinHeight(0).Children(func() {
			s.evidenceCard(c, t.activities.Rows[t.selected])
		})
		return
	}
	showSelected := func() {
		if t.selected >= 0 && t.selected < len(t.activities.Rows) {
			t.detail = true
		}
	}
	edgeLit(ui.Column(c).Grow(1).MinHeight(0).Radius(radiusSheet).Border(1, p.hairline).
		Background(p.surface), p).
		Transition(surfaceFade).Children(func() {
		if s.busy && !t.activityLoaded {
			activitySkeletonRows(c)
			return
		}
		if len(t.activities.Rows) == 0 {
			emptyState(c, iconSearch, "暂无匹配记录", "调整日期、分类、来源或关键词后再试。")
			return
		}
		t.state.Selected = &t.selected
		t.state.Key = func(i int) any { return t.activities.Rows[i].ID }
		t.state.RoundRows = false
		t.state.HeaderCell = func(cell ui.Element, _ int) {
			cell.Background(p.raised)
		}
		table := ui.Table(c, &t.state, activityColumns, len(t.activities.Rows), func(row, col int) {
			item := t.activities.Rows[row]
			sheetCell(c, row, func() {
				switch col {
				case 0:
					ui.Text(c, item.Title).FontSize(sizeBody).LineHeight(leadMetric).MaxLines(3)
				case 1:
					ui.Text(c, item.Source).FontSize(sizeMeta).TextColor(p.muted).SingleLine()
				case 2:
					ui.Text(c, item.Category).FontSize(sizeMeta).
						TextColor(categoryColor(item.Category)).SingleLine()
				case 3:
					ui.Text(c, displayTime(item.Timestamp)).FontSize(sizeMeta).FontFeatures("tnum").
						TextColor(p.muted).SingleLine()
				}
			})
		}).Grow(1)
		if table.Submitted() && !s.busy {
			showSelected()
		}
	})
	s.tableFooter(c, "activity", len(t.activities.Rows), t.activities.More, showSelected)
}

func (s *screen) tableFooter(c *ui.Context, page string, rows int, more bool, showSelected func()) {
	t := &s.table
	p := paletteFor(c.Theme())
	// The foot of a sheet says where in it you are, and offers the ways on.
	// The place is told first and quietly; the ways are what to press.
	// The foot keeps its own height: a bar that was squeezed by the sheet
	// above it would move its buttons out from under the pointer.
	edgeLit(ui.Row(c).Padding(spaceS, spaceM).Wrap().Gap(spaceS).AlignItems(ui.Center).Shrink(0).
		Radius(radiusCard).Border(1, p.hairline).Background(p.surface), p).
		Transition(surfaceFade).Children(func() {
		ui.Textf(c, "第 %d 页", t.applied.Offset/workspace.PageSize+1).
			FontSize(sizeMeta).FontFeatures("tnum").TextColor(p.muted)
		ui.Box(c).Width(spaceS)
		if pillButton(c, "prev", "上一页", nil, false, s.busy || t.applied.Offset == 0).Clicked() {
			filter := t.applied
			filter.Offset -= workspace.PageSize
			s.loadTable(page, filter)
		}
		if pillButton(c, "next", "下一页", nil, false, s.busy || !more).Clicked() {
			filter := t.applied
			filter.Offset += workspace.PageSize
			s.loadTable(page, filter)
		}
		label := "最早优先"
		if t.applied.Ascending {
			label = "最新优先"
		}
		if pillButton(c, "sort", label, nil, false, s.busy).Clicked() {
			filter := t.applied
			filter.Ascending, filter.Offset = !filter.Ascending, 0
			s.loadTable(page, filter)
		}
		// Where in the sheet you are, and how to leave it, are told at
		// opposite ends of the same line: the page on the left, the way
		// into a record on the right, with the room between them empty.
		ui.Box(c).Grow(1).Shrink(1)
		if pillButton(c, "open-selected", "查看所选记录", iconArrow, false,
			s.busy || t.selected < 0 || t.selected >= rows).Clicked() {
			showSelected()
		}
	})
}

func categoryLegend(c *ui.Context, categories []string) {
	p := paletteFor(c.Theme())
	ui.Row(c).Wrap().Gap(spaceM).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "活动分类").FontSize(sizeSmall).FontWeight(weightSemibold).
			LetterSpacing(trackingLabel).TextColor(p.faint)
		for _, category := range categories {
			ui.Row(c).Gap(spaceXS).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(7, 7).Radius(radiusPill).Shrink(0).Background(categoryColor(category))
				ui.Text(c, categoryShort(category)).FontSize(sizeSmall).TextColor(p.muted)
			})
		}
		ui.Text(c, "分钟数留空表示暂无可靠时长证据。").FontSize(sizeSmall).TextColor(p.faint)
	})
}

// sheetCell is one cell of the activity sheet. It carries no stripe of its
// own: a table gives every column its own track, so a background painted
// cell by cell breaks into as many blocks as there are columns and reads
// as a grid of boxes rather than one list. Rows are told apart by where
// their words sit, and chosen rows by the list's own highlight.
func sheetCell(c *ui.Context, row int, body func()) {
	// A cell fills the row it is given and centers what it has to say:
	// one line of a title sits at the same height as the three lines
	// beside it, rather than hanging from the top of the row.
	ui.Column(c).Height(activityRowHeight).FillWidth().FillHeight().
		Padding(spaceS, spaceM).Gap(spaceHair).
		Justify(ui.Center).Clip().Children(body)
}

func periodCell(c *ui.Context, value string) {
	p := paletteFor(c.Theme())
	value = strings.TrimSpace(value)
	if value == "" {
		ui.Text(c, "—").FontSize(sizeMeta).TextColor(p.faint)
		return
	}
	runes := []rune(value)
	if len(runes) > 220 {
		value = string(runes[:219]) + "…"
	}
	color := p.text
	if strings.HasPrefix(value, "暂无可核实的活动记录") {
		color = p.muted
	}
	ui.Text(c, value).FontSize(sizeMeta).LineHeight(leadBody).TextColor(color).MaxLines(4)
}

func categoryMinutes(item model.JournalSummary, category string) *float64 {
	for _, entry := range item.Categories {
		if entry.Name == category {
			return entry.Minutes
		}
	}
	return nil
}

func minutesCell(c *ui.Context, minutes *float64) {
	p := paletteFor(c.Theme())
	value := "—"
	color := p.faint
	if minutes != nil {
		value, color = fmt.Sprintf("%g", *minutes), p.text
	}
	ui.Text(c, value).FontSize(sizeMeta).FontWeight(weightSemibold).FontFeatures("tnum").
		TextColor(color).TextAlign(ui.End).FillWidth()
}

const allCategories = "全部类别"

var activityColumns = []ui.TableColumn{
	{Title: "活动", ID: "activity", Width: 340},
	{Title: "来源", ID: "source", Width: 112},
	{Title: "类别", ID: "category", Width: 168},
	{Title: "时间", ID: "time", Width: 150},
}
