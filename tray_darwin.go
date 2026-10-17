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

// addTrayQuit keeps the menu-bar icon menu-free on macOS, matching the
// behaviour the user signed off on: the right click must pop nothing. A
// registered (silent) OnRClick handler is what suppresses the library's
// default "right click shows the menu". Quit lives in the app menu (Cmd+Q).
func addTrayQuit(onQuit func()) {
	systray.SetOnRClick(func(systray.IMenu) {})
}
