//go:build darwin

package main

import (
	"github.com/energye/systray"
)

// startTray piggybacks on the app's existing NSApplication run loop (Wails
// owns the main thread). Taking ownership here would SIGSEGV inside AppKit,
// so the tray registers with an external loop and returns immediately.
func startTray(onOpen, onQuit func()) {
	start, _ := systray.RunWithExternalLoop(func() { trayMenu(onOpen, onQuit) }, nil)
	start()
}
