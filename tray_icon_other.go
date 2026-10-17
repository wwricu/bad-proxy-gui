//go:build !windows

package main

import _ "embed"

// trayIconBytes is the notification-area icon (PNG on macOS/Linux).
//
//go:embed build/appicon.png
var trayIconBytes []byte
