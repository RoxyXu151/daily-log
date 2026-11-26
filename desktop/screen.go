package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"daily-log/internal/mobileapi"
	"daily-log/internal/model"
	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

type screen struct {
	backend            workspace.Backend
	ctx                context.Context
	dispatch           func(func())
	busy               bool
	jobRunning         bool
	jobMessage         string
	jobPhase           string
	jobFailed          bool
	message            string
	failed             bool
	overview           workspace.Overview
	current            workspace.Journal
	fields             model.JournalText
	saved              model.JournalText
	page               string
	toast              string
	overviewLoaded     bool
	panel              string
	editorSide         string
	table              tableView
	appearance         appearance
	saveAppearance     func(appearance) error
	remote             bool
	host               string
	disconnect         func()
	reconnect          func()
	connectionLost     bool
	conflict           model.JournalView
	categories         []workspace.Category
	categoryEdits      []categoryEdit
	categoryDraft      string
	categoryDraftShort string
	connections        []workspace.Connection
	connection         connectionForm
	addCatalog         bool
	models             workspace.ModelSettings
	profile            profileForm
	modelChoices       []string
	scheduling         scheduleForm
	mobileOptions      mobileAccessOptions
	mobileSession      *mobileAccessSession
	startMobile        func(mobileAccessOptions) (*mobileAccessSession, error)
	pickMobilePath     func(string, bool) (string, error)
	openLink           func(string) error
	activityDetail     workspace.Evidence
	activityBack       string
}

// notify lets a finished action say so with a toast that fades away, in
// place of a banner that would sit at the top of every page: mygo-music's
// way of confirming what just happened.
func (s *screen) notify(message string) { s.toast = message }

func (s *screen) perform(label string, work func() (func(), error)) {
	if s.busy {
		return
	}
	s.busy, s.failed, s.message = true, false, label
	go func() {
		update, err := work()
		s.dispatch(func() {
			s.busy = false
			s.failed = err != nil
			s.connectionLost = errors.Is(err, mobileapi.ErrDisconnected) || errors.Is(err, mobileapi.ErrSession)
			if err != nil {
				s.message = err.Error()
			} else {
				s.message = ""
			}
			if update != nil {
				update()
			}
		})
	}()
}

func (s *screen) refresh() {
	s.perform("读取日志数据…", func() (func(), error) {
		overview, err := s.backend.Overview(s.ctx)
		if err != nil {
			return nil, err
		}
		categories, err := s.backend.Categories(s.ctx)
		if err != nil {
			return nil, err
		}
		return func() {
			s.overview = overview
			s.applyCategories(categories)
			s.overviewLoaded = true
		}, nil
	})
}

func (s *screen) run(label string, operation func(context.Context) error) {
	if s.busy || s.jobRunning {
		return
	}
	s.jobRunning, s.jobFailed, s.jobMessage = true, false, label
	s.jobPhase = ""
	poll, stop := context.WithCancel(s.ctx)
	go s.watchJob(poll)
	go func() {
		err := operation(s.ctx)
		stop()
		overview, refreshErr := s.backend.Overview(s.ctx)
		s.dispatch(func() {
			s.jobRunning = false
			s.jobPhase = ""
			if refreshErr == nil {
				s.overview = overview
				s.overviewLoaded = true
			}
			if err == nil {
				err = refreshErr
			}
			s.jobFailed = err != nil
			if err != nil {
				s.jobMessage = err.Error()
				s.connectionLost = errors.Is(err, mobileapi.ErrDisconnected) || errors.Is(err, mobileapi.ErrSession)
			} else {
				s.jobMessage = ""
				s.notify("后台任务已完成")
			}
		})
	}()
}

func (s *screen) watchJob(ctx context.Context) {
	tick := time.NewTicker(1500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			overview, err := s.backend.Overview(ctx)
			if err == nil {
				s.dispatch(func() {
					if ctx.Err() == nil {
						s.jobPhase = jobPhase(overview.JobStatus)
					}
				})
			}
		}
	}
}

