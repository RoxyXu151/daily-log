package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Skeletons stand in for what is loading: gray blocks in its shape, which
// the content takes the place of without moving anything, and over them a
// lighter band sweeping, across all the blocks of a view in step — the
// loading language of mygo-music's pages.

const (
	shimmerPeriod = 1600 * time.Millisecond
	shimmerSweep  = 1100 * time.Millisecond // of the period, the band crossing; then it rests
	shimmerBand   = float32(260)            // DIPs
)

// shimmer is how the blocks of a skeleton look.
type shimmer struct {
	base, sheen ui.Color
	span        float32 // the width the band crosses: the window's
	still       bool    // the desktop asks for less motion: no band
}

func pageShimmer(c *ui.Context) shimmer {
	p := paletteFor(c.Theme())
	// The band is lighter than the block in dark and paler in light: the
	// blocks are the shape of what is missing, and only breathe.
	s := shimmer{base: ui.RGBA(0, 0, 0, 0.06), sheen: ui.RGBA(0, 0, 0, 0.022)}
	if p.dark {
		s.base, s.sheen = ui.RGBA(255, 255, 255, 0.07), ui.RGBA(255, 255, 255, 0.13)
	}
	s.span, _ = c.Size()
	s.still = c.Preferences().ReduceMotion
	return s
}

// paint fills r, its corners rounded by radius, as a block of the skeleton.
func (s shimmer) paint(p *ui.Painter, r ui.Rect, radius float32) {
	if s.still || r.W <= 0 {
		p.Fill(r, s.base, radius)
		return
	}
	at := time.Duration(p.Now().UnixNano() % int64(shimmerPeriod))
	if at >= shimmerSweep {
		p.Fill(r, s.base, radius)
		p.After(shimmerPeriod - at)
		return
	}
	p.AnimationFrameRate(60)
	t := float32(at) / float32(shimmerSweep)
	x := -shimmerBand/2 + t*(s.span+shimmerBand)
	if x+shimmerBand/2 < r.X || x-shimmerBand/2 > r.X+r.W {
		p.Fill(r, s.base, radius)
		return
	}
	// The band's place along the block, which it may lie partly beyond.
	along := func(x float32) float32 { return (x - r.X) / r.W }
	p.FillGradient(r, ui.LinearGradient{Angle: 90, Oklab: true, Stops: []ui.GradientStop{
		{Color: s.base, At: along(x - shimmerBand/2)},
		{Color: s.sheen, At: along(x)},
		{Color: s.base, At: along(x + shimmerBand/2)},
	}}, radius)
}

// bone is a block of the skeleton, as big as it is laid out.
func (s shimmer) bone(c *ui.Context, radius float32) ui.Element {
	return ui.Box(c).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) { s.paint(p, r, radius) })
}

// text is a line of text at size, as tall as the line and as wide as it is
// laid out, its block the height of the letters.
func (s shimmer) text(c *ui.Context, size, lineHeight float32) ui.Element {
	return ui.Box(c).Shrink(0).Height(size * lineHeight).Draw(func(p *ui.Painter, r ui.Rect) {
		h := size * 0.78
		paint := s.paint
		paint(p, ui.Rect{X: r.X, Y: r.Y + (r.H-h)/2, W: r.W, H: h}, h*0.32)
	})
}

// boneWidths vary the lines of a skeleton, as the lengths of what they
// stand for do, as percentages.
var boneWidths = [...]float32{62, 44, 78, 53, 70, 38, 66, 49, 82, 57, 41, 73}

func boneWidth(i int) float32 { return boneWidths[i%len(boneWidths)] }

// cardSkeleton is a card's worth of blocks: a title, some lines.
func cardSkeleton(c *ui.Context, s shimmer, lines int) {
	p := paletteFor(c.Theme())
	edgeLit(ui.Column(c).Padding(spaceXL).Gap(spaceM).Radius(radiusCard).Border(1, p.hairline).
		Background(p.surface), p).Children(func() {
		s.text(c, sizeCardTitle, leadMetric).Width(110)
		for i := range lines {
			s.text(c, sizeMeta, leadBody).WidthPercent(58 + boneWidth(i)*0.4)
		}
	})
}

