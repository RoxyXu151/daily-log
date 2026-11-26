package main

import (
	"fmt"
	"slices"
	"strings"

	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

type profileForm struct {
	profile workspace.Profile
	token   string
	clear   bool
	open    bool
	remove  bool
	preset  string
}

// modelPresets are the services a first-time user is likely to have, with the
// base address and a sensible model filled in so only the key is left to type.
var modelPresets = []struct{ label, baseURL, model string }{
	{"自定义", "", ""},
	{"OpenAI", "https://api.openai.com/v1", "gpt-4o-mini"},
	{"Anthropic", "https://api.anthropic.com/v1", "claude-sonnet-5-5"},
	{"DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat"},
	{"Ollama 本机", "http://127.0.0.1:11434/v1", "qwen3:8b"},
}

func (f *profileForm) applyPreset(label string) {
	f.preset = label
	for _, p := range modelPresets {
		if p.label != label || p.baseURL == "" {
			continue
		}
		f.profile.BaseURL, f.profile.Model = p.baseURL, p.model
		if f.profile.Name == "" {
			f.profile.Name = p.label
		}
		f.profile.Anonymous = p.label == "Ollama 本机"
		return
	}
}

func (s *screen) openModels() {
	s.page = "models"
	s.perform("读取模型设置…", func() (func(), error) {
		settings, err := s.backend.Models(s.ctx)
		if err != nil {
			return nil, err
		}
		return func() {
			s.models = settings
			s.profile = profileForm{}
			s.modelChoices = nil
		}, nil
	})
}

func (s *screen) modelsScreen(c *ui.Context) {
	// The same list language as 来源管理: one row per service, a chevron
	// that opens the editor inside the row, the edge firming while it is
	// open. Adding a model still stands with the page's name.
	pageHeaderWith(c, "MODELS", "模型设置", "已添加的模型以列表显示；同一时间只有一个被使用，点任一项展开参数与测试。", func() {
		if pillButton(c, "new", "添加模型", iconSpark, true, s.busy).Clicked() {
			s.profile = profileForm{open: true, preset: "自定义", profile: workspace.Profile{Enabled: true}}
		}
	})
	if s.busy && len(s.models.Profiles) == 0 && !s.profile.open {
		ui.Scroll(c.Key("models-scroll")).Grow(1).MinHeight(0).Children(func() { settingsSkeleton(c) })
		return
	}
	ui.Scroll(c.Key("models-scroll")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(spaceL).Children(func() {
			sectionLabel(c, "模型服务")
			if len(s.models.Profiles) == 0 && !s.profile.open {
				card(c, "", func() {
					emptyState(c, iconSpark, "还没有模型连接", "添加兼容 OpenAI 接口的服务后，可指定给日记摘录或 Codex 筛选。")
				})
			}
			for _, profile := range s.models.Profiles {
				s.modelItem(c, profile)
			}
			// A new model has no row to open yet: it arrives as its own
			// open card below the list.
			if s.profile.open && s.profile.profile.ID == "" {
				s.newModelCard(c)
			}
		})
	})
}

// modelItem is one model row, drawn the way a source row is: the whole line
// opens the editor inside it, and the chevron turns on the panel's own
// clock so the two read as one gesture.
func (s *screen) modelItem(c *ui.Context, profile workspace.Profile) {
	p := paletteFor(c.Theme())
	open := s.profile.open && s.profile.profile.ID == profile.ID
	inUse := s.models.Default == profile.ID
	border := p.hairline
	if open {
		border = p.border
	} else if inUse {
		// The live config is ringed in the accent, as cc-switch rings it:
		// which one is running should be true at a glance.
		border = p.accent.Alpha(0.45)
	}
	follow := disclosureMotion()
	follow.Enter, follow.Exit = nil, nil
	card := ui.Column(c.Key("model-"+profile.ID)).Radius(radiusCard).Border(1, border).Background(p.surface)
	if !open {
		edgeLit(card, p)
	}
	card.Clip().Transition(follow).Children(func() {
		s.modelHeader(c, profile, open)
		if open {
			s.profileEditor(c)
		}
	})
}

