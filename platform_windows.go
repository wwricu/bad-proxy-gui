//go:build windows

package main

// hideOnCloseToTray: Windows keeps a notification-area tray, so closing the
// window hides to the tray and the proxy keeps running.
const hideOnCloseToTray = true

// hasTray reports that a menu-bar / notification-area icon is available.
const hasTray = true
