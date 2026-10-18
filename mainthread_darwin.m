#include <dispatch/dispatch.h>
#import <Foundation/Foundation.h>

// badproxyMainTask is the Go function exported by mainthread_darwin.go.
extern void badproxyMainTask(void);

int badproxyIsMainThread(void) {
	return [NSThread isMainThread] ? 1 : 0;
}

void badproxyRunOnMain(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		badproxyMainTask();
	});
}