func (s *screen) view(c *ui.Context) {
	s.appearance.apply(c)
	t := c.Theme()
	width, height := c.Size()
	// A toast says what just finished, then fades: confirmations never
	// stack up over the pages the way mygo-music keeps its pages clear.
	if s.toast != "" {
		c.Toast(s.toast)
		s.toast = ""
	}
	inset := float32(0)
	if width >= 720 {
		inset = sidebarWidth
	}
	toastBand(c, inset)
	if width < 720 {
		root := ui.Column(c).Size(width, height).Background(t.Background)
		root.Background(root.AnimateColor("bg", t.Background, themeFade))
		root.Children(func() {
			bar := ui.Row(c).Padding(spaceS, spaceM).Gap(spaceM).AlignItems(ui.Center).Background(paletteFor(t).sidebar)
			bar.Background(bar.AnimateColor("bg", paletteFor(t).sidebar, themeFade))
			bar.Children(func() {
				ui.Image(c, dailyLogLogo).Size(32, 32).Shrink(0)
				ui.Column(c).Gap(0).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					ui.Text(c, "daily-log").FontSize(sizeField).FontWeight(weightBold).SingleLine()
					ui.Text(c, "个人工作台").FontSize(sizeSmall).TextColor(t.TextMuted).SingleLine()
				})
				if s.remote {
					statusPill(c, "远程", toneAccent, "remote-status")
				}
			})
			nav := ui.Row(c).Padding(spaceXS, spaceM, spaceS, spaceM).Gap(spaceXS).Wrap().
				Background(paletteFor(t).sidebar)
			nav.Background(nav.AnimateColor("bg", paletteFor(t).sidebar, themeFade))
			nav.Children(func() {
				s.compactNavigation(c)
			})
			s.content(c, spaceM)
		})
		return
	}
	root := ui.Row(c).Size(width, height).AlignItems(ui.Stretch).Background(t.Background)
	root.Background(root.AnimateColor("bg", t.Background, themeFade))
	root.Children(func() {
		p := paletteFor(t)
		side := ui.Column(c).Width(sidebarWidth).FillHeight().Shrink(0).
			Padding(spaceL, spaceM, spaceM, spaceM).Gap(spaceXS).Background(p.sidebar)
		side.Background(side.AnimateColor("bg", p.sidebar, themeFade))
		side.Children(func() {
			ui.Row(c).Padding(spaceXS, spaceS, spaceL, spaceS).Gap(spaceM).AlignItems(ui.Center).Children(func() {
				ui.Image(c, dailyLogLogo).Size(34, 34).Shrink(0)
				ui.Column(c).Gap(0).Shrink(1).MinWidth(0).Children(func() {
					ui.Text(c, "daily-log").FontSize(sizeCardTitle).FontWeight(weightBold).LetterSpacing(trackingCard).SingleLine()
					ui.Text(c, "个人工作台").FontSize(sizeSmall).TextColor(p.muted).SingleLine()
				})
			})
			divider(c)
			// The nav breathes by the same room as its buttons, so that a
			// label and the row under it begin at one place.
			ui.Scroll(c.Key("sidebar-nav")).Grow(1).MinHeight(0).Children(func() {
				s.navigation(c)
			})
			if s.remote {
				divider(c)
				ui.Column(c).Gap(spaceXS).Padding(spaceS).Children(func() {
					ui.Text(c, "采集主机").FontSize(sizeSmall).FontWeight(weightSemibold).
						LetterSpacing(trackingLabel).TextColor(p.faint)
					ui.Text(c, s.host).FontSize(sizeMeta).TextColor(p.muted).SingleLine()
				})
				disconnect := choicePill(c, "disconnect", "断开主机", false,
					s.busy || s.jobRunning || s.fields != s.saved)
				if disconnect.Clicked() {
					s.disconnect()
				}
			}
		})
		ui.Box(c).Width(1).FillHeight().Background(p.hairline)
		s.content(c, pagePadding)
	})
}

