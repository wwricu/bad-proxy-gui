package main

import (
	"fmt"

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

// openFormWindow shows one form window, or raises the one already open. A
// second click on the gear must not stack another copy writing the same state,
// and two editors open at once would each be about to overwrite the other.
func (a *App) openFormWindow(name, title, url string, height int) {
	if existing := a.formByName(name); existing != nil {
		existing.Show()
		existing.Focus()
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
	// The list is in another window, so it has to be told to reread once this
	// one is done writing. The hook runs for the native cross as well as for the
	// Cancel button, which is the point: both are the window finishing its work.
	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.mainWindow.EmitEvent("state-changed")
	})
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
