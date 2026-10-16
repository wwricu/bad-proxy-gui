//go:build windows

package main

// hideOnCloseToTray: Windows keeps a notification-area tray, so closing the
// window hides to the tray and the proxy keeps running.
const hideOnCloseToTray = true

// hasTray reports that a notification-area icon is available.
const hasTray = true

// trayCanHide is true on Windows: the tray icon may be hidden. With it hidden,
// closing the window falls through to quit rather than hiding invisibly.
const trayCanHide = true

// On Windows the tray icon left-taps open (SetSystemTrayWindow), so the menu only
// needs a right-click Quit; no "Open" item.
const trayOpenMenuItem = false
const trayQuitMenuItem = true
