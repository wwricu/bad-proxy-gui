//go:build windows

package main

// hideOnCloseToTray: Windows keeps a notification-area tray, so closing the
// window hides to the tray and the proxy keeps running.
const hideOnCloseToTray = true

// hasTray reports that a notification-area icon is available.
const hasTray = true

// trayQuitMenuItem is true on Windows: the tray icon is left-click-to-open, but
// with no Dock there is no other way to quit, so the right-click menu keeps a
// Quit item.
const trayQuitMenuItem = true