func (s *screen) navigation(c *ui.Context) {
	ui.Column(c).Gap(spaceHair).Children(func() {
		sectionLabel(c, "工作台")
		s.nav(c, "", "工作台")

		sectionLabel(c, "记录")
		s.nav(c, "days", "日记列表")

		sectionLabel(c, "设置")
		s.nav(c, "categories", "分类管理")
		s.nav(c, "connections", "来源管理")
		s.nav(c, "models", "模型设置")
		s.nav(c, "schedule", "日志更新")
		s.nav(c, "appearance", "外观")
		if s.startMobile != nil {
			s.nav(c, "mobile", "手机配对")
		}
	})
}

func (s *screen) compactNavigation(c *ui.Context) {
	items := []struct {
		page  string
		label string
	}{{"", "工作台"}}
	items = append(items,
		struct{ page, label string }{"days", "日记列表"},
		struct{ page, label string }{"categories", "分类管理"},
		struct{ page, label string }{"connections", "来源管理"},
		struct{ page, label string }{"models", "模型设置"},
		struct{ page, label string }{"schedule", "日志更新"},
		struct{ page, label string }{"appearance", "外观"},
	)
	if s.startMobile != nil {
		items = append(items, struct{ page, label string }{"mobile", "手机配对"})
	}
	for _, item := range items {
		b := choicePill(c, "nav-"+item.page, item.label, item.page == s.page, s.busy)
		if b.Clicked() {
			s.openPage(item.page)
		}
	}
}

func (s *screen) content(c *ui.Context, padding float32) {
	top, bottom, side := spaceXL, spaceXL, padding
	if padding < pagePadding {
		top, bottom, side = spaceS, spaceS, padding
	}
	// The page and its notices share one room. The page fills it; the
	// notices float over it at its foot, so that nothing said about
	// loading or failing pushes the work down the way a banner dropped
	// into the column does.
	ui.Box(c).Grow(1).MinWidth(0).MinHeight(0).Padding(top, side, bottom, side).Children(func() {
		ui.Column(c.Key("page-" + s.page)).Fill().Gap(spaceL).Children(func() {
			switch s.page {
			case "editor":
				s.editor(c)
			case "days", "activity":
				// The two sheets share one screen: which of them is asked
				// for decides which is built.
				s.tableScreen(c)
			case "appearance":
				s.appearanceScreen(c)
			case "connections":
				s.connectionsScreen(c)
			case "categories":
				s.categoriesScreen(c)
			case "models":
				s.modelsScreen(c)
			case "schedule":
				s.scheduleScreen(c)
			case "mobile":
				s.mobileAccess(c)
			case "activity-detail":
				s.activityScreen(c)
			default:
				s.dashboard(c)
			}
		}).Transition(pageMotion())
		s.floats(c)
	})
}

// floats is what the app says from above a page: what it is waiting for,
// and what it has to report. Both come and go at the foot of the page, to
// the side, where the words of the page itself are not, and both are said
// at once when there is both to say — a failure that arrived while a task
// was running is not a failure the app gets to keep to itself until the
// task is done.
func (s *screen) floats(c *ui.Context) {
	message, working := s.workingText()
	report, failed := s.reportText()
	if !working && report == "" {
		return
	}
	stack := ui.Column(c.Key("floats")).Absolute().Bottom(spaceM).Right(spaceM).Gap(spaceS).
		AlignItems(ui.End)
	// A thing that is only waiting offers nothing to press, so it lets the
	// pointer through: a status that sat on top of a button would otherwise
	// swallow the click meant for the button. A report has a close button,
	// so the foot of the page is its own from then.
	stack.IgnoreInput(report == "")
	stack.Children(func() {
		if report != "" {
			noticeFloat(c, report, failed, s.dismissNotice)
			if s.connectionLost && s.reconnect != nil {
				ui.Row(c.Key("reconnect")).Gap(spaceS).Wrap().AlignItems(ui.Center).
					Transition(popMotion()).Children(func() {
					if pillButton(c, "retry", "重试连接", nil, false, s.busy).Clicked() {
						s.refresh()
					}
					if pillButton(c, "reconnect", "重新配对", iconRefresh, false, s.busy).Clicked() {
						s.reconnect()
					}
				})
			}
		}
		if working {
			workingFloat(c, message)
		}
	})
}

