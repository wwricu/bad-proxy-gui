//go:build !windows && !darwin

package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// trayIconBytes is the notification-area icon (PNG on Linux/BSD).
//
//go:embed build/appicon.png
var trayIconBytes []byte

func applyTrayIcon(tray *application.SystemTray) {
	tray.SetIcon(trayIconBytes)
}
