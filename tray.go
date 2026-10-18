package main

import (
	"github.com/energye/systray"
)

// trayMenu builds the menu-bar / notification-area icon: a left-click or
// double-click reopens the window; what the right-click does is platform
// policy (addTrayQuit).
func trayMenu(onOpen, onQuit func()) {
	systray.SetIcon(trayIconBytes)
	// macOS draws trayTitle as text beside the icon, which the signed-off
	// menu-bar look does not want; every other platform gets the label.
	if trayTitle != "" {
		systray.SetTitle(trayTitle)
	}
	systray.SetTooltip("Bad Proxy")
	systray.SetOnClick(func(systray.IMenu) { onOpen() })
	systray.SetOnDClick(func(systray.IMenu) { onOpen() })
	addTrayQuit(onQuit)
}
