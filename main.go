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
// out: the status bar, whose fixed parts - two actions and four readouts holding
// their longest words - need 483 px of *client* width, leaving the rest for the
// message line. That number is what the self-test measured on this machine, and
// text does not scale evenly, so a display at another scaling can ask for a few
// pixels more. The floor is it plus the frame, which is 13 px wide at 175% and
// 16 at 100%, plus that margin. Setting the floor by the thinner frame instead
// leaves a machine at 100% with the facts hanging over the edge, which is what
// the width checks look for.
//
// The height floor is three node rows plus the bar: a list shorter than that is
// unusable to click in, whatever it costs an empty list in bare window. A window
// height is not a client height - the caption and the border are charged before
// the page sees anything - so the floor counts them too, and the self-test reads
// the list back to check it got three whole rows rather than trusting this sum.
//
// The default matches the floor on purpose: the message line is empty most of
// the time, so every pixel of slack between the actions and the facts shows as
// a hole in the middle of the bar.
const (
	// The bar decides this, not the form windows: with each fact on one line the
	// four of them plus the two buttons need what they need, and a window
	// narrower than that clips the facts off the right edge. It lands above
	// formWindowWidth anyway, so the list window is never narrower than a dialog
	// that opens out of it.
	mainWindowMinW    = 488
	mainWindowRowH    = 63 // one node row: 13+14+8+14+13+1 px
	mainWindowListH   = 4  // the gap .node-list keeps from the window's top edge
	mainWindowBarH    = 36
	mainWindowChromeH = 44 // caption and border: 31 on mac, 39 on Windows at 100%
	mainWindowMinRows = 4  // the floor is this many node rows tall
)

// mainWindowSize returns the initial width/height and the minimums. Width is
// fixed (the bar decides it); height grows with the number of saved nodes so
// the list always has room for one extra row, and never less than the floor.
func mainWindowSize(nodeCount int) (w, h, minW, minH int) {
	w = mainWindowMinW
	minW = mainWindowMinW
	inside := func(rows int) int {
		return mainWindowChromeH + mainWindowBarH + mainWindowListH + rows*mainWindowRowH
	}
	minH = inside(mainWindowMinRows)
	h = inside(nodeCount + 1)
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
