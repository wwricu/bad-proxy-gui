//go:build darwin

package main

// Hide-on-close stays accessory (Dock icon drops); the menu-bar icon is the
// reopen affordance.
const hideOnCloseToTray = true

// hasTray reports that a menu-bar icon is available.
const hasTray = true

// trayCanHide is false on macOS: the menu-bar item is the only way back once the
// window is closed (Dock is dropped on hide), so it cannot be hidden too.
const trayCanHide = false

// The menu-bar icon carries NO menu — a plain click reopens the window.
const trayOpenMenuItem = false
const trayQuitMenuItem = false

// trayUsesNativeClickOpen is true on macOS: SetSystemTrayWindow makes a left-click
// fire our tap handler. We override that handler (customizeTrayTap) to run showWindow
// so the app actually activates and surfaces (Fyne's default just shows the window,
// which can leave an accessory app's window behind everything), and to keep our
// close-to-tray intercept that SetSystemTrayWindow otherwise replaces with a plain Hide.
const trayUsesNativeClickOpen = true
