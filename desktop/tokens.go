package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The scale every screen draws on, so that the app reads as one thing:
// rooms on a four-point grid, corners that grow with the surface they
// round, type that steps by role, and one vocabulary of motion. mygo-music
// keeps the same discipline — a hover is a hover everywhere, and a panel
// opening takes as long as any other panel.

// Room, on a four-point grid. A page breathes by spaceXXL, a card by
// spaceL, and the lines inside a card by spaceS.
const (
	// spaceHair is the room between two lines that are one thought: a
	// date and its weekday, a count and its unit. The grid starts at four,
	// but a table row has to hold two lines in the room of one.
	spaceHair = float32(2)
	spaceXS   = float32(4)
	spaceS    = float32(8)
	spaceM    = float32(12)
	spaceL    = float32(16)
	spaceXL   = float32(20)
	spaceXXL  = float32(24)
	spaceHero = float32(28)
)

// The heights controls keep, whatever else changes: a line of the sidebar,
// a pill, a status, a sheet's header. Named because a row that is 33 in
// one place and 36 in another is two different apps pretending to be one.
const (
	heightNav    = float32(34) // a line of the sidebar
	heightChoice = float32(30) // a pill that is one of several
	heightStatus = float32(24) // a state told in color
	heightField  = float32(32) // a square button holding an icon
	heightPill   = float32(36) // a thing you can press
	heightHeader = float32(40) // the band over a sheet
)

// The narrowest a container may be squeezed before the words it holds
// stop being the words they are. A column that shrinks freely to zero
// never asks its row to wrap, so a date breaks in the middle and a time
// falls down one character per line; these are the widths that make a row
// give way and move its neighbours below it instead.
const (
	widthDate   = float32(150) // "2026-11-24" read at a glance
	widthFacts  = float32(196) // a label and the time it is said of
	widthMetric = float32(120) // a figure and the line under it
)

// Corners, from a row of text to a sheet holding a table.
const (
	radiusField = float32(8)   // an input, a nav row
	radiusRow   = float32(10)  // a row inside a card
	radiusTile  = float32(12)  // a block beside other blocks
	radiusCard  = float32(14)  // a card
	radiusSheet = float32(16)  // a sheet holding a table
	radiusPill  = float32(999) // anything that hugs its own words
)

// Type, by role. The ladder is whole numbers: a half point is a size
// chosen once and copied forever, not a step the eye can name.
const (
	// sizeHero is one step above a page's title, and there is exactly one
	// of them in the app: the day the工作台 is about. A page whose
	// largest word is its name says "here is a form"; a page whose largest
	// word is the day says "here is your day".
	sizeHero      = float32(36) // the day, on the工作台
	sizeDisplay   = float32(30) // a page's title
	sizeFigure    = float32(28) // a day, a number worth reading at a glance
	sizeMetric    = float32(22) // a count in a tile
	sizeSection   = float32(19) // a section inside a page
	sizeCardTitle = float32(15) // a card's own title
	sizeField     = float32(14) // what you are meant to read first
	sizeBody      = float32(13) // a control's label, a line of note
	sizeMeta      = float32(12) // a caption, a status
	sizeSmall     = float32(11) // a count, a unit
	sizeMicro     = float32(10) // the label over a control
)

// Line heights, by what the text has to do.
const (
	leadTight  = float32(1.2) // a title
	leadBody   = float32(1.4) // a paragraph
	leadLoose  = float32(1.5) // a body of evidence you read through
	leadMetric = float32(1.3) // a number and the line under it
)

// Weights, the four the app knows: calm labels, semibold controls, bold
// titles.
const (
	weightRegular  = 400
	weightMedium   = 500
	weightSemibold = 600
	weightBold     = 700
)

// Letter spacing: titles tighten, small caps open up.
const (
	trackingTitle  = float32(-0.6)
	trackingFigure = float32(-0.45)
	trackingCard   = float32(-0.2)
	trackingLabel  = float32(0.4)
)

