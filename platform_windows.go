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
