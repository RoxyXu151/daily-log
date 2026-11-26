package main

import (
	"time"

	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

const (
	sidebarWidth = float32(232)
	pagePadding  = float32(28)
)

// palette is the visual vocabulary shared by every screen. The theme still
// drives built-in widgets, while these colors give the hand-built parts of
// the app the same calm, high-contrast surface.
//
// Surfaces stack: the page sits on bg, a card lifts to surface, a block
// inside a card lifts once more to tile, and a panel set into a card sinks
// to sunken. The ladder goes up in dark mode and down in light, so that
// whatever the appearance, a thing resting inside another reads as inside
// it — which is what a box with a border drawn around it cannot say.
type palette struct {
	dark     bool
	bg       ui.Color
	sidebar  ui.Color
	surface  ui.Color
	tile     ui.Color
	sunken   ui.Color
	raised   ui.Color
	text     ui.Color
	muted    ui.Color
	faint    ui.Color
	hairline ui.Color
	border   ui.Color
	hover    ui.Color
	press    ui.Color
	// sel is the wash under the open row of the sidebar and selText the
	// color its word takes. Light mode tints the row with the accent;
	// dark mode lifts it and lets the word carry the color, because a
	// saturated wash laid over a dark room reads as mud rather than as
	// a place.
	sel      ui.Color
	selText  ui.Color
	accent   ui.Color
	onAccent ui.Color
	// edgeLight and edgeDeep are the two ends of a card's hairline: the
	// edge the light comes from is the paler of the two, so a flat face
	// reads as a face with a window somewhere above it.
	edgeLight ui.Color
	edgeDeep  ui.Color
	// shade is what a floating thing drops behind itself: a notice, a
	// popover, a dialog. Cards drop nothing.
	shade ui.Color
}

// cast is the room's color temperature. A neutral gray is the color of
// nobody having chosen anything, so every surface is mixed a little
// toward the accent the app is lit with — and the deeper a surface sits,
// the more of the cast it takes, the way a real room goes: the light
// falls on what is near, the corners keep the color. Text stays neutral,
// because reading should not be tinted.
func cast(base, accent ui.Color, amount float32) ui.Color {
	return base.MixOklab(accent, amount)
}

func paletteFor(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			dark:      true,
			bg:        cast(ui.Hex("#131316"), t.Accent, 0.03),
			sidebar:   cast(ui.Hex("#19191d"), t.Accent, 0.05),
			surface:   cast(ui.Hex("#1d1d22"), t.Accent, 0.055),
			tile:      cast(ui.Hex("#27272d"), t.Accent, 0.06),
			sunken:    cast(ui.Hex("#161619"), t.Accent, 0.035),
			raised:    cast(ui.Hex("#2b2b31"), t.Accent, 0.065),
			text:      ui.Hex("#f5f5f7"),
			muted:     ui.RGBA(235, 235, 245, 0.66),
			faint:     ui.RGBA(235, 235, 245, 0.44),
			hairline:  ui.RGBA(255, 255, 255, 0.1),
			border:    ui.RGBA(255, 255, 255, 0.18),
			hover:     ui.RGBA(255, 255, 255, 0.06),
			press:     ui.RGBA(255, 255, 255, 0.1),
			sel:       ui.RGBA(255, 255, 255, 0.09),
			selText:   t.Accent.MixOklab(ui.Hex("#ffffff"), 0.3),
			accent:    t.Accent,
			onAccent:  t.AccentText,
			edgeLight: ui.RGBA(255, 255, 255, 0.14),
			edgeDeep:  ui.RGBA(255, 255, 255, 0.055),
			shade:     ui.RGBA(0, 0, 0, 0.55),
		}
	}
	return palette{
		bg:        cast(ui.Hex("#ffffff"), t.Accent, 0.012),
		sidebar:   cast(ui.Hex("#f2f2f6"), t.Accent, 0.03),
		surface:   cast(ui.Hex("#f7f7fa"), t.Accent, 0.024),
		tile:      cast(ui.Hex("#ffffff"), t.Accent, 0.015),
		sunken:    cast(ui.Hex("#eff0f4"), t.Accent, 0.038),
		raised:    cast(ui.Hex("#ffffff"), t.Accent, 0.01),
		text:      ui.Hex("#1b1b20"),
		muted:     ui.RGBA(60, 60, 67, 0.62),
		faint:     ui.RGBA(60, 60, 67, 0.34),
		hairline:  ui.RGBA(0, 0, 0, 0.07),
		border:    ui.RGBA(0, 0, 0, 0.14),
		hover:     ui.RGBA(0, 0, 0, 0.04),
		press:     ui.RGBA(0, 0, 0, 0.08),
		sel:       t.Accent.Alpha(0.09),
		selText:   ui.Hex("#1b1b20"),
		accent:    t.Accent,
		onAccent:  t.AccentText,
		edgeLight: ui.RGBA(0, 0, 0, 0.035),
		edgeDeep:  ui.RGBA(0, 0, 0, 0.095),
		shade:     ui.RGBA(0, 0, 0, 0.18),
	}
}

