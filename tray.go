package main

import (
	"github.com/energye/systray"
)

// trayMenu builds the menu-bar / notification-area icon: a left-click or
// double-click reopens the window; the menu itself only carries Quit.
func trayMenu(onOpen, onQuit func()) {
	systray.SetIcon(trayIconBytes)
	systray.SetTitle("Bad Proxy")
	systray.SetTooltip("Bad Proxy")
	systray.SetOnClick(func(systray.IMenu) { onOpen() })
	systray.SetOnDClick(func(systray.IMenu) { onOpen() })
	quit := systray.AddMenuItem("Quit", "Quit Bad Proxy")
	quit.Click(onQuit)
}