func (s *screen) modelHeader(c *ui.Context, profile workspace.Profile, open bool) {
	p := paletteFor(c.Theme())
	inUse := s.models.Default == profile.ID
	header := ui.Row(c.Key("header")).Padding(spaceM, spaceL).Gap(spaceL).AlignItems(ui.Center)
	tint := ui.Transparent
	switch {
	case open:
		tint = p.accent.Alpha(0.06)
	case inUse:
		// The row in use wears a little of the accent even shut: cc-switch
		// says which config is live before you read a word.
		tint = p.accent.Alpha(0.04)
	case !s.busy && header.Hovered():
		// A row that cannot open now should not light as if it could.
		tint = p.hover
	}
	header.Background(header.AnimateColor("bg", tint, hoverFade))
	header.Children(func() {
		trigger := ui.ButtonBase(c.Key("model-toggle-" + profile.ID)).Label(profile.Name + " · " + profile.Model).
			Role(ui.RoleDisclosure).Expanded(open).Grow(1).Shrink(1).MinWidth(0).Gap(spaceM).
			AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Disabled(s.busy)
		trigger.Background(ui.Transparent)
		trigger.Children(func() {
			ui.Box(c).Size(40, 40).Shrink(0).Radius(radiusTile).Center().Background(p.accent.Alpha(0.13)).Children(func() {
				ui.Text(c, profileInitial(profile.Name)).FontSize(sizeBody).FontWeight(weightBold).TextColor(p.accent)
			})
			ui.Column(c).Gap(spaceHair).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, profile.Name).FontSize(sizeField).FontWeight(weightSemibold).SingleLine()
				ui.Text(c, profile.BaseURL+" · "+profile.Model).FontSize(sizeMeta).TextColor(p.muted).SingleLine()
			})
		})
		if trigger.Clicked() {
			s.toggleModel(profile, open)
		}
		// One status slot of fixed room, then three fixed actions — ▶ 用它,
		// ✎ 编辑, ⋯ 更多. The columns hold from row to row however the
		// words inside them differ; that is what cc-switch gets right.
		status := ui.Row(c.Key(profile.ID + "-status")).Width(96).Shrink(0).Gap(spaceS).
			AlignItems(ui.Center).Justify(ui.End)
		status.Children(func() {
			switch {
			case inUse:
				ui.Box(c).Size(6, 6).Radius(radiusPill).Shrink(0).Background(p.accent)
				ui.Text(c, "使用中").FontSize(sizeSmall).FontWeight(weightSemibold).
					TextColor(p.accent).Shrink(0)
			case profile.Enabled:
				ui.Text(c, "备用").FontSize(sizeSmall).TextColor(p.muted).Shrink(0)
			default:
				ui.Text(c, "已暂停").FontSize(sizeSmall).TextColor(p.faint).Shrink(0)
			}
		})
		use := iconButtonBase(c, "use-profile-"+profile.ID, "使用"+profile.Name,
			s.busy || s.jobRunning || inUse || !profile.Enabled)
		use.Children(func() {
			ic := ui.Icon(c, iconPlay).Size(15, 15)
			ic.TextColor(ic.AnimateColor("fg", p.muted, hoverFade))
		})
		if use.Clicked() {
			s.setDefaultModel(profile)
		}
		edit := iconButton(c, "edit-profile-"+profile.ID, "编辑"+profile.Name, iconPencil, s.busy)
		if edit.Clicked() {
			s.toggleModel(profile, open)
		}
		more := iconButtonBase(c, "more-profile-"+profile.ID, "更多"+profile.Name, s.busy)
		more.Children(func() {
			ic := ui.Icon(c, iconMore).Size(16, 16)
			ic.TextColor(ic.AnimateColor("fg", p.muted, hoverFade))
		})
		more.Menu(func(m *ui.Menu) {
			if m.Item("测试连接").Chosen() {
				s.testProfile(profile)
			}
			if profile.ID != "" && m.Item("移除配置").Chosen() {
				s.profile = profileForm{profile: profile, open: true,
					preset: presetFor(profile.BaseURL), remove: true}
			}
		})
	})
}

func (s *screen) toggleModel(profile workspace.Profile, open bool) {
	// A fetched list belongs to one address: switching rows drops it.
	s.modelChoices = nil
	if open {
		s.profile = profileForm{}
		s.message, s.failed = "", false
		return
	}
	s.profile = profileForm{profile: profile, open: true, preset: presetFor(profile.BaseURL)}
	s.message, s.failed = "", false
}

// presetFor names the service a base address belongs to, so reopening a
// saved model shows the preset it was added from.
func presetFor(baseURL string) string {
	for _, preset := range modelPresets {
		if preset.baseURL != "" &&
			strings.TrimRight(preset.baseURL, "/") == strings.TrimRight(baseURL, "/") {
			return preset.label
		}
	}
	return "自定义"
}

// newModelCard is the one exception to the list: a model that does not
// exist yet has no row to open, so it arrives as its own open card.
func (s *screen) newModelCard(c *ui.Context) {
	p := paletteFor(c.Theme())
	follow := disclosureMotion()
	follow.Enter, follow.Exit = nil, nil
	card := ui.Column(c.Key("model-new")).Radius(radiusCard).Border(1, p.border).Background(p.surface)
	card.Clip().Transition(follow).Children(func() { s.profileEditor(c) })
}