// workingFloat is the small card that says what the app is waiting for.
func workingFloat(c *ui.Context, message string) {
	p := paletteFor(c.Theme())
	row := ui.Row(c.Key("working")).IgnoreInput(true).
		Padding(spaceM, spaceL).Gap(spaceS).AlignItems(ui.Center).Shrink(0).
		Radius(radiusCard).Background(p.raised).Border(1, p.hairline).
		Shadow(0, 8, 24, -4, p.shade.Alpha(0.6)).
		Transition(popMotion())
	row.Children(func() {
		spinnerBox(c, p.accent)
		txt := ui.Text(c, message).FontSize(sizeBody).FontWeight(weightMedium).SingleLine()
		txt.TextColor(txt.AnimateColor("fg", p.muted, hoverFade))
	})
}

// workingText is what the page is waiting for, if anything.
func (s *screen) workingText() (string, bool) {
	switch {
	case s.busy && s.message != "":
		return s.message, true
	case s.jobRunning:
		message := s.jobMessage
		if s.jobPhase != "" {
			message += " · " + s.jobPhase
		}
		if message == "" {
			message = "后台任务正在运行"
		}
		return message, true
	}
	return "", false
}

// reportText is the thing worth saying about what already happened: a
// failure to be dealt with, or the last thing a page reported. What a page
// has to say is said whether or not it is bad news — a thing that stopped,
// a thing that saved — and only the color of the dot knows which it was.
func (s *screen) reportText() (string, bool) {
	switch {
	case s.failed && s.message != "":
		return s.message, true
	case s.jobFailed && s.jobMessage != "":
		return s.jobMessage, true
	case !s.busy && s.message != "":
		return s.message, false
	case !s.jobRunning && s.jobMessage != "":
		return s.jobMessage, false
	}
	return "", false
}

// dismissNotice lets a failure be put away without waiting for the next
// action to overwrite it.
func (s *screen) dismissNotice() {
	s.message, s.failed = "", false
	s.jobMessage, s.jobFailed = "", false
}

func (s *screen) nav(c *ui.Context, page, label string) {
	button := navButton(c, "nav-"+page, label, pageIcon(page), page == s.page, s.busy)
	if button.Clicked() {
		s.openPage(page)
	}
}

func (s *screen) openPage(page string) {
	if page == s.page {
		return
	}
	switch page {
	case "days":
		s.loadTable(page, model.BrowseQuery{})
	case "connections":
		s.openConnections()
	case "categories":
		s.openCategories()
	case "models":
		s.openModels()
	case "schedule":
		s.openSchedule()
	default:
		s.page = page
	}
}

func pageIcon(page string) *ui.SVG {
	switch page {
	case "days":
		return iconBook
	case "connections":
		return iconPlug
	case "categories":
		return iconCategories
	case "models":
		return iconSpark
	case "schedule":
		return iconClock
	case "appearance":
		return iconPalette
	case "mobile":
		return iconPhone
	case "editor":
		return iconPencil
	default:
		return iconHome
	}
}

