package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Light palette: near-white canvas, white cards, a single desaturated steel-blue
// accent. Kept low-chroma so it still reads as a calm, serious desktop tool.
var (
	colBG        = color.NRGBA{0xf5, 0xf6, 0xf8, 0xff}
	colSurface   = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	colSurfaceHi = color.NRGBA{0xe7, 0xea, 0xef, 0xff}
	colBorder    = color.NRGBA{0xd2, 0xd7, 0xde, 0xff}
	colFG        = color.NRGBA{0x22, 0x26, 0x2b, 0xff}
	colMuted     = color.NRGBA{0x69, 0x70, 0x79, 0xff}
	colAccent    = color.NRGBA{0x2f, 0x6f, 0x9f, 0xff}
	colOK        = color.NRGBA{0x2e, 0x7d, 0x4f, 0xff}
	colErr       = color.NRGBA{0xb2, 0x3b, 0x3b, 0xff}
)

type appTheme struct{ fyne.Theme }

func newTheme() fyne.Theme { return &appTheme{Theme: theme.DefaultTheme()} }

// Variant always reports light so icon resources and any variant-aware widgets pick
// the dark-on-light artwork regardless of the OS preference.
func (t *appTheme) Variant() fyne.ThemeVariant { return theme.LightVariant }

func (t *appTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colBG
	case theme.ColorNameForeground:
		return colFG
	case theme.ColorNamePrimary, theme.ColorNameHyperlink, theme.ColorNameFocus:
		return colAccent
	case theme.ColorNameInputBackground, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return colSurface
	case theme.ColorNameHeaderBackground:
		return colSurfaceHi
	case theme.ColorNameButton:
		return colSurfaceHi
	case theme.ColorNameDisabledButton:
		return colSurfaceHi
	case theme.ColorNameInputBorder:
		return colBorder
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return colMuted
	case theme.ColorNameSelection:
		return color.NRGBA{0x2f, 0x6f, 0x9f, 0x33}
	case theme.ColorNameSeparator, theme.ColorNameScrollBar:
		return colBorder
	case theme.ColorNameScrollBarBackground:
		return colBG
	case theme.ColorNameError:
		return colErr
	case theme.ColorNameSuccess:
		return colOK
	default:
		return t.Theme.Color(name, theme.LightVariant)
	}
}

func (t *appTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBar:
		return 7
	case theme.SizeNameText:
		return 13
	case theme.SizeNameHeadingText:
		return 16
	case theme.SizeNameCaptionText:
		return 12
	default:
		return t.Theme.Size(name)
	}
}
