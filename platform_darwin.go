//go:build darwin

package main

// hasTray reports that a menu-bar item is available. On macOS it is the only
// reopen affordance once the window is closed and the Dock icon is dropped,
// so it cannot be hidden.
const hasTray = true

const trayCanHide = false
