//go:build windows

package main

// hasTray reports that a notification-area icon is available; closing the
// window hides to it and the proxy keeps running.
const hasTray = true

// trayCanHide is true on Windows: the tray icon may be hidden. With it
// hidden, closing the window falls through to quit rather than hiding
// invisibly.
const trayCanHide = true

// trayTitle is the label the notification-area icon carries.
const trayTitle = "Bad Proxy"

// trayHasMenu is true on Windows: the right click pops a menu carrying Quit.
const trayHasMenu = true

// windowFrameHeight is what the caption and border cost: Wails' window Height
// is the whole window here, so a form's client height is this much less than
// the number handed to it. Measured in the self-test run, which also charges
// 13 px of width to the frame.
const windowFrameHeight = 36

// The two forms' client heights, measured on this platform.
//
// They are the worst case over display scales, not the number this machine
// reads. Two things move with the scale: the frame is rounded from physical
// pixels, so a 100%-scaled display loses three more client pixels than the 175%
// one these were first measured at; and every 14 px gap in the form rounds
// separately, so the same page is a few pixels taller at 100% than at 175%.
// Both were found by the self-test run on the CI runner, which is 100%.
//
// Too short scrolls, which is the failure worth paying a band of window under
// the buttons to avoid.
const (
	nodeFormHeight     = 478
	settingsFormHeight = 347
)