func (s *screen) setDefaultModel(profile workspace.Profile) {
	s.perform("正在切换当前模型…", func() (func(), error) {
		if err := s.backend.SetDefaultModel(s.ctx, profile.ID); err != nil {
			return nil, err
		}
		return s.reloadModels("", profile.Name+" 已设为当前使用")
	})
}

func profileInitial(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return "M"
	}
	return string(runes[0])
}

func (s *screen) testProfile(profile workspace.Profile) {
	s.perform("正在测试 "+profile.Name+"…", func() (func(), error) {
		err := s.backend.TestModel(s.ctx, workspace.ProfileInput{Profile: profile})
		return func() {
			if err == nil {
				s.notify(profile.Name + " 连接测试通过")
			}
		}, err
	})
}

func (s *screen) profileEditor(c *ui.Context) {
	// The form opens and closes along the list, rather than cutting in.
	ui.Column(c.Key("model-form")).Transition(disclosureMotion()).
		Children(func() { s.profileEditorCard(c, &s.profile) })
}

func (s *screen) profileEditorCard(c *ui.Context, form *profileForm) {
	p := paletteFor(c.Theme())
	card(c.Key("model-form-card"), "连接参数", func() {
		ui.Row(c).Wrap().Gap(spaceS).AlignItems(ui.Center).Children(func() {
			statusPill(c, "OpenAI 兼容接口", toneAccent, "profile-kind")
			ui.Text(c, "密钥只保存在本地配置中。").FontSize(sizeMeta).TextColor(p.muted)
		})
		// Choosing a known service fills the address and model, so a first
		// connection is a preset plus a key rather than three free-text URLs.
		labels := make([]string, len(modelPresets))
		for i, pr := range modelPresets {
			labels[i] = pr.label
		}
		if form.preset == "" {
			form.preset = "自定义"
		}
		if ui.Select(c.Key("model-preset"), &form.preset, labels).Label("服务提供商").Width(180).Changed() {
			form.applyPreset(form.preset)
		}
		ui.Grid(c).Columns(2).Gap(spaceL).Children(func() {
			for _, entry := range []struct {
				label, placeholder string
				value              *string
			}{
				{"配置名称", "例如本机模型", &form.profile.Name},
				{"API 基础地址", "例如 https://example.org/v1", &form.profile.BaseURL},
			} {
				ui.Column(c.Key(entry.label + "-group")).Gap(spaceXS).Children(func() {
					ui.Text(c.Key(entry.label+"-label"), entry.label).
						FontSize(sizeSmall).FontWeight(weightSemibold).LetterSpacing(trackingLabel).TextColor(p.muted)
					ui.TextInput(c.Key(entry.label), entry.value).Label("输入" + entry.label).Placeholder(entry.placeholder).Disabled(s.busy)
				})
			}
			// The model name need not be typed: the service can be asked
			// what it serves, and the answer becomes the choices.
			ui.Column(c.Key("模型名称-group")).Gap(spaceXS).Children(func() {
				ui.Text(c.Key("模型名称-label"), "模型名称").
					FontSize(sizeSmall).FontWeight(weightSemibold).LetterSpacing(trackingLabel).TextColor(p.muted)
				ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c.Key("模型名称"), &form.profile.Model).Label("输入模型名称").
						Placeholder("服务端模型 ID").Disabled(s.busy).Grow(1).Shrink(1).MinWidth(0)
					if pillButton(c, "fetch-models", "获取模型列表", iconRefresh, false, s.busy).Clicked() {
						s.fetchModels(form)
					}
				})
				if len(s.modelChoices) > 0 {
					choices := s.modelChoices
					if !slices.Contains(choices, form.profile.Model) {
						choices = append([]string{form.profile.Model}, choices...)
					}
					ui.Text(c, "从官方列表选择").FontSize(sizeSmall).FontWeight(weightSemibold).
						LetterSpacing(trackingLabel).TextColor(p.muted)
					ui.Select(c.Key("model-choice"), &form.profile.Model, choices).
						Label("从官方列表选择").Grow(1)
				}
			})
		})
		hint := "输入 API 密钥"
		if form.profile.HasToken {
			hint = "已配置，留空保留"
		}
		ui.Column(c.Key("token-group")).Gap(spaceXS).Children(func() {
			ui.Text(c, "API 密钥").FontSize(sizeSmall).FontWeight(weightSemibold).LetterSpacing(trackingLabel).TextColor(p.muted)
			ui.TextInput(c.Key("token"), &form.token).Password().Label("API 密钥").Placeholder(hint).Disabled(s.busy || form.clear)
		})
		if form.profile.HasToken {
			ui.Checkbox(c.Key("clear"), &form.clear, "清除原密钥").Disabled(s.busy)
		}
		// One line says what 启用 means here, so two enabled rows do not
		// read as two models running at once.
		ui.Text(c, "同一时间只有「当前使用」的模型参与日记整理与 Codex 筛选；启用表示它可以被选为当前使用。").
			FontSize(sizeMeta).TextColor(p.muted).LineHeight(leadBody)
		// The two service switches rest on the card as one block rather
		// than drawing yet another box inside it.
		tile(c, func() {
			ui.Checkbox(c.Key("anonymous"), &form.profile.Anonymous, "此服务允许无密钥访问").
				FontSize(sizeBody).Disabled(s.busy)
			ui.Checkbox(c.Key("enabled"), &form.profile.Enabled, "启用此模型配置").
				FontSize(sizeBody).Disabled(s.busy)
		})
		divider(c)
		actionRow(c, func() {
			if pillButton(c, "save-model", "保存模型", iconSpark, true, s.busy).Clicked() {
				s.changeModel(false)
			}
			if pillButton(c, "test-model", "测试模型", nil, false, s.busy).Clicked() {
				s.changeModel(true)
			}
			if form.profile.ID != "" && form.profile.Enabled && s.models.Default != form.profile.ID {
				if pillButton(c, "default-model", "设为默认", nil, false, s.busy).Clicked() {
					s.setDefaultModel(form.profile)
				}
			}
			if form.profile.ID != "" && pillButton(c, "remove-model", "移除模型", nil, false, s.busy).Clicked() {
				form.remove = true
			}
		})
		if form.remove {
			// The warning tint is the block's edge; it opens and closes
			// along the card like any other panel.
			ui.Column(c).Padding(spaceM).Gap(spaceM).Radius(radiusTile).
				Background(c.Theme().Warning.Alpha(0.1)).Transition(disclosureMotion()).Children(func() {
				ui.Text(c, "移除后没有可用的整理模型，日记整理将无法运行，直到你添加并启用一个配置。").
					FontSize(sizeBody).TextColor(c.Theme().Warning).LineHeight(leadBody)
				actionRow(c, func() {
					if pillButton(c, "cancel-remove-model", "取消移除", nil, false, s.busy).Clicked() {
						form.remove = false
					}
					if pillButton(c, "confirm-remove-model", "确认移除模型", nil, true, s.busy).Clicked() {
						s.removeModel()
					}
				})
			})
		}
	})
}

