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
// and the settings window are separate windows and never take this name.
const mainWindowName = "main"

// The main window is sized by its own content now that the forms have moved
// out: the status bar, whose fixed parts - two actions and four readouts
// holding their longest words - need 533 px of *client* width at this machine's
// scaling, leaving the rest for the message line. Text does not scale evenly, so
// the same bar needs 546 px on a display at 100%, and the floor is set by that
// worst case plus the frame, which is 13 px wide at 175% and 16 at 100%. The
// height floor is one node row plus the bar, so an empty list does not look like
// a band of bare window under the last item.
//
// The default matches the floor on purpose: the message line is empty most of
// the time, so every pixel of slack between the actions and the facts shows as
// a hole in the middle of the bar.
const (
	mainWindowMinW   = 560
	mainWindowRowH   = 63 // one node row: 13+14+8+14+13+1 px
	mainWindowBarH   = 36
)

// mainWindowSize returns the initial width/height and the minimums. Width is
// fixed (the bar decides it); height grows with the number of saved nodes so
// the list always has room for one extra row.
func mainWindowSize(nodeCount int) (w, h, minW, minH int) {
	w = mainWindowMinW
	minW = mainWindowMinW
	minH = mainWindowBarH + mainWindowRowH // one node + the bar
	h = mainWindowBarH + (nodeCount+1)*mainWindowRowH
	if h < minH {
		h = minH
	}
	return
}

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

	nodeCount := len(app.st.Nodes)
	w, h, minW, minH := mainWindowSize(nodeCount)
	win := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      mainWindowName,
		Title:     windowTitle,
		Width:     w,
		Height:    h,
		MinWidth:  minW,
		MinHeight: minH,
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
			app.hideToTray()
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
