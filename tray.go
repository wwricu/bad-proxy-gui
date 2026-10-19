package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// setupTray creates the notification-area / menu-bar item. Wails owns the tray
// loop, so this runs on the startup path rather than in a goroutine of its own.
//
// A left click (or a double click) reopens the window; what the right click
// does is platform policy, expressed by trayHasMenu.
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
		// macOS draws a menu for a right click as soon as the item has one. The
		// signed-off menu-bar look has no menu at all, and Quit lives in the app
		// menu (Cmd+Q), so the right click is handled by doing nothing.
		tray.SetMenu(nil)
		tray.OnRightClick(func() {})
	}
	a.tray = tray
}