func (s *screen) dashboard(c *ui.Context) {
	p := paletteFor(c.Theme())
	subtitle := "采集今天的活动，整理成值得回看的日记。"
	if s.remote {
		subtitle = "采集主机 · " + s.host
	}
	// What the page can do sits with its name, not at the end of what it
	// wrote: a page should not have to be scrolled to be refreshed.
	pageHeaderWith(c, "TODAY", "工作台", subtitle, func() {
		if pillButton(c, "refresh", "刷新", iconRefresh, false, s.busy).Clicked() {
			s.refresh()
		}
	})
	if s.busy && !s.overviewLoaded {
		// The first overview: blocks in the shape of the cards, which the
		// cards then take the place of without moving anything.
		ui.Scroll(c.Key("dashboard-scroll")).Grow(1).MinHeight(0).Children(func() { dashboardSkeleton(c) })
		return
	}
	ui.Scroll(c.Key("dashboard-scroll")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(spaceL).Children(func() {
			summary := s.overview.TodaySummary
			// The day's card is a composition, not a stack: at room enough
			// the day and the way into it hold the left and the counts hold
			// the right, the way mygo-music's feature card lays its cover
			// beside its words. Narrower, they take turns.
			day := ui.Column(c.Key("today")).Padding(spaceXL).Gap(spaceL).Radius(radiusCard).
				Border(1, p.hairline).Background(p.surface)
			edgeLit(day, p).Transition(riseMotion(0)).Children(func() {
				ui.Row(c).Gap(spaceXXL).Wrap().AlignItems(ui.Center).Children(func() {
					// The day is the one line on this page that may not be
					// broken in half: it keeps the width it measures, and
					// the counts give way and go below when the window
					// cannot hold both.
					ui.Column(c).Gap(spaceXS).Grow(1).Shrink(1).
						MinWidth(max(textWidth(c, s.overview.Today, sizeHero, weightBold), widthDate) + spaceXS).
						Children(func() {
							ui.Text(c, "今日日记").FontSize(sizeCardTitle).FontWeight(weightBold).LetterSpacing(trackingCard)
							ui.Text(c, s.overview.Today).FontSize(sizeHero).FontWeight(weightBold).
								LetterSpacing(trackingFigure).FontFeatures("tnum").SingleLine()
							ui.Textf(c, "%d 件事项 · %d 条原始证据 · 完成 %d 项任务",
								summary.TopicCount, summary.EventCount, summary.Completed).
								FontSize(sizeMeta).TextColor(p.muted)
							actionRow(c, func() {
								if pillButton(c, "open-today", "打开今日日记", iconArrow, true, s.busy || s.overview.Today == "").Clicked() {
									s.openJournal(s.overview.Today)
								}
							})
						})
					// Two tiles side by side is the least that still reads
					// as figures; below that room the whole block moves
					// under the day rather than crushing its own words.
					ui.Column(c).Grow(2).Shrink(1).MinWidth(widthMetric*2 + spaceS).Children(func() {
						columns := dashboardColumns(c, 4, 1000, 560)
						ui.Grid(c).Columns(columns).Gap(spaceS).Children(func() {
							metricTile(c, "今日事项", fmt.Sprintf("%d", summary.TopicCount), "AI 归纳的有效工作")
							metricTile(c, "原始证据", fmt.Sprintf("%d", summary.EventCount), "用于追溯的事项来源")
							metricTile(c, "完成任务", fmt.Sprintf("%d", summary.Completed), "有完成证据的任务")
							metricTile(c, "采集来源", fmt.Sprintf("%d", len(s.overview.Sources)), "已配置来源")
						})
					})
				})
				// The day's own hours close the card: the counts above say
				// how much there was, this says when it happened.
				divider(c)
				dayRibbon(c, summary)
			})
			// Cards of a row start where the row starts, rather than being
			// stretched to the height of whichever one has most to say.
			// A window wide enough for three cards spends its room on the
			// third rather than on the space between a card's words and
			// its buttons.
			columns := dashboardRow(c)
			ui.Grid(c).Columns(columns).Gap(spaceL).AlignItems(ui.Start).Children(func() {
				s.jobCard(c)
				s.sourceCard(c)
				// Two cards wide, the outputs keep the whole width below
				// them: a row of its own is what its three lines of
				// buttons are drawn for.
				s.outputCard(c.Key("outputs"), columns)
			})
		})
	})
}

// dashboardColumns is how many cards wide a row is: the window asked at
// two places, giving up a column at each, down to the two that still read
// as figures rather than four stacked into a page of nothing but numbers.
func dashboardColumns(c *ui.Context, wide, first, second int) int {
	columns := wide
	if width, _ := c.Size(); width < float32(first) {
		columns = 2
	}
	if width, _ := c.Size(); width < float32(second) {
		columns = secondColumn(wide)
	}
	return columns
}

// dashboardRow is how many cards wide the row under the day is. Two cards
// is the shape the page is drawn for; a third joins them only once the
// window can give each of the three the width a card needs to say what it
// says on one line — the point is not to fill the window, but to stop one
// card from being mostly space between its words and its buttons.
func dashboardRow(c *ui.Context) int {
	width, _ := c.Size()
	switch {
	case width < 720:
		return 1
	case width >= 1520:
		return 3
	default:
		return 2
	}
}

