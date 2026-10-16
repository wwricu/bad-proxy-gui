//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>

// becomeForeground shows the Dock icon, brings the app front, and orders its
// windows key+front. Reopening from the accessory state needs the explicit window
// ordering: after setActivationPolicy+activate the app becomes active (Dock appears),
// but Fyne's win.Show alone can leave the previously-hidden NSWindow off-screen.
static void becomeForeground(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	[NSApp activateIgnoringOtherApps:YES];
	for (NSWindow *w in [NSApp windows]) {
		[w makeKeyAndOrderFront:nil];
	}
}

// becomeBackground hides the Dock icon, leaving only the menu-bar item.
static void becomeBackground(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}
*/
import "C"

func becomeForeground() { C.becomeForeground() }
func becomeBackground() { C.becomeBackground() }
