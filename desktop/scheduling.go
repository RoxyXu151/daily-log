package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"daily-log/internal/model"
	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

type scheduleForm struct {
	settings workspace.ScheduleSettings
	clock    string
	days     string
	included []bool
	install  bool
}

func newScheduleForm(settings workspace.ScheduleSettings) scheduleForm {
	form := scheduleForm{
		settings: settings, clock: settings.Clock, days: strconv.Itoa(settings.LookbackDays),
		included: make([]bool, len(settings.EventTypes)),
	}
	for i, item := range settings.EventTypes {
		form.included[i] = !slices.Contains(settings.Excluded, item.ID)
	}
	return form
}

func (s *screen) openSchedule() {
	s.page = "schedule"
	s.perform("读取日志更新设置…", func() (func(), error) {
		settings, err := s.backend.Schedule(s.ctx)
		if err != nil {
			return nil, err
		}
		return func() {
			s.scheduling = newScheduleForm(settings)
		}, nil
	})
}

func (s *screen) scheduleScreen(c *ui.Context) {
	p := paletteFor(c.Theme())
	pageHeader(c, "AUTOMATION", "日志更新", "安排每日整理时间、回查范围，以及纳入日记的事件类型。")
	if s.busy && s.scheduling.settings.Clock == "" {
		ui.Scroll(c.Key("schedule-scroll")).Grow(1).MinHeight(0).Children(func() { settingsSkeleton(c) })
		return
	}
	ui.Scroll(c.Key("schedule-scroll")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(spaceL).Children(func() {
			form := &s.scheduling
			// The page assembles as one gesture: each card waits a beat
			// behind the one before it.
			cardAt(c.Key("clock"), 0, "定时与补采", func() {
				ui.Text(c, "每天在固定时间整理当天记录，并回查最近几天尚未完成的内容。").
					FontSize(sizeBody).TextColor(p.muted).LineHeight(leadBody)
				width, _ := c.Size()
				columns := 2
				if width < 720 {
					columns = 1
				}
				ui.Grid(c).Columns(columns).Gap(spaceL).Children(func() {
					ui.Column(c.Key("clock-field")).Gap(spaceXS).Children(func() {
						ui.Text(c, "每天执行时间（北京时间）").
							FontSize(sizeSmall).FontWeight(weightSemibold).LetterSpacing(trackingLabel).TextColor(p.muted)
						ui.TextInput(c.Key("clock"), &form.clock).Label("输入定时时间").Placeholder("23:00").Disabled(s.busy)
					})
					ui.Column(c.Key("days-field")).Gap(spaceXS).Children(func() {
						ui.Text(c, "常规回查天数").
							FontSize(sizeSmall).FontWeight(weightSemibold).LetterSpacing(trackingLabel).TextColor(p.muted)
						ui.TextInput(c.Key("days"), &form.days).Label("输入回查天数").Placeholder("3").Disabled(s.busy)
					})
				})
				// What the saved schedule reports about itself sits into
				// the card rather than drawing a box inside it.
				sunken(c, func() {
					factRow(c, "下次运行", func() {
						// A time that has not come yet is not news: the
						// pill says it quietly, in the tone every other
						// plain fact on this page is said in.
						statusPill(c, displayTime(form.settings.Next), toneNeutral, "next-run")
					})
					if form.settings.LastInstalled != "" {
						ui.Textf(c, "本界面上次安装 · %s · 每日 %s",
							displayTime(form.settings.LastInstalled), form.settings.AppliedClock).
							FontSize(sizeMeta).TextColor(p.muted).FontFeatures("tnum")
					}
					if form.settings.Clock != form.settings.AppliedClock {
						ui.Text(c, "如需按此时间运行，请保存后在采集主机安装或更新系统任务。").
							FontSize(sizeBody).TextColor(c.Theme().Warning).LineHeight(leadBody)
					}
				})
			})
			cardAt(c.Key("events"), 1, "纳入日记的事件", func() {
				var excluded int
				// The switches are one grouped list: hairlines between
				// them, not a box around each.
				edgeLit(ui.Column(c).Radius(radiusTile).Border(1, p.hairline).Background(p.tile), p).
					Dividers(1, p.hairline).Clip().Transition(surfaceFade).Children(func() {
					for i, item := range form.settings.EventTypes {
						ui.Column(c.Key(item.ID + "-row")).Padding(spaceM).Children(func() {
							ui.Checkbox(c.Key(item.ID), &form.included[i], fmt.Sprintf("%s · 今日 %d 条", item.Label, item.Today)).
								FontSize(sizeBody).FontFeatures("tnum").Disabled(s.busy)
						})
						if !form.included[i] {
							excluded += item.Today
						}
					}
				})
				ui.Textf(c, "按当前选项，今日已有记录中 %d 条将排除在日记整理之外，活动流水仍可查看。", excluded).
					FontSize(sizeMeta).TextColor(p.muted).LineHeight(leadBody).FontFeatures("tnum")
			})
			toolbar(c, func() {
				if pillButton(c, "save-schedule", "保存更新设置", iconSpark, true, s.busy).Clicked() {
					s.saveSchedule()
				}
				if !s.remote && pillButton(c, "install-schedule", "安装或更新系统任务", nil, false,
					s.busy || !form.saved()).Clicked() {
					form.install = true
				}
			})
			if form.install {
				ui.Column(c.Key("confirm-install-wrap")).Transition(disclosureMotion()).Children(func() {
					// The card is the block: the warning's color carries
					// its weight, so it needs no tinted box inside.
					cardAt(c.Key("confirm-install"), 2, "确认安装", func() {
						ui.Text(c, "按已保存时间安装当前用户的定时与恢复任务。现有 daily-log 系统任务会更新。").
							FontSize(sizeBody).TextColor(c.Theme().Warning).LineHeight(leadBody)
						actionRow(c, func() {
							if pillButton(c, "cancel-install", "取消安装", nil, false, s.busy).Clicked() {
								form.install = false
							}
							if pillButton(c, "confirm-install-task", "确认安装系统任务", nil, true,
								s.busy || !form.saved()).Clicked() {
								s.installSchedule()
							}
						})
					})
				})
			}
		})
	})
}

