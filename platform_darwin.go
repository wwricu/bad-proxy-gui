//go:build darwin

package main

// hideOnCloseToTray: closing the window hides it to the menu-bar icon and the
// proxy keeps running; the menu-bar icon's left-tap (or the Dock) brings it back.
const hideOnCloseToTray = true

// hasTray reports that a menu-bar icon is available.
const hasTray = true

// trayQuitMenuItem is false on macOS: the menu-bar icon is click-to-open only.
// A normal (non-LSUIElement) app shows a Dock icon, and the user quits from there.
const trayQuitMenuItem = false
