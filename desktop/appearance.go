package main

import (
	"errors"
	"slices"

	"daily-log/internal/model"

	"github.com/egoist/mygo/ui"
)

var modes = []string{"跟随系统", "浅色", "深色"}
var accents = []string{"默认", "海蓝", "青绿", "紫罗兰"}

type appearance struct {
	Mode   string `json:"mode"`
	Accent string `json:"accent"`
}

func defaultAppearance() appearance { return appearance{modes[0], accents[0]} }

func decodeAppearance(data []byte) (appearance, error) {
	p := defaultAppearance()
	if err := model.Decode(data, &p); err != nil {
		return defaultAppearance(), err
	}
	if !slices.Contains(modes, p.Mode) || !slices.Contains(accents, p.Accent) {
		return defaultAppearance(), errors.New("外观设置无效，已加载默认值")
	}
	return p, nil
}

func (p appearance) apply(c *ui.Context) {
	theme := *c.Theme()
	switch p.Mode {
	case "浅色":
		theme = *ui.LightTheme()
	case "深色":
		theme = *ui.DarkTheme()
	}
	// The accent is chosen before the palette is read, because the accent
	// is what the room is lit with: every surface takes its color
	// temperature from it, so switching to 海蓝 changes the whole app's
	// air rather than only the color of its buttons.
	theme.Accent, theme.AccentText = accentColors(p.Accent, theme.Dark)
	colors := paletteFor(&theme)
	applyPalette(&theme, colors)
	c.SetTheme(&theme)
}

func (s *screen) appearanceScreen(c *ui.Context) {
	p := paletteFor(c.Theme())
	pageHeader(c, "APPEARANCE", "外观", "选择主题和强调色，调整整个工作台的视觉氛围。")
	card(c, "主题与强调色", func() {
		ui.Text(c, "切换选项可预览效果，保存后重启仍生效。").FontSize(sizeBody).TextColor(p.muted)
		ui.Column(c).Children(func() {
			sectionLabel(c, "主题")
			tabs := make([]designTab, len(modes))
			for i, mode := range modes {
				tabs[i] = designTab{mode, mode}
			}
			segmentedTabs(c, tabs, s.appearance.Mode, func(mode string) {
				s.appearance.Mode = mode
			})
		})
		ui.Column(c).Children(func() {
			sectionLabel(c, "强调色")
			ui.Row(c).Wrap().Gap(spaceS).Children(func() {
				for _, accent := range accents {
					if choicePill(c, "accent-"+accent, accent, s.appearance.Accent == accent, s.busy).Clicked() {
						s.appearance.Accent = accent
					}
				}
			})
		})
		appearancePreview(c)
		divider(c)
		actionRow(c, func() {
			if pillButton(c, "reset-appearance", "恢复默认", nil, false, s.busy).Clicked() {
				s.appearance = defaultAppearance()
			}
			if pillButton(c, "save-appearance", "保存外观", iconSpark, true,
				s.busy || s.saveAppearance == nil).Clicked() {
				p := s.appearance
				s.perform("保存外观…", func() (func(), error) {
					err := s.saveAppearance(p)
					return func() {
						if err == nil {
							s.notify("外观设置已保存")
						}
					}, err
				})
			}
		})
	})
}

// appearancePreview is the whole工作台 in miniature, its colors following
// the choices above it: the page under, the sidebar and the content card
// on it, a block resting on the card and another set into it, a notice
// floating over all of them. Each piece eases from the last appearance to
// the next, so the preview shows what a switch will feel like, not only
// what it will look like.
func appearancePreview(c *ui.Context) {
	p := paletteFor(c.Theme())
	stage := ui.Row(c).Height(128).Padding(spaceS).Gap(spaceS).Radius(radiusCard).
		Border(1, p.hairline).Clip()
	stage.Background(stage.AnimateColor("bg", p.bg, themeFade))
	stage.Children(func() {
		side := ui.Column(c).Width(88).FillHeight().Padding(spaceM, spaceS).Gap(spaceS).Radius(radiusTile)
		side.Background(side.AnimateColor("bg", p.sidebar, themeFade))
		side.Children(func() {
			ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Children(func() {
				ui.Image(c, dailyLogLogo).Size(18, 18).Shrink(0)
				fadeBox(c, p.text.Alpha(0.72)).Height(4).Grow(1).Radius(radiusPill)
			})
			for _, width := range []float32{56, 48, 64} {
				nav := fadeBox(c, p.hover).Height(18).Width(width).Radius(radiusField).Center()
				nav.Children(func() {
					fadeBox(c, p.muted).Height(4).Width(width * 0.55).Radius(radiusPill)
				})
			}
		})
		page := ui.Column(c).Grow(1).Shrink(1).MinWidth(0).FillHeight().Padding(spaceM, spaceS).
			Gap(spaceS).Radius(radiusTile)
		page.Background(page.AnimateColor("bg", p.surface, themeFade))
		page.Children(func() {
			// A notice floats here the way it does over the real pages:
			// the one thing in the app that drops a shade.
			float := ui.Row(c).Absolute().Top(spaceM).Right(spaceL).Padding(spaceS).Gap(spaceS).
				Radius(radiusField).AlignItems(ui.Center).Shrink(0)
			float.Background(float.AnimateColor("bg", p.raised, themeFade))
			float.Border(1, float.AnimateColor("border", p.hairline, themeFade))
			float.Shadow(0, 2, 6, -1, p.shade.Alpha(0.4))
			float.Children(func() {
				dot := ui.Box(c).Size(4, 4).Shrink(0).Radius(radiusPill)
				dot.Background(dot.AnimateColor("bg", p.accent, themeFade))
				fadeBox(c, p.muted).Height(4).Width(28).Radius(radiusPill)
			})
			fadeBox(c, p.text.Alpha(0.78)).Height(6).Width(96).Radius(radiusPill)
			fadeBox(c, p.muted.Alpha(0.55)).Height(4).Width(140).Radius(radiusPill)
			ui.Row(c).Gap(spaceS).Children(func() {
				fadeBox(c, p.accent).Height(24).Width(56).Radius(radiusPill)
				bare := fadeBox(c, p.tile).Height(24).Width(48).Radius(radiusPill)
				bare.Border(1, bare.AnimateColor("border", p.hairline, themeFade))
			})
			ui.Row(c).Gap(spaceS).Grow(1).MinHeight(0).Children(func() {
				fadeBox(c, p.tile).Grow(1).FillHeight().Radius(radiusField)
				fadeBox(c, p.sunken).Grow(1).FillHeight().Radius(radiusField)
			})
		})
	})
}

// fadeBox is a block whose color eases to each new one.
func fadeBox(c *ui.Context, bg ui.Color) ui.Element {
	box := ui.Box(c)
	return box.Background(box.AnimateColor("bg", bg, stateFade))
}