func categoryColor(name string) ui.Color {
	switch name {
	case "科研学习与论文":
		return ui.Hex("#4f77d4")
	case "课程学习":
		return ui.Hex("#368c68")
	case "职业学习与求职":
		return ui.Hex("#c9772d")
	case "校园事务与个人生活":
		return ui.Hex("#b65472")
	default:
		colors := []ui.Color{
			ui.Hex("#4f77d4"), ui.Hex("#368c68"), ui.Hex("#c9772d"),
			ui.Hex("#b65472"), ui.Hex("#7a62c9"), ui.Hex("#3d8d9e"),
			ui.Hex("#a36a32"), ui.Hex("#6d7681"),
		}
		hash := uint32(0)
		for _, r := range name {
			hash = hash*33 + uint32(r)
		}
		return colors[hash%uint32(len(colors))]
	}
}

// categoryShortNames holds the 简称 the user saved, refreshed whenever the
// category list loads: the journal sheet, the legend and the badges all read
// through it, so what 分类管理 shows and what the tables show are one fact.
var categoryShortNames = map[string]string{}

func setCategoryShorts(items []workspace.Category) {
	shorts := make(map[string]string, len(items))
	for _, item := range items {
		if item.Short != "" && item.Short != item.Name {
			shorts[item.Name] = item.Short
		}
	}
	categoryShortNames = shorts
}

func categoryShort(name string) string {
	if short := categoryShortNames[name]; short != "" {
		return short
	}
	switch name {
	case "科研学习与论文":
		return "科研"
	case "课程学习":
		return "课程"
	case "职业学习与求职":
		return "职业"
	case "校园事务与个人生活":
		return "生活"
	}
	return name
}

// accentColors keeps the four accent names that are already saved in users'
// preference files, but gives each one a value tuned for its appearance.
func accentColors(name string, dark bool) (ui.Color, ui.Color) {
	white := ui.Hex("#ffffff")
	switch name {
	case "海蓝":
		if dark {
			return ui.Hex("#4d8dff"), white
		}
		return ui.Hex("#3569c6"), white
	case "青绿":
		if dark {
			return ui.Hex("#1f8d72"), white
		}
		return ui.Hex("#176952"), white
	case "紫罗兰":
		if dark {
			return ui.Hex("#8d65ed"), white
		}
		return ui.Hex("#783992"), white
	default:
		if dark {
			return ui.Oklch(0.66, 0.2, 22), white
		}
		return ui.Oklch(0.6, 0.21, 24), white
	}
}

func applyPalette(t *ui.Theme, p palette) {
	black, white := ui.Hex("#000000"), ui.Hex("#ffffff")
	t.Background = p.bg
	t.Surface = p.tile
	t.SurfaceHover = p.raised
	t.SurfacePressed = p.press
	t.Border = p.border
	t.Text = p.text
	t.TextMuted = p.muted
	t.Accent = p.accent
	t.AccentText = p.onAccent
	t.AccentHover = p.accent.MixOklab(black, 0.1)
	t.AccentPressed = p.accent.MixOklab(black, 0.2)
	if p.dark {
		t.AccentHover = p.accent.MixOklab(white, 0.15)
		t.AccentPressed = p.accent.MixOklab(black, 0.15)
	}
	t.InactiveAccent = p.accent
	t.Selection = p.accent.Alpha(0.3)
	t.Focus = p.accent.Alpha(0.62)
	t.Scrollbar = p.faint
	t.ScrollbarWidth = 6
	t.Radius = radiusField
	t.Spacing = 4
}

// textWidth is the room a line of words needs where it is drawn, asked
// before it is drawn. A column that may shrink to nothing never asks the
// row around it to give way, so a date breaks in the middle: measuring
// the line lets the column refuse to go narrower than the line, and the
// row wraps the rest below instead.
func textWidth(c *ui.Context, text string, size float32, weight int) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: text, Size: size, Weight: weight})
	return w
}

