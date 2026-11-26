package main

import (
	"fmt"

	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

type connectionForm struct {
	selected    workspace.Connection
	values      []workspace.Field
	remove      bool
	testMessage string
	testFailed  bool
	advanced    bool
}

// advancedField marks the developer-facing knobs (service addresses, library
// paths, host commands, calendar/thread id lists) that a first-time user
// should not have to read. They stay reachable under 高级选项 but are not on
// the main path; the primary path is just the credential and a connect button.
func advancedField(source, key string) bool {
	switch source {
	case "zotero":
		return key == "base_url" || key == "library" || key == "session_gap_minutes"
	case "todoist":
		return key == "base_url"
	case "codex":
		return key == "hosts" || key == "start_date"
	case "calendar":
		return key == "calendar_ids"
	}
	return false
}

func (s *screen) openConnections() {
	s.page = "connections"
	s.perform("读取来源设置…", func() (func(), error) {
		items, err := s.backend.Connections(s.ctx)
		if err != nil {
			return nil, err
		}
		return func() {
			s.connections = items
			s.connection = connectionForm{}
		}, nil
	})
}

func (s *screen) selectConnection(item workspace.Connection) {
	s.connection = connectionForm{selected: item, values: append([]workspace.Field(nil), item.Fields...)}
	s.message, s.failed = "", false
}

func (s *screen) toggleConnection(item workspace.Connection) {
	if s.connection.selected.ID == item.ID {
		s.connection = connectionForm{}
		s.message, s.failed = "", false
		return
	}
	s.selectConnection(item)
}

func (s *screen) setConnectionEnabled(item workspace.Connection, enabled bool) {
	input := connectionForm{selected: item, values: item.Fields}.input()
	input.Enabled = enabled
	message := item.Label + " 已停用"
	if enabled {
		message = item.Label + " 已启用"
	}
	// The switch only decides whether the source keeps collecting; it is
	// not a request to read its parameters. Leave whichever card is open
	// as it was, so flipping the switch never unfolds the row under the
	// pointer — opening stays the chevron's and the row's own job.
	openID := s.connection.selected.ID
	s.perform("正在更新采集开关…", func() (func(), error) {
		if err := s.backend.SaveConnection(s.ctx, input); err != nil {
			return nil, err
		}
		return s.reloadConnections(openID, message)
	})
}

// integrationName says how a source is reached, in the words a user can
// match against what they already know about the service.
func integrationName(kind string) string {
	switch kind {
	case "api":
		return "官方 API"
	case "local-api":
		return "本机 API"
	case "file":
		return "本地文件"
	case "oauth":
		return "OAuth 授权"
	case "command":
		return "本机命令"
	}
	return "标准接入"
}

func (s *screen) connectionsScreen(c *ui.Context) {
	pageHeaderWith(c, "SOURCES", "来源管理", "已添加的来源以列表显示；点击任一项展开连接状态与参数。", func() {
		if pillButton(c, "toggle-catalog", "添加来源", iconPlus, !s.addCatalog, s.busy).Clicked() {
			s.addCatalog = !s.addCatalog
		}
	})
	if s.busy && len(s.connections) == 0 {
		ui.Scroll(c.Key("connections-scroll")).Grow(1).MinHeight(0).Children(func() { settingsSkeleton(c) })
		return
	}
	ui.Scroll(c.Key("connections-scroll")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(spaceL).Children(func() {
			if s.addCatalog {
				s.sourceCatalog(c)
			}
			sectionLabel(c, "采集来源")
			listed := 0
			for _, item := range s.connections {
				if !item.Added {
					continue
				}
				listed++
				s.connectionItem(c, item)
			}
			if listed == 0 {
				// emptyState claims and centers its own room, so it needs a
				// surface to sit on rather than a box drawn around it.
				card(c, "", func() {
					emptyState(c, iconPlug, "还没有添加来源", "点上方「添加来源」，从常见服务里挑一个开始。")
				})
			}
		})
	})
}

