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
