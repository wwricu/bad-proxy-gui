//go:build !darwin

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// setupTray creates the notification-area / menu-bar item. Wails owns the tray
// loop, so this runs on the startup path rather than in a goroutine of its own.
//
// A left click (or a double click) reopens the window; what the right click
// does is platform policy, expressed by trayHasMenu.
//
// macOS has its own version in tray_darwin.go: the item there has to be AppKit's
// own so System Settings can manage it.
func setupTray(a *App, wails *application.App) {
	if !hasTray || (trayCanHide && a.st.HideTray) {
		return
	}
	a.trayOn = true

	tray := wails.SystemTray.New()
	applyTrayIcon(tray)
	if trayTitle != "" {
		tray.SetLabel(trayTitle)
	}
	tray.SetTooltip("Bad Proxy")
	tray.AttachWindow(a.mainWindow)
	tray.OnClick(a.showWindow)
	tray.OnDoubleClick(a.showWindow)

	if trayHasMenu {
		menu := wails.NewMenu()
		menu.Add("Quit").OnClick(func(*application.Context) { a.quit() })
		tray.SetMenu(menu)
	} else {
		// A tray that is given a menu shows it on right click whether or not
		// anyone asked for one, so the platforms that want no menu say so by
		// having nothing in it.
		tray.SetMenu(nil)
		tray.OnRightClick(func() {})
	}
	a.tray = tray
}
