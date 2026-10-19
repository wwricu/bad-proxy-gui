//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#import <Foundation/Foundation.h>

typedef void (*clickHandler)(void);

static NSStatusItem *statusItem = nil;
static clickHandler onClickHandler = nil;

// StatusItemTarget handles clicks from the status item button.
@interface StatusItemTarget : NSObject
@end

@implementation StatusItemTarget
+ (instancetype)sharedTarget {
	static StatusItemTarget *shared = nil;
	static dispatch_once_t onceToken;
	dispatch_once(&onceToken, ^{
		shared = [[StatusItemTarget alloc] init];
	});
	return shared;
}

- (void)handleClick:(id)sender {
	if (onClickHandler) {
		onClickHandler();
	}
}
@end

// createNativeStatusItem creates an NSStatusItem with a template image.
// The behavior allows System Settings to manage it.
void* createNativeStatusItem(unsigned char* imgData, int imgLen) {
	@autoreleasepool {
		NSData *data = [NSData dataWithBytes:imgData length:imgLen];
		NSImage *image = [[NSImage alloc] initWithData:data];
		if (!image) return nil;

		[image setTemplate:YES];
		[image setSize:NSMakeSize(18, 18)];

		statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
		if (!statusItem) return nil;

		[statusItem setImage:image];
		[statusItem setHighlightMode:YES];
		[statusItem setToolTip:@"Bad Proxy"];
		[statusItem setBehavior:NSStatusItemBehaviorRemovalAllowed];

		StatusItemTarget *target = [StatusItemTarget sharedTarget];
		statusItem.button.action = @selector(handleClick:);
		statusItem.button.target = target;

		return (__bridge void*)statusItem;
	}
}

// setStatusItemClickHandler sets the click handler for the status item.
void setStatusItemClickHandler(clickHandler handler) {
	onClickHandler = handler;
}

// removeStatusItem removes the status item from the menu bar.
void removeStatusItem() {
	if (statusItem) {
		[[NSStatusBar systemStatusBar] removeStatusItem:statusItem];
		statusItem = nil;
	}
}
*/
import "C"

import (
	"unsafe"
)

// nativeTray holds the raw pointer to the NSStatusItem.
type nativeTray struct {
	ptr unsafe.Pointer
}

// createNativeTray creates a native NSStatusItem that System Settings can manage.
func createNativeTray(imgData []byte) *nativeTray {
	if len(imgData) == 0 {
		return nil
	}
	ptr := C.createNativeStatusItem(
		(*C.uchar)(unsafe.Pointer(&imgData[0])),
		C.int(len(imgData)),
	)
	if ptr == nil {
		return nil
	}
	return &nativeTray{ptr: ptr}
}

// setClickHandler registers a callback for left-clicks on the status item.
func (t *nativeTray) setClickHandler(fn func()) {
	C.setStatusItemClickHandler(C.clickHandler(C.badproxyClickWrapper))
	registerClickHandler(fn)
}

// remove removes the status item from the menu bar.
func (t *nativeTray) remove() {
	C.removeStatusItem()
}

// clickHandlerRegistry stores the Go function to call on click.
var clickHandlerRegistry struct {
	fn func()
}

// registerClickHandler saves the handler for the C callback.
func registerClickHandler(fn func()) {
	clickHandlerRegistry.fn = fn
}

//export badproxyClickWrapper
func badproxyClickWrapper() {
	if clickHandlerRegistry.fn != nil {
		clickHandlerRegistry.fn()
	}
}

// setupTray overrides the generic version on macOS. It creates a native
// NSStatusItem so System Settings can manage the menu-bar icon. The second
// parameter is ignored — it exists only to match the non-darwin signature.
func setupTray(a *App, _ interface{}) {
	if !hasTray || (trayCanHide && a.st.HideTray) {
		return
	}
	a.trayOn = true

	tray := createNativeTray(trayTemplateBytes)
	if tray == nil {
		return
	}
	tray.setClickHandler(a.showWindow)
	a.nativeTray = tray
}
