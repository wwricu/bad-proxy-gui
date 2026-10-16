package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Neutral charcoal palette with a single desaturated steel-blue accent.
// Deliberately low-chroma and dense so it reads as a serious desktop tool.
var (
	colBG        = color.NRGBA{0x14, 0x16, 0x1a, 0xff}
	colSurface   = color.NRGBA{0x1d, 0x20, 0x25, 0xff}
	colSurfaceHi = color.NRGBA{0x26, 0x2a, 0x30, 0xff}
	colBorder    = color.NRGBA{0x33, 0x38, 0x40, 0xff}
	colFG        = color.NRGBA{0xcf, 0xd3, 0xda, 0xff}
	colMuted     = color.NRGBA{0x76, 0x7d, 0x88, 0xff}
	colAccent    = color.NRGBA{0x4f, 0x7c, 0xac, 0xff}
	colAccentTxt = color.NRGBA{0xee, 0xf2, 0xf7, 0xff}
	colOK        = color.NRGBA{0x55, 0x93, 0x6f, 0xff}
	colErr       = color.NRGBA{0xb5, 0x60, 0x5f, 0xff}
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
		return color.NRGBA{0x4f, 0x7c, 0xac, 0x40}
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
		return 5
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 5
	case theme.SizeNameScrollBar:
		return 6
	case theme.SizeNameText:
		return 13
	case theme.SizeNameHeadingText:
		return 15
	case theme.SizeNameCaptionText:
		return 11
	default:
		return t.Theme.Size(name)
	}
}