func (f scheduleForm) input() (workspace.ScheduleInput, error) {
	clock := strings.TrimSpace(f.clock)
	if !model.ValidDailyClock(clock) {
		return workspace.ScheduleInput{}, fmt.Errorf("定时时间须使用 HH:MM")
	}
	days, err := strconv.Atoi(strings.TrimSpace(f.days))
	if err != nil {
		return workspace.ScheduleInput{}, fmt.Errorf("请填写回查天数")
	}
	input := workspace.ScheduleInput{Clock: clock, LookbackDays: days}
	for i, item := range f.settings.EventTypes {
		if !f.included[i] {
			input.Excluded = append(input.Excluded, item.ID)
		}
	}
	slices.Sort(input.Excluded)
	return input, nil
}

func (f scheduleForm) saved() bool {
	input, err := f.input()
	return err == nil && input.Clock == f.settings.Clock && input.LookbackDays == f.settings.LookbackDays &&
		slices.Equal(input.Excluded, f.settings.Excluded)
}

func (s *screen) saveSchedule() {
	input, err := s.scheduling.input()
	if err != nil {
		s.message, s.failed = err.Error(), true
		return
	}
	s.perform("保存更新设置…", func() (func(), error) {
		if err := s.backend.SaveSchedule(s.ctx, input); err != nil {
			return nil, err
		}
		return s.reloadSchedule("更新设置已保存")
	})
}

func (s *screen) installSchedule() {
	s.perform("正在注册系统任务…", func() (func(), error) {
		if err := s.backend.InstallSchedule(s.ctx); err != nil {
			return nil, err
		}
		return s.reloadSchedule("系统任务安装成功")
	})
}

func (s *screen) reloadSchedule(message string) (func(), error) {
	settings, err := s.backend.Schedule(s.ctx)
	return func() {
		if err == nil {
			s.scheduling = newScheduleForm(settings)
			s.overview.NextRun = settings.Next
			s.notify(message)
		}
	}, err
}