// Motion, one vocabulary for the whole app.
//
// hoverFade is a control's color under the pointer: long enough to see,
// short enough not to be noticed. stateFade is a control changing state on
// its own — a choice made, a status turned. themeFade is the whole app
// changing appearance, which should settle rather than snap. Everything
// that moves because a hand pressed it runs on a spring, as mygo-music's
// buttons do: ui.Snappy, and ui.Smooth where the move is larger.
var (
	hoverFade = ui.Animation{Duration: 120 * time.Millisecond}
	stateFade = ui.Animation{Duration: 200 * time.Millisecond}
	themeFade = ui.Animation{Duration: 200 * time.Millisecond, Oklab: true}

	// pressSpring is a button giving under a press, and liftSpring a card
	// rising toward the pointer.
	pressSpring = ui.Snappy
	liftSpring  = ui.Snappy

	// disclosure is how long a panel takes to open or close, and a notice
	// to come or go: one length, everywhere.
	disclosure = 200 * time.Millisecond

	// siblingDelay staggers the members of a set entering together, so a
	// page assembling itself reads as one gesture rather than a scramble.
	siblingDelay = 30 * time.Millisecond
)

// collapsed is what an element says of itself as it opens or closes along
// the column it sits in: no room, its content still opaque, so the words
// slide rather than fade out from nothing.
func collapsed() *ui.Motion { return &ui.Motion{Collapse: true, Opacity: 1} }

// disclosureMotion is a panel, a row of details, a form: opening and
// closing along the column, its neighbors following at the same pace.
func disclosureMotion() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: disclosure, Position: true, Height: true, Colors: true,
		Enter: collapsed(), Exit: collapsed(),
	}
}

// rowFollow is a row of a sheet keeping step with a panel opening or
// closing above it. The panel's own motion (disclosureMotion) takes its
// height over disclosure; the rows it moves must travel the same distance
// on the same curve, or they snap to their new place while the leaving
// copy of the panel is still drawn at the old one — two rows' words in one
// place. Position only: the row's height is its panel's to animate.
func rowFollow() ui.ElementTransition {
	return ui.ElementTransition{Duration: disclosure, Position: true}
}

// fadeMotion is one thing taking the place of another in the same room.
// Both go and come at once rather than in turn: waiting for the copy
// leaving to finish would leave the new thing unseen — and unclickable —
// for the length of the move, which is a lag you can feel on a click.
func fadeMotion() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: disclosure, Colors: true,
		Enter: &ui.Motion{Opacity: 0}, Exit: &ui.Motion{Opacity: 0},
	}
}

// pageMotion is a page coming in place of a page: the copy leaving takes
// no room, so the new page is already in its place and nothing else
// moves. mygo-music's pages change under bars that stay put; here the
// change itself is a crossfade, short enough not to read as a wait.
func pageMotion() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: 180 * time.Millisecond,
		Enter:    &ui.Motion{Opacity: 0}, Exit: &ui.Motion{Opacity: 0},
	}
}

// popMotion is a thing that arrives over the app rather than inside it: a
// notice, a dialog. It comes in a little small and settles, and leaves a
// little large — the shape every dialog in mygo-music takes.
func popMotion() ui.ElementTransition {
	return ui.ElementTransition{
		Animation: &ui.Snappy, Colors: true,
		Enter: &ui.Motion{Scale: 0.94, Opacity: 0},
		Exit:  &ui.Motion{Scale: 0.96, Opacity: 0},
	}
}

// riseMotion is a card coming onto a page: from a little below, and from
// nothing, with the cards after it waiting a moment longer.
func riseMotion(at int) ui.ElementTransition {
	return ui.ElementTransition{
		Duration: 260 * time.Millisecond,
		Animation: &ui.Animation{
			Spring: ui.SpringOptions{Duration: 400 * time.Millisecond, Bounce: 0.1},
			Delay:  time.Duration(at) * siblingDelay,
		},
		Enter: &ui.Motion{Y: 10, Opacity: 0},
		Exit:  &ui.Motion{Opacity: 0},
	}
}

// surfaceFade is what a card, a tile, a sheet does when its colors change
// under it — the appearance switched, a state turned: it settles into the
// new colors rather than snapping.
var surfaceFade = ui.ElementTransition{Duration: 200 * time.Millisecond, Colors: true, Shadows: true}

// staggered is the animation for the at-th-member of a set entering
// together: each waits a beat behind the one before it.
func staggered(at int) *ui.Animation {
	a := ui.Snappy
	a.Delay = time.Duration(at) * siblingDelay
	return &a
}