// sourceCatalog is the 添加来源 panel: the common services that are
// registered but not yet in the list, each saying how it is reached. Adding
// one saves the connection with its standard defaults and opens its card.
func (s *screen) sourceCatalog(c *ui.Context) {
	p := paletteFor(c.Theme())
	var available []workspace.Connection
	for _, item := range s.connections {
		if !item.Added && !item.Removed {
			available = append(available, item)
		}
	}
	ui.Column(c.Key("source-catalog")).Transition(disclosureMotion()).Children(func() {
		card(c, "添加来源", func() {
			ui.Text(c, "按标准接入方式列出常见服务；添加后只需填凭证即可测试。").
				FontSize(sizeMeta).TextColor(p.muted).LineHeight(leadBody)
			if len(available) == 0 {
				ui.Text(c, "所有可用来源都已在列表中。").FontSize(sizeMeta).TextColor(p.muted)
				return
			}
			for _, item := range available {
				ui.Row(c.Key("catalog-" + item.ID)).Gap(spaceM).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Gap(spaceXS).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						ui.Text(c, item.Label).FontSize(sizeBody).FontWeight(weightSemibold).SingleLine()
						ui.Text(c, item.Description).FontSize(sizeMeta).TextColor(p.muted).SingleLine()
					})
					// One column for the kind, one for the button: badges of
					// differing width would otherwise leave the buttons and
					// their labels ragged down the panel.
					ui.Box(c).Width(96).Shrink(0).Children(func() {
						statusPill(c, integrationName(item.Integration), toneNeutral, "catalog-kind-"+item.ID)
					})
					if pillButton(c, "catalog-add-"+item.ID, "添加", nil, false, s.busy).Clicked() {
						s.addSource(item)
					}
				})
			}
		})
	})
}

func (s *screen) addSource(item workspace.Connection) {
	id := item.ID
	s.perform("添加来源…", func() (func(), error) {
		if err := s.backend.SaveConnection(s.ctx, workspace.ConnectionInput{ID: id, Enabled: true}); err != nil {
			return nil, err
		}
		return func() {
			items, err := s.backend.Connections(s.ctx)
			if err == nil {
				s.connections = items
				for _, entry := range items {
					if entry.ID == id {
						s.selectConnection(entry)
						break
					}
				}
			}
			s.addCatalog = false
			s.notify(item.Label + " 已添加，填好凭证后测试连接")
		}, nil
	})
}

func (s *screen) connectionItem(c *ui.Context, item workspace.Connection) {
	p := paletteFor(c.Theme())
	open := s.connection.selected.ID == item.ID
	label := item.Label + " · 暂停"
	if item.Enabled {
		label = item.Label + " · 启用"
	} else if item.Removed {
		label = item.Label + " · 可添加"
	}
	// Opening a source firms its edge and nothing else: no strip of accent
	// laid across the top, since the edge already says which card is awake
	// and a second sign of it is only noise.
	border := p.hairline
	if open {
		border = p.border
	}
	// The card moves only because something inside it does: it keeps the
	// disclosure's clock for its own height and for the place of the cards
	// after it, so the list slides open rather than jumping, but it has no
	// collapsing of its own when the page is assembled.
	follow := disclosureMotion()
	follow.Enter, follow.Exit = nil, nil
	card := ui.Column(c.Key("source-item-"+item.ID)).Radius(radiusCard).Border(1, border).
		Background(p.surface)
	if !open {
		// Only a closed card wears the lit edge: an open one firms its
		// edge instead, and the firmer line is the signal, not the light.
		edgeLit(card, p)
	}
	card.Clip().Transition(follow).Children(func() {
		s.connectionHeader(c, item, label, open)
		if open {
			s.connectionEditor(c.Key("editor"))
		}
	})
}

