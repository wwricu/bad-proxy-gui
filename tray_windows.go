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
