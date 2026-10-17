//go:build windows

package main

import _ "embed"

// trayIconBytes is the notification-area icon. Windows requires ICO bytes;
// PNG data makes Shell_NotifyIcon fail.
//
//go:embed build/windows/icon.ico
var trayIconBytes []byte
