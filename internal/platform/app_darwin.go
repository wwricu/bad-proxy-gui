//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#import <stdlib.h>

// findMainWindow returns the app's main window, matched by title. Matching by
// title rather than walking every window matters: helper windows (e.g. the
// menu-bar status item's) must not be surfaced or intercepted.
static NSWindow* findMainWindow(const char* title) {
	if (title == NULL) {
		return nil;
	}
	NSString* want = [NSString stringWithUTF8String:title];
	for (NSWindow* w in [NSApp windows]) {
		if ([[w title] isEqualToString:want]) {
			return w;
		}
	}
	return nil;
}

// showMainWindow restores the Dock icon and re-surfaces the hidden window.
// Restoring the policy makes the Dock icon appear, but a window that was
// ordered out still needs makeKeyAndOrderFront, so both are always done
// together. Reopening from the accessory state needs the explicit ordering:
// after setActivationPolicy+activate the app becomes active (Dock appears),
// yet a hidden NSWindow stays hidden until it is ordered front.
static void showMainWindow(const char* title) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	[NSApp activateIgnoringOtherApps:YES];
	NSWindow* w = findMainWindow(title);
	if (w != nil) {
		[w makeKeyAndOrderFront:nil];
	}
}

// BecomeBackground hides the Dock icon, leaving only the menu-bar item.
static void BecomeBackground(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}
*/
import "C"

import "unsafe"

// The two entry points below touch NSApp and its windows, so they hop to the
// main thread before calling in: they are reached from tray callbacks and from
// Wails' lifecycle hooks, neither of which runs there.
func BecomeForeground(title string) {
	ct := C.CString(title)
	RunOnMainThread(func() {
		defer C.free(unsafe.Pointer(ct))
		C.showMainWindow(ct)
	})
}

func BecomeBackground() {
	RunOnMainThread(func() { C.BecomeBackground() })
}
