package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPaletteUsesThemeAccent(t *testing.T) {
	theme := *ui.DarkTheme()
	theme.Accent = ui.Hex("#8d65ed")
	theme.AccentText = ui.Hex("#ffffff")
	p := paletteFor(&theme)
	if p.accent != theme.Accent || p.onAccent != theme.AccentText {
		t.Fatalf("custom palette ignored theme accent: %v / %v", p.accent, theme.Accent)
	}
}
