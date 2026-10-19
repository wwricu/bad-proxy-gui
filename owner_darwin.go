//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo CFLAGS: -Werror=objc-method-access
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#import <dispatch/dispatch.h>

// windowKeepAbove makes owner the AppKit parent of window: AppKit keeps it
// ordered above the parent for as long as it lives, and leaves both windows
// clickable. This is the relationship a panel has to the document window that
// opened it. It is deliberately not a sheet - beginSheet would staple the form
// to the main window's title bar and take the list out of reach while it is
// open.
//
// setParentWindow: is the only public way to ask for this. NSWindow publishes
// ownerWindow/parentWindow as read-only properties, so the setter that looks
// obvious - setOwner: - is not part of the interface, and sending it raises
// NSInvalidArgumentException on the next event loop turn. A mistyped selector
// here is a warning, not an error, which is why the build is compiled with
// -Werror=objc-method-access: a dialog that cannot be built beats one that
// kills the app the first time somebody opens it.
//
// The block is the whole reason for the dispatch: this is called from a binding
// goroutine, and AppKit windows are main-thread only. Nothing waits for it, so
// there is no re-entrancy to deadlock on - but the window can be closed before
// the main queue drains, and a block under manual reference counting does not
// keep what it captured, so it holds its own reference for the call.
static void windowKeepAbove(void* windowPtr, void* ownerPtr) {
	if (windowPtr == NULL || ownerPtr == NULL) {
		return;
	}
	NSWindow* window = (NSWindow*)windowPtr;
	NSWindow* owner = (NSWindow*)ownerPtr;
	[window retain];
	[owner retain];
	dispatch_async(dispatch_get_main_queue(), ^{
		[window setParentWindow:owner];
		[window release];
		[owner release];
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
