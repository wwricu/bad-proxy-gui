//go:build windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// trayIcon is the notification-area item, or nil while the setting has it away.
// The handle is kept because hide and show are the same registered icon: Wails
// only flips NIS_HIDDEN, so the picture, the menu and the click handler all
// survive a hide, and bringing it back costs no rebuild.
var trayIcon *application.SystemTray

// setupTray creates the notification-area icon. Wails owns the tray loop, so
// this runs on the startup path rather than in a goroutine of its own.
//
// A left click (or a double click) reopens the window and the right click pops
// the menu carrying Quit. With the icon asked away there is nothing to click,
// and closing the window is then an ordinary close.
//
// macOS has its own version in tray_darwin.go: the item there has to be AppKit's
// own so System Settings can manage it.
func setupTray(a *App) {
	if a.snapshot().HideTray {
		return
	}
	createTray(a)
}

// createTray puts the icon on the bar. It runs at startup, and again when the
// setting is turned back on while the app lives.
func createTray(a *App) {
	tray := a.wails.SystemTray.New()
	applyTrayIcon(tray)
	tray.SetLabel("Bad Proxy")
	tray.SetTooltip("Bad Proxy")
	tray.AttachWindow(a.mainWindow)
	tray.OnClick(a.showWindow)
	tray.OnDoubleClick(a.showWindow)

	menu := a.wails.NewMenu()
	menu.Add("Quit").OnClick(func(*application.Context) { a.quit() })
	tray.SetMenu(menu)

	trayIcon = tray
	a.trayOn.Store(true)
}

// applyTrayVisibility follows a HideTray draft the settings window has just
// saved. The flag moves before the icon: a close that lands beside the change
// has to quit rather than tuck the window into a tray the user just asked not to
// have - which is the one thing this app cannot recover from by itself.
func applyTrayVisibility(a *App, hide bool) {
	switch {
	case hide:
		a.trayOn.Store(false)
		trayIcon.Hide()
	case trayIcon == nil:
		createTray(a)
	default:
		a.trayOn.Store(true)
		trayIcon.Show()
	}
}
