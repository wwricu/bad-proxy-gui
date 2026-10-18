//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#import <stdlib.h>
#import <string.h>

static char* gReopenTitle = NULL;
static id gReopenObserver = nil;

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

// becomeBackground hides the Dock icon, leaving only the menu-bar item.
static void becomeBackground(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}

// onAppBecomeActive is the reopen recovery. Double-clicking the app in Finder
// after the window was closed re-activates the process and macOS brings the
// Dock tile back, but nothing re-shows the window: Wails' darwin layer has no
// reopen/shouldHandleReopen handling and its app delegate has been replaced by
// the menu-bar library's. Watching the activation notification inside ObjC
// needs no Go callback and no extra thread.
static void onAppBecomeActive(void) {
	if (gReopenTitle == NULL) {
		return;
	}
	if ([NSApp activationPolicy] == NSApplicationActivationPolicyRegular) {
		NSWindow* w = findMainWindow(gReopenTitle);
		if (w != nil && [w isVisible]) {
			return;  // ordinary switch back from another app
		}
	}
	showMainWindow(gReopenTitle);
}

// watchReopen copies the window title for onAppBecomeActive and registers the
// activation observer once. Must run on the main thread.
static void watchReopen(const char* title) {
	free(gReopenTitle);
	gReopenTitle = title == NULL ? NULL : strdup(title);
	if (gReopenObserver == nil) {
		id observer = [[NSNotificationCenter defaultCenter]
			addObserverForName:NSApplicationDidBecomeActiveNotification
			object:nil
			queue:[NSOperationQueue mainQueue]
			usingBlock:^(NSNotification* note) { onAppBecomeActive(); }];
		gReopenObserver = [observer retain];
	}
}
*/
import "C"

import "unsafe"

// The three entry points below touch NSApp and its windows, so they hop to the
// main thread before calling in: they are reached from tray callbacks and from
// Wails' lifecycle hooks, neither of which runs there.
func becomeForeground(title string) {
	ct := C.CString(title)
	runOnMainThread(func() {
		defer C.free(unsafe.Pointer(ct))
		C.showMainWindow(ct)
	})
}

func becomeBackground() {
	runOnMainThread(func() { C.becomeBackground() })
}

// watchReopen arms the "double-click the app again" recovery. The ObjC side
// strdup()s the title into process-lifetime state, so the CString here is only
// a transport and is freed on return.
func watchReopen(title string) {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	runOnMainThread(func() { C.watchReopen(ct) })
}
