//go:build darwin

package main

// hasTray reports that a menu-bar item is available. On macOS it is the only
// reopen affordance once the window is closed and the Dock icon is dropped,
// so it cannot be hidden.
const hasTray = true

const trayCanHide = false

// trayTitle is empty on macOS: AppKit renders a status item's title as text
// next to the icon, and the menu-bar item is signed off as icon-only.
const trayTitle = ""

// trayHasMenu is false on macOS: the menu-bar item is signed off as icon-only
// with no right-click menu. Quit lives in the app menu (Cmd+Q).
const trayHasMenu = false

// windowFrameHeight is 0 on macOS: Wails hands the window's Height straight to
// -[NSWindow initWithContentRect:...], so the number is already the webview's
// own height and the title bar is extra on top of it. Measured in the self-test
// run, which reads 452 px of page out of a window of 452 and a frame of 483.
const windowFrameHeight = 0

// resizeFrameHeight is 28 here, and it is not the same convention as the one
// above. -[WebviewWindow setSize:] ends in -[NSWindow setFrame:display:animate:]
// - the whole window, title bar included - where the creation call above took
// the content rect. So the number that opened a 462 px page is the number that
// leaves a 434 px page once the window exists, and a form being resized has to
// pay the caption on top.
const resizeFrameHeight = 28

// The two forms' client heights: what each window opens at, before the page has
// measured itself and asked for the height it actually needs.
//
// They are not the Windows numbers: a text field and a popup are drawn by the
// operating system, and AppKit draws them shorter than Win32 does. The settings
// form is 39 px of content shorter here than on Windows, which is why a height
// shared between the two platforms leaves one of them either scrolling or
// holding a band of empty window under its own buttons.
const (
	nodeFormHeight     = 462
	settingsFormHeight = 302
)
