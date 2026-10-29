//go:build darwin

// The menu-bar item is AppKit's own NSStatusItem, not Wails' SystemTray. Only
// the former appears in System Settings, which is where the user can turn a
// menu-bar item off; the Wails one is invisible to it. tray_windows.go, which
// uses Wails, is the other platform's version.
//
// The Objective-C is in tray_darwin.m rather than in the preamble below because
// //export only allows declarations there - the same split
// internal/platform/mainthread_darwin.m uses, and for the same reason.
package main

/*
#cgo LDFLAGS: -framework Cocoa

void* badproxyCreateStatusItem(const unsigned char* imgData, int imgLen);
void badproxySetStatusItemVisible(int visible);
int badproxyStatusItemVisible(void);
*/
import "C"

import (
	_ "embed"
	"unsafe"

	"github.com/wwricu/bad-proxy-gui/internal/platform"
)

// trayTemplateBytes is the menu-bar glyph: flat black on transparent, which is
// what a template image is masked from.
//
//go:embed build/traytemplate.png
var trayTemplateBytes []byte

// trayClick is what a click on the menu-bar item does. AppKit delivers it on
// the main thread, so it is set before the item exists and never changed after.
var trayClick func()

//export badproxyTrayClicked
func badproxyTrayClicked() {
	if trayClick != nil {
		platform.PostToMainThread(trayClick)
	}
}

// createNativeTray puts the template glyph in the menu bar. A dataset AppKit
// cannot decode, or a status bar that refuses the item, both come back false.
func createNativeTray(imgData []byte) bool {
	if len(imgData) == 0 {
		return false
	}
	return C.badproxyCreateStatusItem((*C.uchar)(unsafe.Pointer(&imgData[0])),
		C.int(len(imgData))) != nil
}

// setupTray puts the menu-bar item in place unless the saved setting has it
// away.
//
// trayOn is true either way. It says that closing the window hides it rather
// than ends the app, and here that is what happens with or without a glyph on
// the bar: what brings the window back is launching the app again, which
// SingleInstance hands to the instance that is already running. The item is a
// shortcut to that, not the only route, so hiding it strands nothing.
func setupTray(a *App) {
	a.trayOn.Store(true)
	if a.snapshot().HideTray {
		return
	}
	createTray(a)
}

// createTray asks for the item. AppKit keeps one per app, so this is also the
// way back when the setting is turned off again mid-run: an item that is already
// there comes back rather than stacking a second one.
func createTray(a *App) {
	trayClick = a.showWindow
	if !createNativeTray(trayTemplateBytes) {
		trayClick = nil
	}
}

// applyTrayVisibility follows a HideTray draft the settings window has just
// saved.
//
// The status item belongs to AppKit, so the work is done on the main thread and
// this waits for it: the binding that got here is holding "Applying..." for the
// user anyway, and returning before the item had moved would report a change
// that had not happened yet.
func applyTrayVisibility(a *App, hide bool) {
	platform.RunOnMainThread(func() {
		if !hide {
			createTray(a)
		}
		setNativeTrayVisible(!hide)
	})
}

// setNativeTrayVisible shows or hides the item, and says nothing when there is
// no item to show. Call it on the main thread.
func setNativeTrayVisible(visible bool) {
	if visible {
		C.badproxySetStatusItemVisible(1)
		return
	}
	C.badproxySetStatusItemVisible(0)
}

// nativeTrayVisible is the item's own answer - 1 shown, 0 hidden, -1 when there
// has never been one - read on the main thread because that is where it lives.
func nativeTrayVisible() int {
	var seen int
	platform.RunOnMainThread(func() { seen = int(C.badproxyStatusItemVisible()) })
	return seen
}
