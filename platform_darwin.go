//go:build darwin

package main

// hideOnCloseToTray: closing the window hides it to the menu-bar icon and the
// proxy keeps running; the menu-bar icon brings it back.
const hideOnCloseToTray = true

// hasTray reports that a menu-bar icon is available.
const hasTray = true

// trayCanHide is false on macOS: the app runs as an accessory (no Dock icon),
// so the menu-bar icon is the only way to reopen or quit. Hiding it would strand
// the app (Fyne has no Dock-reopen/quit hook), so "hide menu-bar icon" is not
// offered here.
const trayCanHide = false

// macOS status items act through a menu, so the icon carries Open + Quit.
const trayOpenMenuItem = true
const trayQuitMenuItem = true

// trayUsesNativeClickOpen is false on macOS: Fyne's SetSystemTrayWindow shows the
// window without activating the (accessory) app, so it can open behind everything.
// We reopen via the menu's Open item, which runs our own activating showWindow.
const trayUsesNativeClickOpen = false
