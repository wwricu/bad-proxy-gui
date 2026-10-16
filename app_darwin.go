//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>

// becomeForeground shows the Dock icon and brings the app front, which is what a
// windowed launch/reopen needs. Without activating, an accessory app's window can
// open behind everything (reads as "click did nothing").
static void becomeForeground(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	[NSApp activateIgnoringOtherApps:YES];
}

// becomeBackground hides the Dock icon, leaving only the menu-bar item.
static void becomeBackground(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}
*/
import "C"

func becomeForeground() { C.becomeForeground() }
func becomeBackground() { C.becomeBackground() }
