//go:build windows

package main

import (
	"github.com/energye/systray"
)

// startTray owns a dedicated message loop for the tray icon; run it in a
// goroutine.
func startTray(onOpen, onQuit func()) {
	systray.Run(func() { trayMenu(onOpen, onQuit) }, nil)
}

// addTrayQuit gives the Windows icon a right-click menu carrying only Quit;
// left click reopens the window.
func addTrayQuit(onQuit func()) {
	quit := systray.AddMenuItem("Quit", "Quit Bad Proxy")
	quit.Click(onQuit)
}
