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

// resizeFrameHeight is the same frame again, because resizing a window uses the
// whole-window convention that creating one does. It is only a starting guess:
// the frame is 36 px on a display at 175% scaling and 39 at 100%, and the page
// measures the difference and asks for the rest.
const resizeFrameHeight = windowFrameHeight

// The two forms' client heights: what each window opens at, before the page has
// measured itself and asked for the height it actually needs.
//
// They are the worst case over display scales, not the number this machine
// reads. Two things move with the scale: the frame is rounded from physical
// pixels, so a 100%-scaled display loses three more client pixels than the 175%
// one these were first measured at; and every 14 px gap in the form rounds
// separately, so the same page is a few pixels taller at 100% than at 175%.
// Both were found by the self-test run on the CI runner, which is 100%.
//
// Getting them too large is now a one-frame resize rather than a permanent band
// of window, and too small a scroll for the same moment - but the self-test
// still reads these numbers back, so a form that has grown since they were
// measured fails the run instead of flickering in the release.
const (
	nodeFormHeight     = 478
	settingsFormHeight = 347
)
