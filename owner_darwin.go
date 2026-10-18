//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#import <dispatch/dispatch.h>

// windowKeepAbove makes owner the AppKit owner of window: AppKit keeps it
// ordered above the owner for as long as it lives, and leaves both windows
// clickable. This is the relationship a panel has to the document window that
// opened it. It is deliberately not a sheet - beginSheet would staple the form
// to the main window's title bar and take the list out of reach while it is
// open.
//
// The block is the whole reason for the dispatch: this is called from a binding
// goroutine, and AppKit windows are main-thread only.
static void windowKeepAbove(void* windowPtr, void* ownerPtr) {
	if (windowPtr == NULL || ownerPtr == NULL) {
		return;
	}
	NSWindow* window = (NSWindow*)windowPtr;
	NSWindow* owner = (NSWindow*)ownerPtr;
	dispatch_async(dispatch_get_main_queue(), ^{
		[window setOwner:owner];
	});
}
*/
import "C"

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

func keepAbove(win application.Window, owner application.Window) {
	hwnd := win.NativeWindow()
	ownerHwnd := owner.NativeWindow()
	if hwnd == nil || ownerHwnd == nil {
		return
	}
	C.windowKeepAbove(hwnd, ownerHwnd)
}