func strokeIcon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

var (
	iconHome         = strokeIcon(`<path d="M3.5 10.5 12 3l8.5 7.5"/><path d="M5.5 9v11h13V9"/><path d="M9.5 20v-6h5v6"/>`)
	iconJournal      = strokeIcon(`<path d="M6 3.5h11A2.5 2.5 0 0 1 19.5 6v14.5H6z"/><path d="M6 3.5v17"/><path d="M10 8h5M10 11.5h5M10 15h3"/>`)
	iconActivity     = strokeIcon(`<path d="M3 12h4l2.2-6 4.1 12 2.2-6H21"/>`)
	iconBook         = strokeIcon(`<path d="M4 4.5h5.5A2.5 2.5 0 0 1 12 7v13a2.5 2.5 0 0 0-2.5-2.5H4z"/><path d="M20 4.5h-5.5A2.5 2.5 0 0 0 12 7v13a2.5 2.5 0 0 1 2.5-2.5H20z"/>`)
	iconPlug         = strokeIcon(`<path d="M9 7V3M15 7V3"/><path d="M7 7h10v4.5a5 5 0 0 1-10 0z"/><path d="M12 16.5V21"/>`)
	iconSpark        = strokeIcon(`<path d="m12 3 1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/><path d="m18.5 16 .8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2-2.2-.8 2.2-.8z"/>`)
	iconClock        = strokeIcon(`<circle cx="12" cy="12" r="8.5"/><path d="M12 7v5l3.5 2"/>`)
	iconPalette      = strokeIcon(`<path d="M12 3a9 9 0 1 0 0 18h1.2a1.8 1.8 0 0 0 0-3.6h-.7a1.8 1.8 0 0 1 0-3.6H17A4 4 0 0 0 21 9.8C21 6 17 3 12 3z"/><circle cx="7.5" cy="10" r=".8" fill="currentColor" stroke="none"/><circle cx="10" cy="6.8" r=".8" fill="currentColor" stroke="none"/><circle cx="14.4" cy="7" r=".8" fill="currentColor" stroke="none"/>`)
	iconPhone        = strokeIcon(`<rect x="7" y="2.5" width="10" height="19" rx="2.5"/><path d="M10 5h4"/><path d="M11 18.5h2"/>`)
	iconCategories   = strokeIcon(`<path d="M4 6.5h7M4 12h10M4 17.5h6"/><path d="M16 5.5h4v4h-4zM16 14h4v4h-4z"/>`)
	iconPencil       = strokeIcon(`<path d="M4 20h4.3L20 8.3 15.7 4 4 15.7z"/><path d="m13.7 6 4.3 4.3"/>`)
	iconRefresh      = strokeIcon(`<path d="M20 7v5h-5"/><path d="M18.2 16a7.5 7.5 0 1 1 .6-8.5L20 12"/>`)
	iconArrow        = strokeIcon(`<path d="M5 12h14"/><path d="m13 6 6 6-6 6"/>`)
	iconChevronDown  = strokeIcon(`<path d="m7 9 5 5 5-5"/>`)
	iconChevronRight = strokeIcon(`<path d="m9 7 5 5-5 5"/>`)
	iconPlay         = strokeIcon(`<path d="m8 5 11 7-11 7z"/>`)
	iconSearch       = strokeIcon(`<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 4.5 4.5"/>`)
	iconPlus         = strokeIcon(`<path d="M12 5v14M5 12h14"/>`)
	iconClose        = strokeIcon(`<path d="m7 7 10 10M17 7 7 17"/>`)
	iconMore         = strokeIcon(`<circle cx="5" cy="12" r="1.4" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.4" fill="currentColor" stroke="none"/><circle cx="19" cy="12" r="1.4" fill="currentColor" stroke="none"/>`)
)

// sectionLabel names what a group of rows is. It is inset by the room a
// row insets its own content, so a column of labels and the column of rows
// under it are one column rather than two that nearly agree.
func sectionLabel(c *ui.Context, label string) {
	ui.Text(c, label).FontSize(sizeSmall).FontWeight(weightSemibold).
		TextColor(paletteFor(c.Theme()).faint).LetterSpacing(trackingLabel).
		Margin(spaceS, 0, spaceXS, spaceM)
}

