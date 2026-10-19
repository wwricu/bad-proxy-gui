//go:build windows

package platform

import (
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	user32Attention   = syscall.NewLazyDLL("user32.dll")
	procFlashWindowEx = user32Attention.NewProc("FlashWindowEx")
)

// FLASHW_ALL blinks the caption and the taskbar button together;
// FLASHW_TIMERNOFG keeps them blinking until the window is in the foreground,
// so the effect ends the moment the user goes where it is pointing. A count of
// zero lets the system decide how many blinks that is, the same as the dialog
// manager's.
const (
	flashwAll       = 0x00000003
	flashwTimerNoFG = 0x00000004
)

type flashInfo struct {
	size    uint32
	hwnd    syscall.Handle
	flags   uint32
	count   uint32
	timeout uint32
}

// FlashForAttention asks the window manager itself for the "over here" blink.
// This is the effect a Win32 modal dialog gives when you click outside it: the
// dialog manager calls FlashWindowEx, it does not move the window, and neither
// do we.
func FlashForAttention(win application.Window) {
	hwnd := win.NativeWindow()
	if hwnd == nil {
		return
	}
	info := flashInfo{
		size:  uint32(unsafe.Sizeof(flashInfo{})),
		hwnd:  syscall.Handle(uintptr(hwnd)),
		flags: flashwAll | flashwTimerNoFG,
	}
	procFlashWindowEx.Call(uintptr(unsafe.Pointer(&info)))
}
