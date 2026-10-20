package main

import (
	"embed"
	"io/fs"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wwricu/bad-proxy-gui/internal/platform"
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
// out. The status bar's fixed parts - two actions and four readouts holding
// their longest words - ask for 455 px of client width; whatever the page has
// left of that is the message line. The window is wider than the bar because
// the frame is charged first, and the frame is not the same at every display
// scale: this machine at 175% hands the page 475 px of a 488 px window, so the
// line has 20 px to say something in, and a runner at 100% takes more frame out
// of the same 488 and leaves the line less.
//
// Neither number is taken on trust. The self-test sizes a real window to the
// floor and reads back both what the bar demands and what the page was given,
// because the last time this arithmetic was written down by hand it went stale
// and the floor sat 2 px narrower than the bar for a whole release.
//
// The height floor is mainWindowMinRows node rows plus the bar: a list shorter
// than that is unusable to click in, whatever it costs an empty list in bare
// window. A window height is not a client height - the caption and the border
// are charged before the page sees anything - so the floor counts them too, and
// the self-test reads the list and the real chrome back rather than trusting
// this sum.
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
	mainWindowMinW  = 488
	mainWindowRowH  = 63 // one node row: 13+14+8+14+13+1 px
	mainWindowListH = 4  // the gap .node-list keeps from the window's top edge
	mainWindowBarH  = 36
	// mainWindowChromeH is the gap between the page's pixels and the units this
	// platform counts a window's Height and MinHeight in - per platform, and the
	// one shared 44 it replaces left macOS able to drag a caption below its own
	// floor. The spec files say why; the self-test reads the real page back
	// rather than trusting either number.
	mainWindowChromeH = platform.MainWindowChromeH
	mainWindowMinRows = 4 // the floor is this many node rows tall
)

// mainWindowSize returns the initial width/height and the minimums, each in the
// units this platform's convention counts (see mainWindowChromeH). Width is
// fixed (the bar decides it); height grows with the number of saved nodes so
// the list always has room for one extra row, and never less than the floor.
func mainWindowSize(nodeCount int) (w, h, minW, minH int) {
	w = mainWindowMinW
	minW = mainWindowMinW
	inside := func(rows int) int {
		return mainWindowBarH + mainWindowListH + rows*mainWindowRowH
	}
	minH = inside(mainWindowMinRows) + mainWindowChromeH
	h = inside(nodeCount + 1) + mainWindowChromeH
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
		logf("assets: %v", err)
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

	// snapshot, not app.st: the state belongs to mu, and reading it here should
	// not depend on no binding goroutine existing yet.
	nodeCount := len(app.snapshot().Nodes)
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
		if app.trayOn && !app.quitting.Load() {
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
		logf("run loop ended: %v", err)
	}
}
