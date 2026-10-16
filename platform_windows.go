//go:build windows

package main

// hideOnCloseToTray: Windows keeps a notification-area tray, so closing the
// window hides to the tray and the proxy keeps running.
const hideOnCloseToTray = true