// connectionHeader is one source's line: its name, the switch that decides
// whether it keeps collecting, and the chevron that opens the rest. All
// three rest on the same wash, so the row lights as one thing from edge to
// edge instead of leaving the controls standing outside the color.
func (s *screen) connectionHeader(c *ui.Context, item workspace.Connection, label string, open bool) {
	p := paletteFor(c.Theme())
	header := ui.Row(c.Key("header")).Padding(spaceM, spaceL).Gap(spaceL).AlignItems(ui.Center)
	tint := ui.Transparent
	switch {
	case open:
		tint = p.accent.Alpha(0.06)
	case !s.busy && header.Hovered():
		// A row that cannot be opened now should not light as if it could.
		tint = p.hover
	}
	header.Background(header.AnimateColor("bg", tint, hoverFade))
	header.Children(func() {
		// The row's own background carries the hover, so the button stays
		// quiet: one surface per row, not a highlight drawn inside one.
		trigger := ui.ButtonBase(c.Key("source-" + item.ID)).Label(label).Role(ui.RoleDisclosure).
			Expanded(open).Grow(1).Shrink(1).MinWidth(0).Gap(spaceM).
			AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Disabled(s.busy)
		trigger.Background(ui.Transparent)
		trigger.Children(func() {
			ui.Box(c).Size(40, 40).Shrink(0).Radius(radiusTile).Center().
				Background(p.accent.Alpha(0.13)).Children(func() {
				ui.Icon(c, iconPlug).Size(18, 18).TextColor(p.accent)
			})
			ui.Column(c).Gap(spaceXS).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, item.Label).FontSize(sizeField).FontWeight(weightSemibold).SingleLine()
				ui.Text(c, item.Description).FontSize(sizeMeta).TextColor(p.muted).SingleLine()
			})
		})
		if trigger.Clicked() {
			s.toggleConnection(item)
		}
		ui.Column(c).Gap(spaceXS).Shrink(0).Children(func() {
			ui.Text(c, "采集").FontSize(sizeMicro).FontWeight(weightSemibold).
				LetterSpacing(trackingLabel).TextColor(p.muted)
			enabled := item.Enabled
			if ui.Switch(c.Key("source-switch-"+item.ID), &enabled).
				Label("启用" + item.Label + "采集").Disabled(s.busy).Changed() {
				s.setConnectionEnabled(item, enabled)
			}
		})
		action, angle := "展开", float32(0)
		if open {
			action, angle = "收起", 90
		}
		expander := iconButtonBase(c, "source-expand-"+item.ID, action+item.Label, s.busy).
			Role(ui.RoleDisclosure).Expanded(open)
		// The chevron turns on the clock the panel opens to, so the two are
		// read as one gesture.
		rotation := expander.Animate("rotation", angle, disclosure)
		expander.Children(func() {
			ic := ui.Icon(c, iconChevronRight).Size(16, 16).Rotate(rotation)
			ic.TextColor(ic.AnimateColor("fg", p.muted, hoverFade))
		})
		if expander.Clicked() {
			s.toggleConnection(item)
		}
	})
}

func (s *screen) connectionEditor(c *ui.Context) {
	p := paletteFor(c.Theme())
	form := &s.connection
	ui.Column(c).Padding(spaceL).Gap(spaceM).Transition(disclosureMotion()).Children(func() {
		// No rule above the panel: the well below brings its own edge with
		// it, and a line drawn over an edge says the same thing twice.
		s.connectionStatus(c, form.selected.ID)
		ui.Text(c, "右上角的采集开关控制后续任务；关闭不会删除历史记录。").
			FontSize(sizeMeta).TextColor(p.muted).LineHeight(leadBody)
		sectionLabel(c, "连接参数")
		if form.selected.ID == "zotero" {
			actionRow(c, func() {
				if pillButton(c, "detect-zotero", "检测本机 Zotero", iconRefresh, false, s.busy).Clicked() {
					s.detectZotero()
				}
			})
		}
		ui.Column(c).Gap(spaceM).Children(func() {
			for i := range form.values {
				field := &form.values[i]
				if advancedField(form.selected.ID, field.Key) {
					continue
				}
				s.connectionField(c, field)
			}
		})
		hasAdvanced := false
		for _, field := range form.values {
			if advancedField(form.selected.ID, field.Key) {
				hasAdvanced = true
			}
		}
		if hasAdvanced {
			if choicePill(c, "advanced-toggle", "高级选项", form.advanced, s.busy).Clicked() {
				form.advanced = !form.advanced
			}
			if form.advanced {
				ui.Column(c).Gap(spaceM).Transition(disclosureMotion()).Children(func() {
					for i := range form.values {
						field := &form.values[i]
						if !advancedField(form.selected.ID, field.Key) {
							continue
						}
						s.connectionField(c, field)
					}
				})
			}
		}
		divider(c)
		actionRow(c, func() {
			if pillButton(c, "save-connection", "保存连接", iconSpark, true, s.busy).Clicked() {
				s.changeConnection(false)
			}
			if pillButton(c, "test-connection", "测试连接", nil, false, s.busy).Clicked() {
				s.changeConnection(true)
			}
			if pillButton(c, "remove-connection", "移除连接", nil, false,
				s.busy || form.selected.Removed).Clicked() {
				form.remove = true
			}
		})
		if form.testMessage != "" {
			tone, label, color := toneGood, "本次测试通过", c.Theme().Success
			if form.testFailed {
				tone, label, color = toneDanger, "本次测试失败", c.Theme().Danger
			}
			// A wash of the tone, not a box drawn in it: the pill and the
			// message already name the result, so the block only has to keep
			// the two together.
			ui.Column(c).Padding(spaceM).Gap(spaceM).Radius(radiusTile).
				Background(color.Alpha(0.08)).Transition(fadeMotion()).Children(func() {
				ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Wrap().Children(func() {
					statusPill(c, label, tone, "connection-test-result")
					ui.Text(c, "该结果只代表当前填写的参数").FontSize(sizeSmall).TextColor(p.muted)
				})
				ui.Text(c, form.testMessage).FontSize(sizeBody).TextColor(color).
					LineHeight(leadBody).Selectable()
			})
		}
		if form.remove {
			// The panel opens along the card the way every other panel in
			// the app does, on the same clock.
			ui.Column(c).Padding(spaceM).Gap(spaceM).Radius(radiusTile).
				Background(c.Theme().Warning.Alpha(0.1)).Transition(disclosureMotion()).Children(func() {
				ui.Text(c, "移除会停止后续采集并清除界面保存的凭证，历史记录仍可查看。").
					FontSize(sizeBody).TextColor(c.Theme().Warning).LineHeight(leadBody)
				actionRow(c, func() {
					if pillButton(c, "cancel-remove", "取消移除", nil, false, s.busy).Clicked() {
						form.remove = false
					}
					if pillButton(c, "confirm-remove", "确认移除连接", nil, true, s.busy).Clicked() {
						s.removeConnection()
					}
				})
			})
		}
	})
}

