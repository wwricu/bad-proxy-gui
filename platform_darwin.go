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
