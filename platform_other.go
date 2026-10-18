//go:build !windows && !darwin

package main

// hasTray is false on Linux/BSD: no verified tray integration, so the window
// close is left to quit rather than hiding invisibly.
const hasTray = false

const trayCanHide = false

// trayTitle only has to exist for trayMenu to compile; startTray never runs
// where hasTray is false.
const trayTitle = "Bad Proxy"