// pageHeader opens a page: what kind of page, what it is, and what it is
// for, with room at the side for the page's own actions.
func pageHeader(c *ui.Context, eyebrow, title, subtitle string) {
	pageHeaderWith(c, eyebrow, title, subtitle, nil)
}

// pageHeaderWith is a page header holding a place for actions at its end:
// the work a page offers, lined up with its name rather than dropped under
// it, and a slot that says what the page is busy with without moving any
// of the rest.
func pageHeaderWith(c *ui.Context, eyebrow, title, subtitle string, actions func()) {
	p := paletteFor(c.Theme())
	row := ui.Row(c).Gap(spaceL).AlignItems(ui.End).Wrap().Margin(0, 0, spaceXL, 0)
	row.Children(func() {
		ui.Column(c).Gap(spaceXS).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			if eyebrow != "" {
				// The kind of page is named in the accent the room is lit
				// with: it is the page's own voice, not a caption written
				// over it, and it gives every page one note of color at
				// the top where the eye arrives.
				ui.Text(c, eyebrow).FontSize(sizeMeta).FontWeight(weightSemibold).
					LetterSpacing(trackingLabel).TextColor(p.accent.Alpha(0.82))
			}
			ui.Text(c, title).FontSize(sizeDisplay).FontWeight(weightBold).LetterSpacing(trackingTitle)
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(sizeBody).TextColor(p.muted).LineHeight(leadBody)
			}
		})
		if actions != nil {
			ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Shrink(0).Children(actions)
		}
	})
}

// workingBadge is a page saying what it is waiting for. It lives in the
// header, where nothing it says can push the page down the way a banner
// dropped over the top of it would.
func workingBadge(c *ui.Context, key, message string) {
	p := paletteFor(c.Theme())
	row := ui.Row(c.Key(key)).Height(28).PaddingX(spaceM).Gap(spaceS).Radius(radiusPill).
		AlignItems(ui.Center).Shrink(0)
	row.Background(row.AnimateColor("bg", p.hover, themeFade))
	row.Children(func() {
		spinnerBox(c, p.accent)
		txt := ui.Text(c, message).FontSize(sizeMeta).FontWeight(weightMedium).SingleLine()
		txt.TextColor(txt.AnimateColor("fg", p.muted, hoverFade))
	})
}

// edgeLit draws a face's hairline the way light falls on a face: paler
// along the edge it comes from, deeper along the one it does not. It is
// the difference between a rectangle that has a color and a rectangle
// that is somewhere — and it costs the eye nothing to read.
func edgeLit(el ui.Element, p palette) ui.Element {
	return el.BorderLinearGradient(ui.LinearGradient{Angle: 180, From: p.edgeLight, To: p.edgeDeep})
}

// card is a surface holding its own subject: flat, edged by a hairline,
// and dropping nothing. Depth in this app comes from surfaces stacked on
// surfaces, as mygo-music's do; only things floating over the app — a
// notice, a popover — cast a shadow.
func card(c *ui.Context, title string, body func()) {
	p := paletteFor(c.Theme())
	edgeLit(ui.Column(c).Padding(spaceXL).Gap(spaceM).Radius(radiusCard).
		Border(1, p.hairline).Background(p.surface), p).
		Transition(surfaceFade).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(sizeCardTitle).FontWeight(weightBold).LetterSpacing(trackingCard)
		}
		body()
	})
}

// cardAt is a card arriving on a page with the others beside it.
func cardAt(c *ui.Context, at int, title string, body func()) {
	p := paletteFor(c.Theme())
	edgeLit(ui.Column(c).Padding(spaceXL).Gap(spaceM).Radius(radiusCard).
		Border(1, p.hairline).Background(p.surface), p).
		Transition(ui.ElementTransition{
			Duration: 260 * time.Millisecond, Position: true, Colors: true,
			Animation: staggered(at),
			Enter:     &ui.Motion{Y: 10, Opacity: 0},
			Exit:      collapsed(),
		}).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(sizeCardTitle).FontWeight(weightBold).LetterSpacing(trackingCard)
		}
		body()
	})
}

// tile is a block resting on a card: a metric, a note. It lifts rather
// than being drawn round with a line.
func tile(c *ui.Context, body func()) {
	ui.Column(c).Padding(spaceM).Gap(spaceXS).Radius(radiusTile).Background(paletteFor(c.Theme()).tile).
		Transition(surfaceFade).Children(body)
}

