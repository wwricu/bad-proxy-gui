//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// nativeContextMenuSupported tells the frontend to use the real OS menu
// instead of drawing the DOM fallback.
const nativeContextMenuSupported = true

var (
	user32Ctx            = syscall.NewLazyDLL("user32.dll")
	kernel32Ctx          = syscall.NewLazyDLL("kernel32.dll")
	procCreatePopupMenu  = user32Ctx.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32Ctx.NewProc("AppendMenuW")
	procTrackPopupMenuEx = user32Ctx.NewProc("TrackPopupMenuEx")
	procDestroyMenu      = user32Ctx.NewProc("DestroyMenu")
	procGetCursorPos     = user32Ctx.NewProc("GetCursorPos")
	procGetLastError     = kernel32Ctx.NewProc("GetLastError")
	procPostMessage      = user32Ctx.NewProc("PostMessageW")
	procSetForeground    = user32Ctx.NewProc("SetForegroundWindow")
	procGetForeground    = user32Ctx.NewProc("GetForegroundWindow")
	procRegisterClassExW = user32Ctx.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32Ctx.NewProc("CreateWindowExW")
	procShowWindow       = user32Ctx.NewProc("ShowWindow")
	procDefWindowProcW   = user32Ctx.NewProc("DefWindowProcW")
	procPeekMessageW     = user32Ctx.NewProc("PeekMessageW")
	procTranslateMessage = user32Ctx.NewProc("TranslateMessage")
	procDispatchMessageW = user32Ctx.NewProc("DispatchMessageW")
	procMsgWait          = user32Ctx.NewProc("MsgWaitForMultipleObjects")
	procGetModuleHandleW = kernel32Ctx.NewProc("GetModuleHandleW")
)

const (
	mfString      = 0x0000
	tpmReturnCmd  = 0x0100
	ctxCmdBase    = 1000
	wsPopup       = 0x80000000
	wsVisible     = 0x10000000
	wsExToolWin   = 0x00000080
	swShow        = 5
	pmRemove      = 0x0001
	pmNoYield     = 0x0002
	qsAllInput    = 0x04FF
	wmNull        = 0x0000
	menuHostClass = "BadProxyMenuHost"
	menuPumpWait  = 50 // ms
)

// wndClassEx mirrors WNDCLASSEXW as laid out on 64-bit Windows.
type wndClassEx struct {
	size          uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	x       int32
	y       int32
}

type menuRequest struct {
	labels []string
	done   chan int
}

var (
	menuStart   sync.Once
	menuReq     = make(chan menuRequest, 1)
	menuReady   = make(chan struct{})
	menuHostErr error
	menuProcRef uintptr // keeps the window class callback alive
)

// u converts a signed coordinate to the uintptr a Win32 call expects.
func u(v int) uintptr { return uintptr(uint64(int64(v))) }

func menuHostProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// menuThread owns the host window and tracks the menu. Wails delivers bound
// calls on a worker goroutine whose thread pumps no messages, while the UI
// thread that owns the main window sits inside Chromium's message pump, where a
// Win32 menu modal loop is cancelled within milliseconds. A thread of our own
// with a window of our own is what TrackPopupMenuEx needs.
func menuThread() {
	runtime.LockOSThread()
	host, err := newMenuHost()
	menuHostErr = err
	close(menuReady)
	if err != nil {
		return
	}
	for {
		select {
		case req := <-menuReq:
			req.done <- trackMenu(host, req.labels)
		default:
		}
		// Keep pumping so Windows still considers the host window alive.
		procMsgWait.Call(0, 0, 1, menuPumpWait, qsAllInput)
		for {
			var m winMsg
			r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove|pmNoYield)
			if r == 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}
}

// newMenuHost registers the window class and creates the off-screen window that
// owns the shortcut menu. It must be called on the menu thread.
func newMenuHost() (uintptr, error) {
	className, err := syscall.UTF16PtrFromString(menuHostClass)
	if err != nil {
		return 0, err
	}
	cls := wndClassEx{}
	cls.size = uint32(unsafe.Sizeof(cls))
	cls.lpfnWndProc = syscall.NewCallback(menuHostProc)
	menuProcRef = cls.lpfnWndProc
	cls.hInstance, _, _ = procGetModuleHandleW.Call(0)
	cls.lpszClassName = className
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&cls)))

	title, _ := syscall.UTF16PtrFromString("Bad Proxy menu host")
	host, _, _ := procCreateWindowExW.Call(wsExToolWin,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup|wsVisible,
		u(-32000), u(-32000), 1, 1, 0, 0, cls.hInstance, 0)
	if host == 0 {
		le, _, _ := procGetLastError.Call()
		return 0, fmt.Errorf("menu host window could not be created (win32 error %d)", le)
	}
	procShowWindow.Call(host, swShow)
	return host, nil
}

// trackMenu shows the shortcut menu at the cursor and returns the selected
// index, or -1 when the user dismisses it. It must run on the thread that
// created host.
func trackMenu(host uintptr, labels []string) int {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return -1
	}
	defer procDestroyMenu.Call(menu)
	for i, label := range labels {
		p, err := syscall.UTF16PtrFromString(label)
		if err != nil {
			return -1
		}
		if r, _, _ := procAppendMenuW.Call(menu, mfString, uintptr(ctxCmdBase+i), uintptr(unsafe.Pointer(p))); r == 0 {
			return -1
		}
	}
	var pt struct{ x, y int32 }
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return -1
	}
	// A shortcut menu tracks only while its owner window is in the foreground;
	// the app window is, so this is a no-op in practice and only heals the odd
	// focus case. Restore the previous foreground window afterwards.
	fg, _, _ := procGetForeground.Call()
	procSetForeground.Call(host)
	cmd, _, _ := procTrackPopupMenuEx.Call(menu, tpmReturnCmd, uintptr(pt.x), uintptr(pt.y), host, 0)
	procPostMessage.Call(host, wmNull, 0, 0)
	if fg != 0 {
		procSetForeground.Call(fg)
	}
	if cmd == 0 {
		return -1
	}
	return int(cmd) - ctxCmdBase
}

// showNativeContextMenu pops a real Win32 shortcut menu at the cursor and
// waits until the user picks an item or dismisses it. Returns the selected
// index, or -1 when dismissed.
func showNativeContextMenu(labels []string) (int, error) {
	menuStart.Do(func() { go menuThread() })
	select {
	case <-menuReady:
	case <-time.After(3 * time.Second):
		return -1, fmt.Errorf("menu thread is not responding")
	}
	if menuHostErr != nil {
		return -1, menuHostErr
	}
	done := make(chan int, 1)
	select {
	case menuReq <- menuRequest{labels: labels, done: done}:
	case <-time.After(3 * time.Second):
		return -1, fmt.Errorf("menu thread is busy")
	}
	select {
	case sel := <-done:
		return sel, nil
	case <-time.After(5 * time.Minute):
		return -1, fmt.Errorf("context menu never closed")
	}
}
