//go:build windows

package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// trayIconBytes is the notification-area icon. Windows requires ICO bytes;
// PNG data makes Shell_NotifyIcon fail.
//
//go:embed build/windows/icon.ico
var trayIconBytes []byte

// applyTrayIcon uses the app icon itself: the notification area letterboxes an
// oversized image instead of stretching it to the bar, and keeps its colour.
func applyTrayIcon(tray *application.SystemTray) {
	tray.SetIcon(trayIconBytes)
}