// sunken is a panel set into the surface around it: the details of a row,
// a note kept for later. Its edge is the card's turned inside out — the
// lip nearest the light is the one in shadow, and the far wall of the well
// is where the light lands.
func sunken(c *ui.Context, body func()) {
	p := paletteFor(c.Theme())
	ui.Column(c).Padding(spaceL).Gap(spaceM).Radius(radiusTile).Background(p.sunken).
		Border(1, p.hairline).
		BorderLinearGradient(ui.LinearGradient{Angle: 180, From: p.edgeDeep, To: p.edgeLight}).
		Transition(surfaceFade).Children(body)
}

// callout is a block that means something beyond its words: a result that
// failed, a step about to cost something. It takes the color of that
// meaning and lets the surface do the rest, so a warning does not have to
// be drawn with a border on top of a border.
func callout(c *ui.Context, color ui.Color, body func()) {
	ui.Column(c).Padding(spaceM).Gap(spaceS).Radius(radiusTile).Background(color.Alpha(0.1)).
		Transition(surfaceFade).Children(body)
}

func toolbar(c *ui.Context, body func()) {
	p := paletteFor(c.Theme())
	edgeLit(ui.Row(c).Padding(spaceM).Gap(spaceS).Wrap().AlignItems(ui.Center).Radius(radiusCard).
		Border(1, p.hairline).Background(p.surface), p).
		Transition(surfaceFade).Children(body)
}

func divider(c *ui.Context) {
	ui.Box(c).Height(1).FillWidth().Background(paletteFor(c.Theme()).hairline)
}

// navButton is one line of the sidebar. Its icon stands where the section
// labels above it do, so the column of words reads as one column.
func navButton(c *ui.Context, key, label string, icon *ui.SVG, active, disabled bool) ui.Element {
	p := paletteFor(c.Theme())
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(heightNav).PaddingX(spaceM).Gap(spaceM).
		Radius(radiusField).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Disabled(disabled)
	bg := ui.Transparent
	switch {
	case disabled:
		bg = ui.Transparent
	case active:
		// Where the reader is should be the one place in the app the
		// accent is worn — a gray pill among gray rows only says that
		// something was pressed once.
		bg = p.sel
	case b.Hovered():
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, hoverFade))
	fg, iconColor := p.text, p.text
	if disabled {
		fg, iconColor = p.faint, p.faint
	} else if active {
		iconColor, fg = p.accent, p.selText
	}
	if !disabled && b.Pressed() {
		b.AnimatePose(ui.Pose{Scale: 0.98}, pressSpring)
	}
	b.Children(func() {
		if icon != nil {
			ic := ui.Icon(c, icon).Size(17, 17)
			ic.TextColor(ic.AnimateColor("fg", iconColor, hoverFade))
		}
		txt := ui.Text(c, label).FontSize(sizeBody).FontWeight(weightMedium).SingleLine()
		txt.TextColor(txt.AnimateColor("fg", fg, hoverFade))
	})
	return b
}

func plainButton(c *ui.Context, key, label string, disabled bool) ui.Element {
	p := paletteFor(c.Theme())
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(heightField).PaddingX(spaceS).Radius(radiusField).
		AlignItems(ui.Start).Justify(ui.Start).FocusRing(false).Disabled(disabled)
	bg := ui.Transparent
	if !disabled && b.Hovered() {
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, hoverFade))
	fg := p.text
	if disabled {
		fg = p.faint
	}
	if !disabled && b.Pressed() {
		b.AnimatePose(ui.Pose{Scale: 0.985}, pressSpring)
	}
	b.Children(func() {
		txt := ui.Text(c, label).FontSize(sizeBody).FontWeight(weightMedium).Shrink(1).MinWidth(0).SingleLine()
		txt.TextColor(txt.AnimateColor("fg", fg, hoverFade))
	})
	return b
}