// fetchModels asks the service at the form's current address and key what
// it serves. Nothing is saved: the list only feeds 模型名称.
func (s *screen) fetchModels(form *profileForm) {
	input := workspace.ProfileInput{Profile: form.profile}
	if form.token != "" {
		token := form.token
		input.Token = &token
	}
	s.perform("读取官方模型列表…", func() (func(), error) {
		models, err := s.backend.ListModels(s.ctx, input)
		return func() {
			if err == nil {
				s.modelChoices = models
				s.notify(fmt.Sprintf("已获取 %d 个官方模型", len(models)))
			}
		}, err
	})
}

func (s *screen) changeModel(test bool) {
	form := s.profile
	input := workspace.ProfileInput{Profile: form.profile}
	if form.clear {
		token := ""
		input.Token = &token
	} else if form.token != "" {
		input.Token = &form.token
	}
	if test {
		s.perform("正在发送模型测试请求…", func() (func(), error) {
			err := s.backend.TestModel(s.ctx, input)
			return func() {
				if err == nil {
					s.notify("模型响应通过测试")
				}
			}, err
		})
		return
	}
	s.perform("保存模型配置…", func() (func(), error) {
		id, err := s.backend.SaveModel(s.ctx, input)
		if err != nil {
			return nil, err
		}
		return s.reloadModels(id, "模型设置已保存")
	})
}

func (s *screen) removeModel() {
	id := s.profile.profile.ID
	s.perform("移除模型…", func() (func(), error) {
		if err := s.backend.RemoveModel(s.ctx, id); err != nil {
			return nil, err
		}
		return s.reloadModels("", "模型配置已移除")
	})
}

func (s *screen) reloadModels(id, message string) (func(), error) {
	settings, err := s.backend.Models(s.ctx)
	return func() {
		s.profile = profileForm{}
		s.modelChoices = nil
		if err == nil {
			s.models = settings
			for _, profile := range settings.Profiles {
				if profile.ID == id {
					s.profile = profileForm{profile: profile, open: true, preset: presetFor(profile.BaseURL)}
				}
			}
			s.notify(message)
		}
	}, err
}
