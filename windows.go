package main

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Form windows are the settings window and the node editor: ordinary webview
// windows of their own, sized to the form they hold, so neither of them ever
// needs a scrollbar and neither of them covers the list it is talking about.
//
// The delete confirmation is deliberately not one of these. It is one sentence
// and two buttons in the window that owns the row, and a title bar for a single
// question is all frame and no content.
const (
	settingsWindowName = "settings"
	nodeWindowPrefix   = "node-"

	// Both numbers are the form's own height plus the window frame, taken from
	// the self-test run rather than guessed: on Windows the frame costs 13 px of
	// width and 36 px of height, and the two forms need 416 and 312 px of client
	// height - the self-test fails if either then shows a band of empty window
	// above its own buttons.
	formWindowWidth      = 460
	settingsWindowHeight = 348
	nodeWindowHeight     = 452
)

// openFormWindow shows one form window, or refuses to. There is never a second
// one: the editor and the settings window both hold the whole GUIState, so two
// of them open at once would each be about to write back a stale copy. A repeat
// request raises the window that already has the floor and has it blink, which
// is how Windows answers a duplicate request against a window that already has
// the user's answer outstanding.
func (a *App) openFormWindow(name, title, url string, height int) {
	if existing := a.anyFormWindow(); existing != nil {
		a.nudgeFormWindow(existing)
		return
	}
	win := a.wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          name,
		Title:         title,
		Width:         formWindowWidth,
		Height:        height,
		DisableResize: true,
		URL:           url,
	})
	// A form belongs to the window it was called from, so the window manager
	// keeps it above that one. Without this the list can be clicked into and
	// bury the form it is supposed to be waiting for.
	keepAbove(win, a.mainWindow)
	// The list is in another window, so it has to be told to reread once this
	// one is done writing. The hook runs for the native cross as well as for the
	// Cancel button, which is the point: both are the window finishing its work.
	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.mainWindow.EmitEvent("state-changed")
	})
}

// nudgeFormWindow is the answer to a form request that cannot be honoured:
// raise the window that already holds a form and make it blink for itself.
//
// The blink is the operating system's own, not an animation of ours. This is
// the effect every Win32 modal dialog gives when you click outside it - the
// dialog manager calls FlashWindowEx (caption plus taskbar button) rather than
// moving the window, and so do we. macOS gets the same intent through
// requestUserAttention, which bounces the Dock icon.
func (a *App) nudgeFormWindow(win application.Window) {
	win.Show()
	win.Focus()
	flashForAttention(win)
}

func (a *App) openSettingsWindow() {
	a.openFormWindow(settingsWindowName, "Settings", "/settings.html", settingsWindowHeight)
}

// openNodeWindow edits the node at index, or adds one when index is negative.
// The index travels in the URL: the editor reads the same state the list does,
// so there is no second copy of a draft to keep in sync.
func (a *App) openNodeWindow(index int) error {
	if index >= 0 && index >= len(a.st.Nodes) {
		return fmt.Errorf("invalid index: %d", index)
	}
	name := nodeWindowPrefix + "new"
	title := "Add node"
	url := "/node.html"
	if index >= 0 {
		name = fmt.Sprintf("%s%d", nodeWindowPrefix, index)
		title = "Edit node"
		url = fmt.Sprintf("/node.html?index=%d", index)
	}
	a.openFormWindow(name, title, url, nodeWindowHeight)
	return nil
}

// formByName returns an open form window with the given name, if there is one.
// A name the window manager no longer knows about is simply not open.
func (a *App) formByName(name string) application.Window {
	win, ok := a.wails.Window.GetByName(name)
	if !ok {
		return nil
	}
	return win
}

// anyFormWindow returns the form window that is open, if there is one. The main
// window is named "main" and never matches, so this is only ever the editor or
// the settings window.
func (a *App) anyFormWindow() application.Window {
	for _, win := range a.wails.Window.GetAll() {
		if n := win.Name(); n == settingsWindowName || strings.HasPrefix(n, nodeWindowPrefix) {
			return win
		}
	}
	return nil
}

// hideFormWindow and showFormWindow move the form with the list it belongs to.
// A window hidden by the app is not taken out of the window list, so the form is
// still found again on the way back - which matters, because it is the one
// window holding a draft the user has not finished.
func (a *App) hideFormWindow() {
	if form := a.anyFormWindow(); form != nil {
		form.Hide()
	}
}

func (a *App) showFormWindow() {
	form := a.anyFormWindow()
	if form == nil {
		return
	}
	form.Show()
	// The form is the window still waiting for an answer, so it takes the
	// floor when the app comes back.
	form.Focus()
}