// dayRibbon lays a day along its own hours: one block for each AI item,
// as wide as the time it took, in the color its category means
// everywhere else. It says what a list of times cannot — where the day
// was full, where it was thin, and what the fullness was made of. The
// well between the blocks is the idle time, so the ribbon is drawn from
// the evidence and nothing about it is decoration: without two items
// carrying both ends of a time, it has no right to appear at all.
func dayRibbon(c *ui.Context, summary model.JournalSummary) {
	p := paletteFor(c.Theme())
	type span struct {
		start, end time.Time
		category   string
	}
	var spans []span
	cursor := time.Time{}
	for _, topic := range summary.Topics {
		start, err := model.ParseTime(topic.Start, model.Shanghai)
		if err != nil {
			continue
		}
		end, err := model.ParseTime(topic.End, model.Shanghai)
		if err != nil || !end.After(start) {
			continue
		}
		if !cursor.IsZero() && start.Before(cursor) {
			start = cursor
		}
		if end.Before(start) {
			continue
		}
		spans = append(spans, span{start, end, topic.Category})
		cursor = end
	}
	if len(spans) < 2 {
		return
	}
	first, last := spans[0].start, spans[len(spans)-1].end
	total := last.Sub(first).Minutes()
	if total <= 0 {
		return
	}
	ui.Column(c).Gap(spaceS).Children(func() {
		ui.Row(c).Gap(spaceM).AlignItems(ui.Center).Children(func() {
			ui.Text(c, first.Format("15:04")).FontSize(sizeSmall).FontFeatures("tnum").
				TextColor(p.faint).Shrink(0)
			track := ui.Row(c.Key("day-ribbon")).Grow(1).MinWidth(0).Height(6).
				Radius(radiusPill).Clip().AlignItems(ui.Center)
			track.Background(track.AnimateColor("bg", p.sunken, themeFade))
			track.Children(func() {
				at := first
				for _, item := range spans {
					if gap := item.start.Sub(at).Minutes(); gap > 0.5 {
						ui.Box(c).Grow(float32(gap)).Height(6)
					}
					color := categoryColor(item.category)
					ui.Box(c).Grow(float32(item.end.Sub(item.start).Minutes())).Height(6).
						MinWidth(3).Background(color).Transition(surfaceFade)
					at = item.end
				}
				if tail := last.Sub(at).Minutes(); tail > 0.5 {
					ui.Box(c).Grow(float32(tail)).Height(6)
				}
			})
			ui.Text(c, last.Format("15:04")).FontSize(sizeSmall).FontFeatures("tnum").
				TextColor(p.faint).Shrink(0)
		})
		// The colors need their names, or the ribbon is a pattern rather
		// than a statement. Only the categories the day actually holds.
		ui.Row(c).Wrap().Gap(spaceM).AlignItems(ui.Center).Children(func() {
			seen := map[string]bool{}
			for _, item := range spans {
				if item.category == "" || seen[item.category] {
					continue
				}
				seen[item.category] = true
				ui.Row(c).Gap(spaceXS).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(7, 7).Radius(radiusPill).Shrink(0).
						Background(categoryColor(item.category))
					ui.Text(c, categoryShort(item.category)).FontSize(sizeSmall).TextColor(p.muted)
				})
			}
		})
	})
}

