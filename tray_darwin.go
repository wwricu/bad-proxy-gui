//go:build darwin

// The menu-bar item is AppKit's own NSStatusItem, not Wails' SystemTray. Only
// the former appears in System Settings, which is where the user turns a
// menu-bar item off; the Wails one is invisible to it. tray.go, which uses
// Wails, is built for every other platform.
//
// The Objective-C is in tray_darwin.m rather than in the preamble below because
// //export only allows declarations there - the same split mainthread_darwin.m
// uses, and for the same reason.
package main

/*
#cgo LDFLAGS: -framework Cocoa

void* badproxyCreateStatusItem(const unsigned char* imgData, int imgLen);
*/
import "C"

import (
	_ "embed"
	"unsafe"
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
		postToMainThread(trayClick)
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

// setupTray is the darwin counterpart of the one in tray_windows.go. The second
// parameter is ignored - it exists only to match that signature at the call
// site in main.go.
//
// There is no HideTray check here: the menu-bar item is the only reopen
// affordance once the window is closed and the Dock icon is dropped, so it
// cannot be hidden. The user turns it off in System Settings instead.
func setupTray(a *App, _ interface{}) {
	trayClick = a.showWindow
	if !createNativeTray(trayTemplateBytes) {
		trayClick = nil
		return
	}
	a.trayOn = true
}
