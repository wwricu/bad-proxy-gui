//go:build darwin

package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// trayTemplateBytes is the menu-bar glyph: flat black on transparent, which is
// what a template image is masked from.
//
//go:embed build/traytemplate.png
var trayTemplateBytes []byte

// applyTrayIcon hands the menu bar a template image rather than the app icon.
//
// AppKit stretches whatever it is given to the full thickness of the bar, so
// the app icon's full-bleed white tile became a white block as tall as the bar
// that stayed white in dark mode too. A template is drawn in whatever colour
// the bar needs, and the padding baked into this png is what makes the glyph
// smaller than the bar.
func applyTrayIcon(tray *application.SystemTray) {
	tray.SetTemplateIcon(trayTemplateBytes)
}