// pillButton is the app's one button. It hugs its own words: a button
// stretched to the room it happens to be given is a bar, not a button.
func pillButton(c *ui.Context, key, label string, icon *ui.SVG, primary, disabled bool) ui.Element {
	p := paletteFor(c.Theme())
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(heightPill).PaddingX(spaceXL).Gap(spaceS).Radius(radiusPill).
		AlignItems(ui.Center).Justify(ui.Center).FocusRing(false).Disabled(disabled).Shrink(0)
	bg, fg, border := p.hover, p.text, p.hairline
	if primary {
		bg, fg, border = p.accent, p.onAccent, ui.Transparent
	}
	switch {
	case disabled:
		bg, fg = p.hover, p.faint
	case b.Hovered():
		if primary {
			bg = c.Theme().AccentHover
		} else {
			bg = p.press
		}
	}
	b.Background(b.AnimateColor("bg", bg, hoverFade)).
		Border(1, b.AnimateColor("border", border, hoverFade))
	if !disabled && b.Pressed() {
		b.AnimatePose(ui.Pose{Scale: 0.96}, pressSpring)
	}
	b.Children(func() {
		if icon != nil {
			ic := ui.Icon(c, icon).Size(14, 14)
			ic.TextColor(ic.AnimateColor("fg", fg, hoverFade))
		}
		txt := ui.Text(c, label).FontSize(sizeBody).FontWeight(weightSemibold).SingleLine()
		txt.TextColor(txt.AnimateColor("fg", fg, hoverFade))
	})
	return b
}

// actionRow lines buttons up at the start of the room they are given,
// which is where a button belongs unless the row asks otherwise.
func actionRow(c *ui.Context, body func()) {
	ui.Row(c).Wrap().Gap(spaceS).AlignItems(ui.Center).Children(body)
}

func choicePill(c *ui.Context, key, label string, selected, disabled bool) ui.Element {
	p := paletteFor(c.Theme())
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(heightChoice).PaddingX(spaceM).Radius(radiusPill).
		AlignItems(ui.Center).Justify(ui.Center).FocusRing(false).Disabled(disabled).Shrink(0)
	bg, fg, border := ui.Transparent, p.muted, p.hairline
	if selected {
		bg, fg, border = p.accent.Alpha(0.13), p.accent, p.accent.Alpha(0.26)
	}
	switch {
	case disabled:
		fg = p.faint
	case b.Hovered():
		if selected {
			bg = p.accent.Alpha(0.2)
		} else {
			bg = p.hover
		}
	}
	b.Background(b.AnimateColor("bg", bg, hoverFade)).
		Border(1, b.AnimateColor("border", border, hoverFade))
	if !disabled && b.Pressed() {
		b.AnimatePose(ui.Pose{Scale: 0.96}, pressSpring)
	}
	b.Children(func() {
		txt := ui.Text(c, label).FontSize(sizeSmall).FontWeight(weightSemibold).SingleLine()
		txt.TextColor(txt.AnimateColor("fg", fg, hoverFade))
	})
	return b
}

func iconButton(c *ui.Context, key, label string, icon *ui.SVG, disabled bool) ui.Element {
	b := iconButtonBase(c, key, label, disabled)
	b.Children(func() {
		ic := ui.Icon(c, icon).Size(16, 16)
		ic.TextColor(ic.AnimateColor("fg", paletteFor(c.Theme()).muted, hoverFade))
	})
	return b
}

func iconButtonBase(c *ui.Context, key, label string, disabled bool) ui.Element {
	p := paletteFor(c.Theme())
	b := ui.ButtonBase(c.Key(key)).Label(label).Size(heightField, heightField).Radius(radiusPill).Center().FocusRing(false).
		Disabled(disabled).Shrink(0)
	bg := ui.Transparent
	if !disabled && b.Hovered() {
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, hoverFade))
	if !disabled && b.Pressed() {
		b.AnimatePose(ui.Pose{Scale: 0.88}, pressSpring)
	}
	return b
}

type designTab struct {
	id    string
	label string
}