func (s *screen) jobCard(c *ui.Context) {
	p := paletteFor(c.Theme())
	tone, label := toneGood, "最近一次已完成"
	if s.jobRunning {
		tone, label = toneAccent, "正在运行"
	} else if s.overview.JobStatus == "error" {
		tone, label = toneDanger, "最近一次失败"
	}
	last := "尚未生成"
	if s.overview.LastRun != "" {
		last = displayTime(s.overview.LastRun)
	}
	cardAt(c.Key("job-card"), 1, "运行任务", func() {
		// At room enough the facts hold the left and the ways on hold the
		// right, so a card is not a column of words with a desert beside it.
		wide, _ := c.Size()
		ui.Row(c).Gap(spaceXXL).Wrap().AlignItems(ui.Start).Children(func() {
			// A time is said on one line or not at all: the facts keep the
			// width one needs, and the buttons below it take a line of
			// their own when the card cannot hold both.
			ui.Column(c).Gap(spaceS).Grow(1).Shrink(1).MinWidth(widthFacts).Children(func() {
				factRow(c, "状态", func() { statusPill(c, label, tone, "job-status") })
				factRow(c, "上次整理", func() { figure(c, last) })
				if s.overview.NextRun != "" {
					factRow(c, "下次预计", func() { figure(c, displayTime(s.overview.NextRun)) })
				}
				if !s.jobRunning {
					switch s.overview.JobStatus {
					case "error":
						ui.Text(c, "最近一次任务失败 · "+s.overview.JobError).FontSize(sizeMeta).
							TextColor(c.Theme().Danger).LineHeight(leadBody)
					case "success":
						ui.Text(c, "最近一次任务已完成").FontSize(sizeMeta).TextColor(p.muted)
					default:
						if phase := jobPhase(s.overview.JobStatus); phase != "" {
							ui.Text(c, "最近记录 · "+phase).FontSize(sizeMeta).TextColor(p.muted)
						}
					}
				}
			})
			if wide >= 900 {
				ui.Column(c).Gap(spaceS).Shrink(0).Children(func() {
					if pillButton(c, "run", "整理今日", iconSpark, true, s.busy || s.jobRunning).Clicked() {
						s.run("正在整理今日日记…", s.backend.Run)
					}
					if pillButton(c, "run-week", "整理最近一周", nil, false, s.busy || s.jobRunning).Clicked() {
						s.run("正在整理最近一周…", func(ctx context.Context) error {
							return s.backend.Regenerate(ctx, 7)
						})
					}
				})
			} else {
				actionRow(c, func() {
					if pillButton(c, "run", "整理今日", iconSpark, true, s.busy || s.jobRunning).Clicked() {
						s.run("正在整理今日日记…", s.backend.Run)
					}
					if pillButton(c, "run-week", "整理最近一周", nil, false, s.busy || s.jobRunning).Clicked() {
						s.run("正在整理最近一周…", func(ctx context.Context) error {
							return s.backend.Regenerate(ctx, 7)
						})
					}
				})
			}
		})
	})
}

func (s *screen) sourceCard(c *ui.Context) {
	cardAt(c.Key("sources-card"), 2, "采集来源", func() {
		for _, item := range s.overview.Sources {
			status := "已停用"
			if item.Enabled {
				status = sourceStatus(item.Status)
			}
			ui.Row(c.Key(item.Name)).Gap(spaceM).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.Name).FontSize(sizeBody).FontWeight(weightMedium).
					Grow(1).Shrink(1).MinWidth(0).SingleLine()
				tone := toneNeutral
				if item.Enabled && item.Status == "success" {
					tone = toneGood
				} else if item.Enabled && item.Status == "error" {
					tone = toneDanger
				}
				statusPill(c, status, tone, item.Name+"-status")
			})
		}
		actionRow(c, func() {
			if pillButton(c, "open-connections", "打开来源管理", nil, false, s.busy).Clicked() {
				s.openConnections()
			}
		})
	})
}

// secondColumn is what a row of two becomes when the window runs out: a
// row of one for cards, which read stacked, and still two for figures,
// which do not.
func secondColumn(wide int) int {
	if wide > 2 {
		return 2
	}
	return 1
}

// factRow is one line of a card that reports: what is being said of, and
// the saying. Labels line up, so the card can be read down its side.
func factRow(c *ui.Context, label string, value func()) {
	p := paletteFor(c.Theme())
	ui.Row(c).Gap(spaceM).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(sizeMeta).TextColor(p.muted).Width(64).Shrink(0)
		// The value gets what is left, and takes only as much of it as it
		// needs: a row that wrapped instead would move its own words
		// around as the window changed.
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(value)
	})
}

// figure is a number or a time, set so that two of them in a column of
// rows line up by themselves.
func figure(c *ui.Context, value string) {
	ui.Text(c, value).FontSize(sizeMeta).FontFeatures("tnum").
		TextColor(paletteFor(c.Theme()).text)
}