func (f connectionForm) input() workspace.ConnectionInput {
	result := workspace.ConnectionInput{
		ID: f.selected.ID, Enabled: f.selected.Enabled,
		Values: map[string]string{}, Secrets: map[string]string{},
	}
	for _, field := range f.values {
		if field.ReadOnly {
			continue
		}
		if field.Kind == "secret" {
			if field.Value != "" {
				result.Secrets[field.Key] = field.Value
			}
		} else {
			result.Values[field.Key] = field.Value
		}
	}
	return result
}

func (s *screen) changeConnection(test bool) {
	input := s.connection.input()
	if test {
		s.perform("正在测试连接…", func() (func(), error) {
			result, err := s.backend.TestConnection(s.ctx, input)
			return func() {
				if err == nil {
					s.notify(result)
					s.connection.testMessage, s.connection.testFailed = result, false
				} else {
					s.connection.testMessage, s.connection.testFailed = err.Error(), true
				}
			}, err
		})
		return
	}
	s.perform("正在保存连接…", func() (func(), error) {
		if err := s.backend.SaveConnection(s.ctx, input); err != nil {
			return nil, err
		}
		return s.reloadConnections(input.ID, "来源设置已保存")
	})
}

func (s *screen) removeConnection() {
	id := s.connection.selected.ID
	s.perform("正在移除连接…", func() (func(), error) {
		if err := s.backend.RemoveConnection(s.ctx, id); err != nil {
			return nil, err
		}
		return s.reloadConnections(id, "连接已移除，历史记录仍可查看")
	})
}

func (s *screen) reloadConnections(id, message string) (func(), error) {
	items, err := s.backend.Connections(s.ctx)
	overview, overviewErr := s.backend.Overview(s.ctx)
	return func() {
		s.connection = connectionForm{}
		if err == nil {
			s.connections = items
			for _, item := range items {
				if item.ID == id {
					s.selectConnection(item)
					break
				}
			}
		}
		if overviewErr == nil {
			s.overview = overview
			s.overviewLoaded = true
		}
		if err == nil {
			s.notify(message)
		}
	}, err
}

// connectionField renders one parameter. Google's credential files are picked
// with a file dialog rather than typed as a path, so the main path never asks
// for a filesystem location.
func (s *screen) connectionField(c *ui.Context, field *workspace.Field) {
	p := paletteFor(c.Theme())
	key := c.Key(field.Key)
	if field.ReadOnly {
		ui.Row(c).Gap(spaceM).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c.Key(field.Key+"-label"), field.Label).
				FontSize(sizeSmall).FontWeight(weightSemibold).LetterSpacing(trackingLabel).
				TextColor(p.muted)
			statusPill(c, "在采集主机设置", toneNeutral, key)
		})
		return
	}
	ui.Column(c.Key(field.Key + "-group")).Gap(spaceXS).Children(func() {
		ui.Text(c.Key(field.Key+"-label"), field.Label).
			FontSize(sizeSmall).FontWeight(weightSemibold).LetterSpacing(trackingLabel).
			TextColor(p.muted)
		switch field.Kind {
		case "secret":
			hint := "尚未配置"
			if field.Configured {
				hint = "已配置，留空保留"
			}
			ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Children(func() {
				ui.TextInput(key, &field.Value).Password().Placeholder(hint).
					Label("输入" + field.Label).Disabled(s.busy).Grow(1).Shrink(1).MinWidth(0)
				if s.pickMobilePath != nil && s.connection.selected.ID == "calendar" &&
					(field.Key == "oauth_path" || field.Key == "account_path") {
					if pillButton(c, "pick-"+field.Key, "选择文件", nil, false, s.busy).Clicked() {
						s.pickConnectionFile(field)
					}
				}
			})
		case "list", "hosts":
			ui.TextArea(key, &field.Value).Height(82).Label("输入" + field.Label).Disabled(s.busy)
		default:
			ui.TextInput(key, &field.Value).Label("输入" + field.Label).Disabled(s.busy)
		}
	})
}

