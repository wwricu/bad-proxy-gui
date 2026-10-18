package main

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Form windows are the settings sheet and the node editor. They are ordinary
// webview windows of their own, sized to the form they hold, which is what
// retires both the hand-built Win32 dialogs and the in-page cards that had to
// stand in for them on other platforms.
const (
	settingsWindowName = "settings"
	nodeWindowPrefix   = "node-"

	formWindowWidth = 412
	// The tallest form is the node editor with every optional field showing.
	settingsWindowHeight = 436
	nodeWindowHeight     = 460
)

// openFormWindow shows one form window, or raises the one already open: a
// second click on the gear must not stack another copy of the same sheet.
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
	// one is done writing.
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
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
