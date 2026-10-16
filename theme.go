package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// brand palette: deep navy surfaces, sky accent. Chosen for a calm, professional
// desktop-tool feel rather than Fyne's default flat white.
var (
	colBG        = color.NRGBA{0x0f, 0x14, 0x20, 0xff}
	colSurface   = color.NRGBA{0x16, 0x20, 0x2e, 0xff}
	colSurfaceHi = color.NRGBA{0x1c, 0x28, 0x38, 0xff}
	colBorder    = color.NRGBA{0x2a, 0x3b, 0x52, 0xff}
	colFG        = color.NRGBA{0xe6, 0xea, 0xf2, 0xff}
	colMuted     = color.NRGBA{0x8a, 0x99, 0xb0, 0xff}
	colAccent    = color.NRGBA{0x38, 0xbd, 0xf8, 0xff}
	colAccentTxt = color.NRGBA{0x06, 0x11, 0x1c, 0xff}
	colOK        = color.NRGBA{0x34, 0xd3, 0x99, 0xff}
	colErr       = color.NRGBA{0xf8, 0x71, 0x71, 0xff}
)

type appTheme struct{ fyne.Theme }

func newTheme() fyne.Theme { return &appTheme{Theme: theme.DefaultTheme()} }

func (t *appTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colBG
	case theme.ColorNameForeground:
		return colFG
	case theme.ColorNamePrimary, theme.ColorNameHyperlink, theme.ColorNameFocus:
		return colAccent
	case theme.ColorNameSelection:
		return color.NRGBA{0x38, 0xbd, 0xf8, 0x33}
	case theme.ColorNameHeaderBackground:
		return colSurface
	case theme.ColorNameButton:
		return colSurfaceHi
	case theme.ColorNameInputBackground:
		return colSurface
	case theme.ColorNameInputBorder:
		return colBorder
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return colMuted
	case theme.ColorNameDisabledButton:
		return colSurface
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return colSurface
	case theme.ColorNameSeparator:
		return colBorder
	case theme.ColorNameScrollBar:
		return colBorder
	case theme.ColorNameScrollBarBackground:
		return colBG
	case theme.ColorNameError:
		return colErr
	case theme.ColorNameSuccess:
		return colOK
	default:
		return t.Theme.Color(name, variant)
	}
}

func (t *appTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 10
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameScrollBar:
		return 8
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 20
	default:
		return t.Theme.Size(name)
	}
}
