package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed logo.svg
var logoSVG []byte

var dailyLogLogo = ui.MustParseSVG(logoSVG)