// dashboardSkeleton is the工作台 while its overview loads: the page's own
// shape in blocks, so that the cards arriving change nothing around them.
func dashboardSkeleton(c *ui.Context) {
	p := paletteFor(c.Theme())
	s := pageShimmer(c)
	ui.Column(c).Gap(spaceL).Children(func() {
		ui.Row(c).Gap(spaceL).AlignItems(ui.End).Children(func() {
			ui.Column(c).Gap(spaceS).Grow(1).Children(func() {
				s.text(c, sizeMeta, leadMetric).Width(58)
				s.text(c, sizeDisplay, leadTight).WidthPercent(34)
				s.text(c, sizeBody, leadBody).WidthPercent(48)
			})
			s.bone(c, radiusPill).Size(88, 36)
		})
		// The skeleton is the page's shape, so it is drawn with the page's
		// own composition: the day and the way into it on the left, the
		// counts on the right, the day's hours closing the card. Anything
		// else would move the cards when the data arrives.
		day := ui.Column(c).Padding(spaceXL).Gap(spaceL).Radius(radiusCard).Border(1, p.hairline).
			Background(p.surface)
		edgeLit(day, p).Children(func() {
			ui.Row(c).Gap(spaceXXL).Wrap().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Gap(spaceXS).Grow(1).Shrink(1).MinWidth(widthDate + spaceXS).Children(func() {
					s.text(c, sizeCardTitle, leadMetric).Width(96)
					s.text(c, sizeHero, leadTight).WidthPercent(62)
					s.text(c, sizeMeta, leadBody).WidthPercent(92)
					s.bone(c, radiusPill).Size(150, heightPill)
				})
				ui.Column(c).Grow(2).Shrink(1).MinWidth(widthMetric*2 + spaceS).Children(func() {
					columns := dashboardColumns(c, 4, 1000, 560)
					ui.Grid(c).Columns(columns).Gap(spaceS).Children(func() {
						for range columns {
							s.bone(c, radiusTile).Height(88)
						}
					})
				})
			})
			divider(c)
			ui.Column(c).Gap(spaceS).Children(func() {
				ui.Row(c).Gap(spaceM).AlignItems(ui.Center).Children(func() {
					s.text(c, sizeSmall, leadMetric).Width(34)
					s.bone(c, radiusPill).Grow(1).Height(6)
					s.text(c, sizeSmall, leadMetric).Width(34)
				})
				ui.Row(c).Gap(spaceM).Children(func() {
					for range 3 {
						s.bone(c, radiusPill).Size(46, 14)
					}
				})
			})
		})
		columns := dashboardRow(c)
		ui.Grid(c).Columns(columns).Gap(spaceL).AlignItems(ui.Start).Children(func() {
			for i := range 2 {
				ui.Column(c.Key(i)).Children(func() { cardSkeleton(c, s, 3) })
			}
			if columns < 3 {
				ui.Box(c.Key("outputs-skeleton")).ColumnSpan(columns).Children(func() { cardSkeleton(c, s, 4) })
			} else {
				cardSkeleton(c, s, 4)
			}
		})
	})
}

