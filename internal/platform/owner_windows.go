//go:build windows

package platform

import (
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	user32Owner           = syscall.NewLazyDLL("user32.dll")
	procSetWindowLongPtrW = user32Owner.NewProc("SetWindowLongPtrW")
	procGetWindowOwner    = user32Owner.NewProc("GetWindow")
	procSetWindowPosOwner = user32Owner.NewProc("SetWindowPos")
)

const (
	// GWLP_HWNDPARENT is -8: for a top-level window it is the owner, the window
	// it must always sit above.
	gwlpHwndParent = ^uintptr(7)

	// GW_OWNER is how the owner is read back. GWLP_HWNDPARENT cannot be used
	// for that: on a top-level window it reports the parent, which a top-level
	// window does not have, so it answers 0 even while the owner is set.
	gwOwner = 4

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
)

// HwndOf is the window's handle, or 0 for a window that is already gone.
func HwndOf(win application.Window) uintptr {
	if win == nil {
		return 0
	}
	return uintptr(win.NativeWindow())
}

// OwnerOf returns the window that keeps w on top of it, or 0 when w answers to
// no window in particular.
func OwnerOf(win application.Window) uintptr {
	hwnd := HwndOf(win)
	if hwnd == 0 {
		return 0
	}
	var owner uintptr
	application.InvokeSync(func() {
		owner, _, _ = procGetWindowOwner.Call(hwnd, gwOwner)
	})
	return owner
}

// KeepAbove gives the form window the z-order half of a Win32 dialog: the
// window manager itself guarantees it stays above the main window for as long
// as it lives, so clicking the list can never bury the form that is waiting for
// an answer.
//
// Only the ownership half is taken. Wails' AttachModal also calls
// EnableWindow(main, false), and a disabled WebView2 host is fatal here - the
// next WM_ACTIVATE makes Wails' focus handler call Chromium.Focus(), which
// fails against a disabled host, and go-webview2 answers any focus error with
// os.Exit(1). So the form floats above the list, and the list stays clickable.
//
// Both calls run on the UI thread because that is the thread the window was
// created on; from another one SetWindowPos SendMessage's a thread that is not
// pumping and the app stops there.
func KeepAbove(win application.Window, owner application.Window) {
	hwnd, ownerHwnd := HwndOf(win), HwndOf(owner)
	if hwnd == 0 || ownerHwnd == 0 {
		return
	}
	application.InvokeSync(func() {
		procSetWindowLongPtrW.Call(hwnd, gwlpHwndParent, ownerHwnd)
		// A no-op move is what makes the window manager re-link the window into
		// the band directly above its owner.
		procSetWindowPosOwner.Call(hwnd, 0, 0, 0, 0, 0,
			uintptr(swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate))
	})
}

// RaiseToTop puts a window at the top of the z-order without asking for the
// foreground, which the window manager can refuse without saying so. The
// self-test drives it there rather than calling Focus: a silent veto would turn
// the assertion that follows into a test of nothing.
func RaiseToTop(win application.Window) {
	hwnd := HwndOf(win)
	if hwnd == 0 {
		return
	}
	application.InvokeSync(func() {
		procSetWindowPosOwner.Call(hwnd, 0 /* HWND_TOP */, 0, 0, 0, 0,
			uintptr(swpNoMove|swpNoSize|swpNoActivate))
	})
}
