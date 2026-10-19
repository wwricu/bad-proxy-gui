//go:build !windows && !darwin

package main

// hasTray is false on Linux/BSD: no verified tray integration, so the window
// close is left to quit rather than hiding invisibly.
const hasTray = false

const trayCanHide = false

// trayTitle only has to exist for setupTray to compile; the tray never runs
// where hasTray is false.
const trayTitle = "Bad Proxy"

// trayHasMenu only has to exist for setupTray to compile; the tray never runs
// where hasTray is false.
const trayHasMenu = false

// windowFrameHeight is unmeasured where there is no self-test run to measure it
// in, and the form windows have never been shipped here.
const windowFrameHeight = 0

// resizeFrameHeight is unmeasured for the same reason; the page measures what
// the frame actually cost and asks again for the difference, so being wrong
// here costs one extra resize rather than a window that does not fit.
const resizeFrameHeight = 0

// The form heights are the macOS numbers: nothing has measured them here, and
// like the frame above, they are only what a form window opens at before the
// page asks for the height it needs.
const (
	nodeFormHeight     = 462
	settingsFormHeight = 346
)