// segmentedTabs is a switch between views as AppKit draws one: segments of
// one width in a small track, divided by hairlines, the one chosen raised
// on a capsule that springs to the next as it changes.
func segmentedTabs(c *ui.Context, tabs []designTab, active string, choose func(string)) {
	p := paletteFor(c.Theme())
	selected := 0
	for i, tab := range tabs {
		if tab.id == active {
			selected = i
		}
	}
	width := float32(0)
	for _, tab := range tabs {
		w, _ := c.MeasureText(0, ui.Span{Text: tab.label, Size: sizeBody})
		width = max(width, w+26)
	}
	const height, inset = float32(32), float32(3)
	radius := float32(9)
	track, capsule, dividerColor := ui.RGBA(0, 0, 0, 0.06), ui.Hex("#ffffff"), ui.RGBA(0, 0, 0, 0.13)
	if p.dark {
		track, capsule, dividerColor = ui.RGBA(255, 255, 255, 0.08), p.raised, ui.RGBA(255, 255, 255, 0.13)
	}
	at := float32(0)
	trackEl := ui.Row(c).Height(height).Width(inset*2+width*float32(len(tabs))).Padding(inset).
		Radius(radius).Background(track).Border(1, p.hairline).Shrink(0)
	at = trackEl.AnimateTo("at", float32(selected),
		ui.Animation{Spring: ui.SpringOptions{Duration: 280 * time.Millisecond, Bounce: 0.08}})
	trackEl.Draw(func(pp *ui.Painter, r ui.Rect) {
		// Hairlines between segments, but not beside the capsule.
		for i := 1; i < len(tabs); i++ {
			if d := float32(i) - at; d > -1 && d < 2 {
				continue
			}
			x := r.X + inset + float32(i)*width
			pp.Fill(ui.Rect{X: x - 0.5, Y: r.Y + 6, W: 1, H: r.H - 12}, dividerColor, 0)
		}
		pill := ui.Rect{X: r.X + inset + at*width, Y: r.Y + inset, W: width, H: r.H - 2*inset}
		if !p.dark {
			pp.Shadow(pill, radius-3, 0, 0.5, 1, 0, ui.RGBA(0, 0, 0, 0.2))
			pp.Shadow(pill, radius-3, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.06))
		}
		pp.Fill(pill, capsule, radius-3)
	}).Children(func() {
		for i, tab := range tabs {
			b := ui.ButtonBase(c.Key(tab.label)).Label(tab.label).Width(width).Height(height - 2*inset).
				Radius(radius - 3).AlignItems(ui.Center).Justify(ui.Center).FocusRing(false)
			if b.Pressed() && i != selected {
				b.Background(dividerColor)
			}
			fg := p.muted
			if i == selected {
				fg = p.text
			}
			b.Children(func() {
				txt := ui.Text(c, tab.label).FontSize(sizeBody).FontWeight(weightSemibold).SingleLine()
				txt.TextColor(txt.AnimateColor("fg", fg, hoverFade))
			})
			if b.Clicked() {
				choose(tab.id)
			}
		}
	})
}

// toastBand is what the app says about what just happened. The library
// drops those words at the foot of the window, which is exactly where a
// sheet keeps its buttons; here they come at the head instead, over the
// page's name, where nothing is meant to be pressed. The card wears the
// app's own face rather than the theme's inverse one, so a confirmation
// and a report are cut from the same cloth.
func toastBand(c *ui.Context, inset float32) {
	p := paletteFor(c.Theme())
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		// inset is what the window's left edge holds back from the page —
		// the sidebar. Centered over the window, a word would sit over the
		// navigation; over the page, it sits over what it is talking about.
		viewport.Padding(spaceM, spaceM, 0, spaceM+inset).Justify(ui.Start).
			AlignItems(ui.Center).Gap(spaceXS)
		for _, item := range toasts {
			part := ui.ToastBase(c, item)
			box := part.Root.Row().AlignItems(ui.Center).Gap(spaceS).
				Padding(spaceS, spaceL).Radius(radiusCard).MaxWidth(520).Shrink(0).
				Background(p.raised).Border(1, p.hairline).
				Shadow(0, 8, 24, -4, p.shade.Alpha(0.6)).
				Transition(popMotion())
			box.Children(func() {
				dot := ui.Box(c).Size(6, 6).Radius(radiusPill).Shrink(0).Background(p.accent)
				dot.Background(dot.AnimateColor("fg", p.accent, themeFade))
				ui.Text(c, item.Title).FontSize(sizeBody).FontWeight(weightMedium).
					TextColor(p.text).LineHeight(leadBody)
				if item.Action == "" {
					return
				}
				button := part.ActionButton().Padding(spaceXS, spaceM).Radius(radiusPill).Shrink(0)
				button.Background(button.AnimateColor("bg", p.hover, hoverFade))
				button.Children(func() {
					ui.Text(c, item.Action).FontSize(sizeBody).FontWeight(weightSemibold).
						TextColor(p.accent).SingleLine()
				})
			})
		}
	})
}

