//go:build darwin

package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/systray"
)

// customizeTrayTap makes the macOS menu-bar icon's left-click run our activating
// showWindow. Fyne's SetSystemTrayWindow installs a default tap handler that only
// calls glw.Show, which leaves an accessory app's window behind everything; we replace
// it with showWindow (becomeForeground + Show + RequestFocus). We also restore the
// close-to-tray intercept that SetSystemTrayWindow replaced with a plain Hide, so
// closing still drops the app to accessory (hiding the Dock icon).
//
// This mirrors Fyne's own handler exactly (a fyne.Do callback invoked from the systray
// left-click event), so it runs on the AppKit/Fyne main thread where the NSApp calls in
// becomeForeground are safe.
func customizeTrayTap(c *controller) {
	systray.SetOnTapped(func() { fyne.Do(c.showWindow) })
	c.win.SetCloseIntercept(c.hideToTray)
}
