//go:build windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// setupTray creates the notification-area icon. Wails owns the tray loop, so
// this runs on the startup path rather than in a goroutine of its own.
//
// A left click (or a double click) reopens the window and the right click pops
// the menu carrying Quit.
//
// macOS has its own version in tray_darwin.go: the item there has to be AppKit's
// own so System Settings can manage it.
func setupTray(a *App, wails *application.App) {
	if trayCanHide && a.st.HideTray {
		return
	}
	a.trayOn = true

	tray := wails.SystemTray.New()
	applyTrayIcon(tray)
	tray.SetLabel("Bad Proxy")
	tray.SetTooltip("Bad Proxy")
	tray.AttachWindow(a.mainWindow)
	tray.OnClick(a.showWindow)
	tray.OnDoubleClick(a.showWindow)

	menu := wails.NewMenu()
	menu.Add("Quit").OnClick(func(*application.Context) { a.quit() })
	tray.SetMenu(menu)
	a.tray = tray
}
