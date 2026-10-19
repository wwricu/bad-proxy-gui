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
// The 6 px over the measured content is not slack to look good in: the frame is
// rounded from physical pixels, so a 100%-scaled display loses three more
// client pixels than a 175% one, and the editor's nine fields fit exactly at
// the higher scale and scroll at the lower. The self-test run on the CI
// runner - 100% - is what found it.
const (
	nodeFormHeight     = 468
	settingsFormHeight = 347
)