// detectZotero fills the local defaults if empty and probes, so connecting
// Zotero is one click rather than reading an address and a library path.
func (s *screen) detectZotero() {
	for i := range s.connection.values {
		field := &s.connection.values[i]
		switch field.Key {
		case "base_url":
			if field.Value == "" {
				field.Value = "http://localhost:23119/api"
			}
		case "library":
			if field.Value == "" {
				field.Value = "users/0"
			}
		}
	}
	s.changeConnection(true)
}

func (s *screen) pickConnectionFile(field *workspace.Field) {
	if s.pickMobilePath == nil {
		return
	}
	path, err := s.pickMobilePath("选择"+field.Label, false)
	if err == nil && path != "" {
		field.Value = path
	}
}

// connectionStatus is what the last collection did, sunk into the card as
// an inset panel: the details of a source sit below it rather than beside
// it, so a second surface says "inside the card" better than a line could.
func (s *screen) connectionStatus(c *ui.Context, id string) {
	for _, item := range s.overview.Sources {
		if item.Name != id {
			continue
		}
		p := paletteFor(c.Theme())
		tone := toneNeutral
		if item.Status == "success" {
			tone = toneGood
		} else if item.Status == "error" {
			tone = toneDanger
		} else if item.Status == "running" {
			tone = toneAccent
		}
		sunken(c, func() {
			ui.Row(c).Gap(spaceM).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, "最近一次采集").FontSize(sizeMeta).FontWeight(weightSemibold).TextColor(p.muted)
				statusPill(c, sourceStatus(item.Status), tone, item.Name+"-sync")
			})
			last := displayTime(item.LastSuccess)
			if last == "" {
				last = "尚未成功采集"
			}
			ui.Text(c, "最近成功 · "+last).FontSize(sizeMeta).TextColor(p.muted).FontFeatures("tnum")
			if item.Coverage != "" {
				label := map[string]string{"ok": "可用", "limited": "有限", "unavailable": "不可用"}[item.Coverage]
				ui.Text(c, "今日覆盖 · "+label+" · 记录至 "+displayTime(item.ObservedThrough)).
					FontSize(sizeMeta).TextColor(p.muted).FontFeatures("tnum")
			}
			if attempt := item.Attempt; attempt != nil {
				if attempt.RequestedSince != "" {
					window := displayTime(attempt.RequestedSince)
					if attempt.RequestedUntil != "" {
						window += " 至 " + displayTime(attempt.RequestedUntil)
					}
					ui.Text(c, "本次请求 · "+window).FontSize(sizeMeta).TextColor(p.muted).FontFeatures("tnum")
				}
				if attempt.EffectiveSince != "" && attempt.EffectiveSince != attempt.RequestedSince {
					ui.Text(c, "实际可回查起点 · "+displayTime(attempt.EffectiveSince)).
						FontSize(sizeMeta).TextColor(c.Theme().Warning).FontFeatures("tnum")
				}
				if attempt.HasCounts {
					ui.Textf(c, "返回 %d 条 · 新增或更新 %d 条", attempt.Returned, attempt.Changed).
						FontSize(sizeMeta).TextColor(p.muted).FontFeatures("tnum")
				}
				if attempt.HistoryGap {
					message := "本次请求超出了来源可回查范围。"
					if attempt.HistoryLimitDays > 0 {
						message = fmt.Sprintf("本次请求超出了账户的 %d 天历史范围。", attempt.HistoryLimitDays)
					}
					ui.Text(c, message).FontSize(sizeMeta).TextColor(c.Theme().Warning).FontFeatures("tnum")
				}
			}
			if item.ErrorMessage != "" {
				ui.Text(c, item.ErrorMessage).FontSize(sizeMeta).TextColor(c.Theme().Danger).LineHeight(leadBody)
			}
			ui.Text(c, "这是历史采集结果；连接测试不会改写此处状态。").
				FontSize(sizeSmall).TextColor(p.faint)
		})
		return
	}
}
