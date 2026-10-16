//go:build darwin

package main

// hideOnCloseToTray: macOS is a menu-bar app (LSUIElement), so closing the
// window hides it to the status bar and the proxy keeps running.
const hideOnCloseToTray = true

// hasTray reports that a menu-bar / notification-area icon is available.
const hasTray = true
