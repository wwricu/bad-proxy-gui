#import <Cocoa/Cocoa.h>

// badproxyTrayClicked is the Go function exported by tray_darwin.go. cgo makes
// it C-callable and generates this exact prototype into _cgo_export.h.
extern void badproxyTrayClicked(void);

static NSStatusItem *statusItem = nil;
static id clickTarget = nil;

// BadproxyClickTarget is the target of the status item button. AppKit sends
// the action on the main thread, which is why the Go side hands the work to
// the run loop instead of running it here.
@interface BadproxyClickTarget : NSObject
- (void)handleClick:(id)sender;
@end

@implementation BadproxyClickTarget
- (void)handleClick:(id)sender {
	(void)sender;
	badproxyTrayClicked();
}
@end

// badproxyCreateStatusItem puts a template glyph in the menu bar and returns
// the item, or NULL when there is none. Calling it twice returns the item
// already there rather than stacking a second one.
//
// The pool is drained before returning, so the two objects that have to outlive
// the call are the ones AppKit itself keeps: the status bar holds its item, and
// the item holds its image. The retain below is belt and braces for the item.
void *badproxyCreateStatusItem(const unsigned char *imgData, int imgLen) {
	@autoreleasepool {
		if (statusItem != nil) {
			return (void *)statusItem;
		}
		NSData *data = [NSData dataWithBytes:imgData length:imgLen];
		if (data == nil) {
			return NULL;
		}
		NSImage *image = [[NSImage alloc] initWithData:data];
		if (image == nil) {
			return NULL;
		}
		[image setTemplate:YES];
		[image setSize:NSMakeSize(18, 18)];
		statusItem = [[[NSStatusBar systemStatusBar]
			statusItemWithLength:NSSquareStatusItemLength] retain];
		if (statusItem == nil) {
			[image release];
			return NULL;
		}
		[statusItem setImage:image];
		[image release];
		[statusItem setHighlightMode:YES];
		[statusItem setToolTip:@"Bad Proxy"];
		clickTarget = [BadproxyClickTarget new];
		[statusItem.button setTarget:clickTarget];
		[statusItem.button setAction:@selector(handleClick:)];
		return (void *)statusItem;
	}
}

// badproxySetStatusItemVisible shows or hides the menu-bar item. The item is
// kept rather than released: this is the same object coming back, with its
// image, its tooltip and its click target still attached. With no item at all it
// says nothing, so a setting that was already off before the app ever made one
// is not a reason to make one.
void badproxySetStatusItemVisible(int visible) {
	if (statusItem != nil) {
		[statusItem setVisible:(visible ? YES : NO)];
	}
}

// badproxyStatusItemVisible is the item's own answer - 1 shown, 0 hidden, -1 no
// item - which is what the macOS self-test reads back after a settings draft.
int badproxyStatusItemVisible(void) {
	if (statusItem == nil) {
		return -1;
	}
	return [statusItem isVisible] ? 1 : 0;
}
