//go:build darwin

// AppKit only lets the process main thread create a window. Two of our macOS
// entry points break that rule by accident: Wails occupies the main thread with
// [NSApp run], so anything we call from a goroutine lands on a worker thread -
// and both systray's nativeStart (the status item owns an NSStatusBarWindow)
// and the BecomeForeground/BecomeBackground calls reach into AppKit from there.
// The result is an NSInternalInconsistencyException, not a returned error, so
// the process dies before any Go-side handling can see it.
//
// The task therefore has to be handed back to the run loop Wails already owns.
// //export confines this file's preamble to declarations, which is why the
// dispatch block itself lives in mainthread_darwin.m.
package platform

/*
extern void badproxyMainTask(void);
void badproxyRunOnMain(void);
int badproxyIsMainThread(void);
*/
import "C"

// mainQueue holds the work waiting for the main thread. A channel is what keeps
// concurrent callers from overwriting each other's task: each dispatch_async
// pops exactly one entry, so pushes and pops balance and every caller runs its
// own closure.
var mainQueue = make(chan func(), 16)

//export badproxyMainTask
func badproxyMainTask() {
	select {
	case fn := <-mainQueue:
		fn()
	default:
	}
}

// runOnMainThread runs fn on the main thread and waits for it to finish.
// Already on that thread, it runs inline: queueing and then waiting on the very
// loop we are standing in would deadlock.
func runOnMainThread(fn func()) {
	if C.badproxyIsMainThread() == 1 {
		fn()
		return
	}
	done := make(chan struct{})
	mainQueue <- func() {
		fn()
		close(done)
	}
	C.badproxyRunOnMain()
	<-done
}

// PostToMainThread queues fn for the main run loop and returns without waiting.
//
// This is what an AppKit action handler needs: cgo parks the thread that called
// into Go until the exported function returns, and that thread is the main one,
// so a task queued from there and waited on there would be waiting behind the
// very call it is standing inside.
//
// A full queue is the same thread's fault - it is the run loop that empties it -
// so the task is dropped rather than waited on. Waiting would turn a click that
// lands on a wedged app into a second thread stuck behind it, which is a worse
// failure than the click going nowhere.
func PostToMainThread(fn func()) {
	select {
	case mainQueue <- fn:
		C.badproxyRunOnMain()
	default:
	}
}
