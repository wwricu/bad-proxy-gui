//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#import <stdlib.h>

// becomeForeground shows the Dock icon and activates the app, then orders ONLY the
// main window (matched by title) key+front. Reopening from the accessory state needs
// the explicit ordering: after setActivationPolicy+activate the app becomes active
// (Dock appears) but Fyne's win.Show alone can leave the hidden NSWindow off-screen.
// We must NOT loop over every window — Fyne creates a hidden "SystrayMonitor" helper
// window (whose close is intercepted as Quit), so surfacing it flashes an empty window
// and closing it terminates the app.
static void becomeForeground(const char* title) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	[NSApp activateIgnoringOtherApps:YES];
	if (title == NULL) {
		return;
	}
	NSString* want = [NSString stringWithUTF8String:title];
	for (NSWindow* w in [NSApp windows]) {
		if ([[w title] isEqualToString:want]) {
			[w makeKeyAndOrderFront:nil];
		}
	}
}

// becomeBackground hides the Dock icon, leaving only the menu-bar item.
static void becomeBackground(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}
*/
import "C"

import "unsafe"

func becomeForeground(title string) {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	C.becomeForeground(ct)
}

func becomeBackground() { C.becomeBackground() }