func jobPhase(status string) string {
	switch status {
	case "collecting":
		return "采集来源"
	case "generating":
		return "整理日记"
	case "exporting":
		return "导出与同步"
	default:
		return ""
	}
}

// outputCard is the third card of the dashboard's row. Where the row is
// two cards wide it keeps the whole width below them, since a row of its
// own is what its three lines of buttons are drawn for; where the row is
// three it takes one column like the others.
func (s *screen) outputCard(c *ui.Context, columns int) {
	labels := map[string]string{"markdown": "Markdown / JSON", "sheets": "Google Sheets", "excel": "Excel"}
	card := func() {
		cardAt(c, 3, "本地导出与表格同步", func() {
			// One row per output, divided by hairlines rather than drawn as a
			// card inside a card: what belongs together is kept together by
			// the line between it, which is how mygo-music groups a list.
			p := paletteFor(c.Theme())
			edgeLit(ui.Column(c).Gap(0).Radius(radiusTile).Border(1, p.hairline), p).
				Dividers(1, p.hairline).Clip().Transition(surfaceFade).Children(func() {
				for _, item := range s.overview.Outputs {
					s.outputRow(c, item, labels[item.Name])
				}
			})
		})
	}
	if columns < 3 {
		ui.Box(c.Key("outputs-row")).ColumnSpan(columns).Children(card)
		return
	}
	card()
}

func (s *screen) outputRow(c *ui.Context, item workspace.OutputStatus, label string) {
	p := paletteFor(c.Theme())
	if label == "" {
		label = item.Name
	}
	tone, status := toneNeutral, "已停用"
	if item.Enabled {
		status = sourceStatus(item.Status)
		switch item.Status {
		case "success":
			tone = toneGood
		case "error":
			tone = toneDanger
		case "running":
			tone = toneAccent
		}
	}
	action := "同步 " + label
	if item.Name == "markdown" {
		action = "导出 Markdown / JSON"
	}
	if item.Status == "error" {
		action = "重试 " + label
	}
	ui.Row(c.Key("output-"+item.Name)).Gap(spaceL).Padding(spaceM, spaceM).Wrap().
		AlignItems(ui.Center).Children(func() {
		ui.Column(c).Gap(spaceXS).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, label).FontSize(sizeField).FontWeight(weightSemibold).SingleLine()
			switch {
			case !item.Enabled:
				ui.Text(c, "开启后会在此显示导出结果。").FontSize(sizeMeta).TextColor(p.muted)
			case item.ErrorMessage != "":
				ui.Text(c, item.ErrorMessage).FontSize(sizeMeta).TextColor(c.Theme().Danger).
					LineHeight(leadBody)
			default:
				details := []string{}
				if item.LastSuccess != "" {
					details = append(details, "上次成功 "+displayTime(item.LastSuccess))
				}
				if summary := item.Summary; summary != nil {
					if item.Name == "markdown" {
						details = append(details, fmt.Sprintf("更新 %d 个文件", summary.Files))
					} else {
						details = append(details, fmt.Sprintf("写入 %d 行 · 保留 %d 行", summary.Written, summary.Protected))
						if summary.Skipped > 0 {
							details = append(details, fmt.Sprintf("%d 行含凭证已跳过", summary.Skipped))
						}
					}
				}
				if len(details) > 0 {
					ui.Text(c, strings.Join(details, " · ")).FontSize(sizeMeta).
						TextColor(p.muted).LineHeight(leadBody)
				}
			}
		})
		statusPill(c, status, tone, item.Name+"-output-status")
		if pillButton(c, "output-run-"+item.Name, action, nil, false, s.busy || s.jobRunning).Clicked() {
			s.run(action+"…", func(ctx context.Context) error {
				return s.backend.Export(ctx, item.Name)
			})
		}
	})
}

func sourceStatus(status string) string {
	switch status {
	case "success":
		return "成功"
	case "running":
		return "处理中"
	case "error":
		return "失败"
	default:
		return "等待首次运行"
	}
}
