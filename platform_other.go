//go:build !windows && !darwin

package main

// hideOnCloseToTray is false on Linux/BSD: no verified tray integration, so the
// window close is left to Fyne's default (quit) rather than hiding invisibly.
const hideOnCloseToTray = false

// hasTray is false on Linux/BSD: no tray/menu-bar icon option is offered.
const hasTray = false