// noticeFloat is what the app has to report, said from above the page
// rather than from inside it: the page keeps its place while the notice
// comes and goes, so a failure never pushes the work down the way a banner
// in the column does. It takes its place in the stack at the foot of the
// page, which is where the page's own words are not.
func noticeFloat(c *ui.Context, message string, failed bool, dismiss func()) {
	p := paletteFor(c.Theme())
	bg, fg, edge := p.raised, p.text, p.hairline
	dot := p.muted
	if failed {
		dot = c.Theme().Danger
	}
	ui.Row(c.Key("notice")).MaxWidth(420).Shrink(0).
		Padding(spaceM, spaceL).Gap(spaceS).AlignItems(ui.Center).
		Radius(radiusCard).Background(bg).Border(1, edge).
		Shadow(0, 8, 24, -4, p.shade.Alpha(0.6)).
		Transition(popMotion()).Children(func() {
		ui.Box(c).Size(6, 6).Radius(radiusPill).Background(dot)
		ui.Text(c, message).FontSize(sizeBody).FontWeight(weightMedium).TextColor(fg).
			LineHeight(leadBody).Grow(1).Shrink(1).MinWidth(0)
		if dismiss != nil {
			b := iconButtonBase(c, "notice-close", "关闭提示", false)
			b.Children(func() {
				ic := ui.Icon(c, iconClose).Size(13, 13)
				ic.TextColor(ic.AnimateColor("fg", p.muted, hoverFade))
			})
			if b.Clicked() {
				dismiss()
			}
		}
	})
}

type statusTone int

const (
	toneNeutral statusTone = iota
	toneGood
	toneWarn
	toneDanger
	toneAccent
)

// statusPill is a state of one thing, told in color: it fades from one tone
// to the next as the state changes, rather than snapping. It hugs its own
// words, and stands where the row puts it.
func statusPill(c *ui.Context, label string, tone statusTone, key any) {
	p := paletteFor(c.Theme())
	bg, fg := p.hover, p.muted
	switch tone {
	case toneGood:
		bg, fg = c.Theme().Success.Alpha(0.14), c.Theme().Success
	case toneWarn:
		bg, fg = c.Theme().Warning.Alpha(0.16), c.Theme().Warning
	case toneDanger:
		bg, fg = c.Theme().Danger.Alpha(0.14), c.Theme().Danger
	case toneAccent:
		bg, fg = p.accent.Alpha(0.14), p.accent
	}
	row := ui.Row(c.Key(key)).Height(heightStatus).PaddingX(spaceS).Gap(spaceXS).Radius(radiusPill).
		AlignItems(ui.Center).Shrink(0).AlignSelf(ui.Start)
	row.Background(row.AnimateColor("bg", bg, themeFade))
	row.Children(func() {
		dot := ui.Box(c).Size(5, 5).Radius(radiusPill).Shrink(0)
		dot.Background(dot.AnimateColor("fg", fg, themeFade))
		txt := ui.Text(c, label).FontSize(sizeSmall).FontWeight(weightSemibold).SingleLine()
		txt.TextColor(txt.AnimateColor("fg", fg, themeFade))
	})
}

// metricTile is one number worth reading: the count, and what it counts.
func metricTile(c *ui.Context, label, value, hint string) {
	p := paletteFor(c.Theme())
	ui.Column(c).Padding(spaceM).Gap(spaceXS).Radius(radiusTile).Background(p.tile).
		Transition(surfaceFade).Children(func() {
		ui.Text(c, label).FontSize(sizeSmall).FontWeight(weightSemibold).LetterSpacing(trackingLabel).
			TextColor(p.muted)
		ui.Text(c, value).FontSize(sizeMetric).FontWeight(weightBold).LetterSpacing(trackingCard).
			FontFeatures("tnum")
		if hint != "" {
			ui.Text(c, hint).FontSize(sizeSmall).TextColor(p.muted).LineHeight(leadMetric)
		}
	})
}

func emptyState(c *ui.Context, icon *ui.SVG, title, detail string) {
	p := paletteFor(c.Theme())
	ui.Column(c).Grow(1).MinHeight(180).Center().Gap(spaceM).Padding(spaceHero).
		Transition(ui.ElementTransition{
			Duration: 200 * time.Millisecond,
			Enter:    &ui.Motion{Opacity: 0, Scale: 0.97},
			Exit:     &ui.Motion{Opacity: 0},
		}).Children(func() {
		ui.Box(c).Size(56, 56).Radius(radiusCard).Center().Background(p.hover).Children(func() {
			ui.Icon(c, icon).Size(24, 24).TextColor(p.faint)
		})
		ui.Text(c, title).FontSize(sizeSection).FontWeight(weightBold).LetterSpacing(trackingCard).
			TextAlign(ui.Center)
		ui.Text(c, detail).FontSize(sizeBody).TextColor(p.muted).TextAlign(ui.Center).
			MaxWidth(420).LineHeight(leadBody)
	})
}