// journalSkeletonRows is the日记总表 while its first page loads: rows of
// blocks in the shape of the sheet's own columns, measured from the same
// constants the rows are cut from.
func journalSkeletonRows(c *ui.Context) {
	s := pageShimmer(c)
	width, _ := c.Size()
	_, h := c.Size()
	rows := max(5, min(10, int(h/journalRowTall)))
	for i := range rows {
		ui.Row(c.Key(i)).Height(journalRowHeight).Shrink(0).PaddingX(journalInset).Gap(spaceL).
			AlignItems(ui.Center).Children(func() {
			ui.Column(c).Width(journalDayWidth).Shrink(0).Gap(spaceHair).Children(func() {
				s.text(c, sizeBody, leadMetric).Width(78)
				s.text(c, sizeSmall, leadMetric).Width(40)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(spaceXS).Children(func() {
				s.text(c, sizeBody, leadMetric).WidthPercent(boneWidth(i))
				s.text(c, sizeSmall, leadMetric).WidthPercent(boneWidth(i+4) * 0.5)
			})
			ui.Box(c).Width(journalCountWidth).Shrink(0).Children(func() {
				s.text(c, sizeBody, leadMetric).Width(68)
			})
			if width >= 1300 {
				ui.Box(c).Width(journalCatsWidth).Shrink(0).Children(func() {
					s.text(c, sizeSmall, leadMetric).WidthPercent(88)
				})
				ui.Box(c).Width(journalRecordWidth).Shrink(0).Children(func() {
					s.text(c, sizeSmall, leadMetric).WidthPercent(72)
				})
			}
			ui.Box(c).Width(spaceXL).Shrink(0)
		})
		if i < rows-1 {
			ui.Box(c).Height(1).FillWidth().Background(paletteFor(c.Theme()).hairline.Alpha(0.7))
		}
	}
}

// activitySkeletonRows is the活动流水 while its first page loads: cells in
// the shape of the sheet's own columns, so the rows that come take the
// place of these and nothing moves.
func activitySkeletonRows(c *ui.Context) {
	s := pageShimmer(c)
	_, h := c.Size()
	rows := max(6, min(12, int(h/activityRowHeight)))
	for i := range rows {
		ui.Row(c.Key(i)).Height(activityRowHeight).Shrink(0).Padding(spaceS, spaceM).Gap(spaceL).
			AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(spaceS).Children(func() {
				s.text(c, sizeBody, leadMetric).WidthPercent(boneWidth(i))
				s.text(c, sizeMeta, leadMetric).WidthPercent(boneWidth(i+2) * 0.6)
			})
			ui.Box(c).Width(92).Shrink(0).Children(func() { s.text(c, sizeMeta, leadMetric).WidthPercent(70) })
			ui.Box(c).Width(154).Shrink(0).Children(func() { s.text(c, sizeMeta, leadMetric).WidthPercent(80) })
			ui.Box(c).Width(156).Shrink(0).Children(func() { s.text(c, sizeMeta, leadMetric).WidthPercent(60) })
		})
		if i < rows-1 {
			ui.Box(c).Height(1).FillWidth().Background(paletteFor(c.Theme()).hairline.Alpha(0.7))
		}
	}
}

// settingsSkeleton is a settings page while it loads: cards of blocks in
// the shape of the forms they hold.
func settingsSkeleton(c *ui.Context) {
	p := paletteFor(c.Theme())
	s := pageShimmer(c)
	ui.Column(c).Gap(spaceL).Children(func() {
		for i := range 3 {
			edgeLit(ui.Column(c.Key(i)).Padding(spaceXL).Gap(spaceM).Radius(radiusCard).Border(1, p.hairline).
				Background(p.surface), p).Children(func() {
				s.text(c, sizeCardTitle, leadMetric).WidthPercent(26 + float32(i)*8)
				ui.Row(c).Gap(spaceL).AlignItems(ui.Center).Children(func() {
					s.text(c, sizeMeta, leadBody).WidthPercent(38)
					ui.Box(c).Grow(1).Shrink(1)
					s.text(c, sizeMeta, leadBody).WidthPercent(22)
				})
				s.bone(c, radiusRow).Height(40)
			})
		}
	})
}

// spinnerBox breathes three dots in sequence, the pause between actions
// made visible without a bar that moves on its own.
func spinnerBox(c *ui.Context, color ui.Color) {
	still := c.Preferences().ReduceMotion
	ui.Box(c).Size(16, 16).Shrink(0).Center().Draw(func(p *ui.Painter, r ui.Rect) {
		if still {
			for i := range 3 {
				p.Fill(dotRect(r, i, 3.5), color.Alpha(0.5), 999)
			}
			return
		}
		period := float64(1100 * time.Millisecond)
		frac := float64(p.Now().UnixNano()%int64(period)) / period
		for i := range 3 {
			t := frac - float64(i)*0.16
			t -= math.Floor(t)
			a := 0.22 + 0.78*math.Sin(t*math.Pi)
			p.Fill(dotRect(r, i, 3.5), color.Alpha(float32(a)), 999)
		}
		p.AnimationFrameRate(30)
	})
}

func dotRect(r ui.Rect, i int, d float32) ui.Rect {
	cx := r.X + r.W/2 + (float32(i)-1)*(d+3)
	return ui.Rect{X: cx - d/2, Y: r.Y + r.H/2 - d/2, W: d, H: d}
}
