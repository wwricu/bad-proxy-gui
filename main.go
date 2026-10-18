package main

import (
	"embed"
	"io/fs"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend
var bundled embed.FS

// windowTitle is the native window title; the macOS activation helpers match
// the main window by it, so it must stay in sync with the window's Title.
const windowTitle = "Bad Proxy GUI"

// mainWindowName identifies the one window that always exists. The node editor
// and the settings sheet are drawn inside it and never take this name.
const mainWindowName = "main"

// The window is sized by the two things that must fit in it: the status bar,
// whose fixed parts - two actions and four readouts holding their longest words
// - need 533 px, leaving the rest for the message line; and the tallest sheet,
// the node editor with every optional field visible, which is 433 px high. The
// self-test drives these same numbers and reads the widths back.
const (
	mainWindowWidth  = 800
	mainWindowHeight = 600
	mainWindowMinW   = 660
	mainWindowMinH   = 540
)

func main() {
	// The tray is declared before the run loop starts, and whether it exists
	// depends on the saved state, so the store is read here rather than inside
	// the service's startup.
	app := newApp()

	frontend, err := fs.Sub(bundled, "frontend")
	if err != nil {
		println("Error:", err.Error())
		return
	}

	wails := application.New(application.Options{
		Name:        windowTitle,
		Description: "Bad Proxy client",
		Mac: application.MacOptions{
			// Closing the window hides it to the menu bar, so the last window
			// closing is not a request to leave.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.wwricu.bad-proxy-gui",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				app.showWindow()
			},
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(frontend),
		},
		Services:   []application.Service{application.NewService(app)},
		OnShutdown: app.shutdown,
	})

	win := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      mainWindowName,
		Title:     windowTitle,
		Width:     mainWindowWidth,
		Height:    mainWindowHeight,
		MinWidth:  mainWindowMinW,
		MinHeight: mainWindowMinH,
		URL:       "/",
	})

	app.wails = wails
	app.mainWindow = win
	setupTray(app, wails)

	// A close is a hide while the tray is there to bring the window back. The
	// hook runs before Wails destroys anything, so Cancel() has to be the one
	// that says no.
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if app.trayOn && !app.quitting {
			e.Cancel()
			becomeBackground()
			win.Hide()
		}
	})

	// Reopening on macOS: the Dock tile and the menu bar item both have to bring
	// back the window that was merely hidden.
	wails.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		app.showWindow()
	})

	if err := wails.Run(); err != nil {
		println("Error:", err.Error())
	}
}
