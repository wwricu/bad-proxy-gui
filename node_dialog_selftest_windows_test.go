//go:build windows

package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Drives the native node editor with Win32 messages instead of synthesized
// keyboard/mouse input, so it runs unattended and cannot affect anything
// outside its own window. Enable explicitly:
//
//	BADPROXY_GUI_SELFTEST=1 go test -run TestNodeDialogSelfTest -count=1 -v
type stChild struct {
	hwnd       uintptr
	class      string
	id         int
	l, t, r, b int32
	visible    bool
	enabled    bool
	style      uintptr
}

type stGuiInfo struct {
	size        uint32
	hwndActive  uintptr
	hwndFocus   uintptr
	hwndCapture uintptr
	hwndMenuOwn uintptr
	hwndCaret   uintptr
	rcCaret     [4]uint32
}

var (
	stUser32           = syscall.NewLazyDLL("user32.dll")
	stEnumWindows      = stUser32.NewProc("EnumWindows")
	stEnumChildWindows = stUser32.NewProc("EnumChildWindows")
	stGetClassName     = stUser32.NewProc("GetClassNameW")
	stGetCtrlID        = stUser32.NewProc("GetDlgCtrlID")
	stIsWinVisible     = stUser32.NewProc("IsWindowVisible")
	stIsWinEnabled     = stUser32.NewProc("IsWindowEnabled")
	stGetWinThread     = stUser32.NewProc("GetWindowThreadProcessId")
	stPostMsg          = stUser32.NewProc("PostMessageW")
	stGetGuiThreadInfo = stUser32.NewProc("GetGUIThreadInfo")
	stGetWindowLongW   = stUser32.NewProc("GetWindowLongW")
	stGetForeground    = stUser32.NewProc("GetForegroundWindow")
	stGetClientRect    = stUser32.NewProc("GetClientRect")
	stGetWindowText    = stUser32.NewProc("GetWindowTextW")
	stGetWindowDC      = stUser32.NewProc("GetWindowDC")
	stReleaseDC        = stUser32.NewProc("ReleaseDC")
	stSetForeground    = stUser32.NewProc("SetForegroundWindow")
	stSetWindowPos     = stUser32.NewProc("SetWindowPos")
	stWindowFromPoint  = stUser32.NewProc("WindowFromPoint")
	stSetProcDpiCtx    = stUser32.NewProc("SetProcessDpiAwarenessContext")
	stSetProcDpi       = stUser32.NewProc("SetProcessDPIAware")

	stGdi32          = syscall.NewLazyDLL("gdi32.dll")
	stCreateCompDC   = stGdi32.NewProc("CreateCompatibleDC")
	stCreateCompBmp  = stGdi32.NewProc("CreateCompatibleBitmap")
	stSelect         = stGdi32.NewProc("SelectObject")
	stGetDIBits      = stGdi32.NewProc("GetDIBits")
	stDeleteDC       = stGdi32.NewProc("DeleteDC")
	stDeleteObj      = stGdi32.NewProc("DeleteObject")
	stBitBlt         = stGdi32.NewProc("BitBlt")
	stClientToScreen = stUser32.NewProc("ClientToScreen")

	stGWLStyle   = -16
	stGWLExStyle = -20
)

// stDPIPerMonitorV2 is (DPI_AWARENESS_CONTEXT)-4. The packaged app declares this
// in build/windows/wails.exe.manifest, so the test process has to ask for it at
// runtime - a test binary has no embedded manifest.
const stDPIPerMonitorV2 = ^uintptr(3)

// The checks below measure rects and pixels of a real window. A DPI-unaware
// process on a scaled display gets every rect virtualized back to 96 DPI while
// PrintWindow fills physical pixels, so the geometry assertions compare numbers
// the user never sees and the colour sampler reads the wrong pixels entirely.
// Being aware the way the app is makes both agree.
func TestMain(m *testing.M) {
	if r, _, _ := stSetProcDpiCtx.Call(stDPIPerMonitorV2); r == 0 {
		stSetProcDpi.Call()
	}
	os.Exit(m.Run())
}

// Geometry the HTML modal uses, so the native editor is checked against the
// same numbers instead of against itself.
const (
	wantClientW = 380
	wantLabelW  = 110 // .form-grid grid-template-columns
	wantFieldX  = 136 // 16 pad + 110 label + 10 column gap
	wantFieldW  = 228 // 380 - 136 - 16 pad
	wantFieldH  = 26
	wantCardPad = 16
	wantBtnH    = 28
	wantBtnGap  = 8 // .modal-actions gap
	wantRowGap  = 8
	// The parent draws each EDIT's border one inset outside the control, so the
	// grid's box model is the frame, not the window rect.
	wantFrameInset = 2
	wantCornerPad  = 8           // stay clear of the 4px rounded corners when sampling a line
	wantTitle      = "Edit Node" // the native caption's action word for an existing node
	wantSaveText   = "Save"
	wantCancel     = "Cancel"
)

const (
	stCBGetCount = 0x0146
	stCBSetCur   = 0x014E // CB_SETCURSEL
	stCBGetCur   = 0x0147 // CB_GETCURSEL
	// CB_GETDROPPEDCONTROLRECT: where the list will actually paint when the user
	// clicks the arrow. A combo whose window is only as tall as the closed
	// control drops a list with no rows in it, which looks exactly like a
	// dropdown that does nothing.
	stCBGetDropRc = 0x0152
)

// stDropRect reports the height, in pixels, of the list a combobox paints when
// the user clicks its arrow.
func stDropRect(h uintptr) int {
	var r ndRect
	stSend(h, stCBGetDropRc, 0, uintptr(unsafe.Pointer(&r)))
	return int(r.bottom - r.top)
}

func stSend(h, msg, w, l uintptr) uintptr {
	r, _, _ := ndSendMsg.Call(h, msg, w, l)
	return r
}

func stClassName(h uintptr) string {
	buf := make([]uint16, 256)
	stGetClassName.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf)
}

func stRect(h uintptr) (l, t, r, b int32) {
	var rc ndRect
	ndGetWndRect.Call(h, uintptr(unsafe.Pointer(&rc)))
	return rc.l, rc.top, rc.right, rc.bottom
}

// stChildren lists every child control of hwnd in creation order.
func stChildren(hwnd uintptr) []stChild {
	var out []stChild
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		l, t, r, b := stRect(h)
		v, _, _ := stIsWinVisible.Call(h)
		e, _, _ := stIsWinEnabled.Call(h)
		id, _, _ := stGetCtrlID.Call(h)
		st, _, _ := stGetWindowLongW.Call(h, ndU(stGWLStyle))
		out = append(out, stChild{hwnd: h, class: stClassName(h), id: int(id),
			l: l, t: t, r: r, b: b, visible: v != 0, enabled: e != 0, style: st})
		return 1
	})
	stEnumChildWindows.Call(hwnd, cb, 0)
	return out
}

// stTopWindow waits for the editor window to appear in this process.
func stTopWindow(class string, d time.Duration) uintptr {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		var found uintptr
		cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
			if stClassName(h) == class {
				found = h
				return 0
			}
			return 1
		})
		stEnumWindows.Call(cb, 0)
		if found != 0 {
			return found
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0
}

func stSet(h uintptr, s string) {
	p, _ := syscall.UTF16PtrFromString(s)
	stSend(h, ndWMSetTxt, 0, uintptr(unsafe.Pointer(p)))
}

func stGet(h uintptr) string {
	n := stSend(h, ndWMGetLen, 0, 0)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	stSend(h, ndWMGetTxt, n+1, uintptr(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf)
}

func stCombo(h uintptr, msg, w uintptr) int {
	return int(stSend(h, msg, w, 0))
}

// stCmd delivers a WM_COMMAND exactly the way Windows does: low word of
// wParam is the control id, high word the notification code.
func stCmd(parent uintptr, id, notify int, child uintptr) {
	stPostMsg.Call(parent, ndWMCommand, uintptr(id)|uintptr(notify)<<16, child)
	time.Sleep(150 * time.Millisecond)
}

func stFind(kids []stChild, id int) (stChild, bool) {
	for _, c := range kids {
		if c.id == id {
			return c, true
		}
	}
	return stChild{}, false
}

// stClientOrigin maps the window's client (0,0) to screen coordinates, so a
// child rect can be compared with the CSS box model the modal uses.
func stClientOrigin(hwnd uintptr) (int, int) {
	pt := struct{ x, y int32 }{}
	stClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	return int(pt.x), int(pt.y)
}

func stText(hwnd uintptr) string {
	buf := make([]uint16, 128)
	stGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

// stShot grabs hwnd's rectangle off the desktop and hands back the pixels.
// Everything else about this dialog is asserted numerically; the picture is the
// only way to check what the user will actually see - colours, fonts, and
// whether the web-style buttons really are the web-style buttons.
//
// PrintWindow cannot be used for this. The field frames are drawn by the form's
// own WM_PAINT and PrintWindow asks a window for WM_PRINTCLIENT instead, so it
// returns a bitmap with an empty client area: every border reads as #000000 and
// the colour checks fail on a form that looks exactly right on screen.
func stShot(t *testing.T, hwnd uintptr, name string) *image.RGBA {
	// A desktop grab is only honest while nothing is painted over the window, and
	// a test process is not allowed to take the foreground, so raise it the way a
	// dialog is raised and put it back afterwards.
	const (
		stHwndTopmost = ^uintptr(0) // HWND_TOPMOST
		stHwndNoTop   = ^uintptr(1) // HWND_NOTOPMOST
		stSwpNoMove   = 0x0002
		stSwpNoSize   = 0x0001
		stSwpShowWin  = 0x0040
	)
	stSetWindowPos.Call(hwnd, stHwndTopmost, 0, 0, 0, 0,
		stSwpNoMove|stSwpNoSize|stSwpShowWin)
	stSetForeground.Call(hwnd)
	time.Sleep(300 * time.Millisecond)
	defer stSetWindowPos.Call(hwnd, stHwndNoTop, 0, 0, 0, 0, stSwpNoMove|stSwpNoSize)
	if !stOnTop(t, hwnd) {
		return nil
	}
	return stGrab(t, hwnd, name)
}

// stOnTop fails loudly rather than measuring somebody else's window: a grab of
// an occluded rectangle looks like a pass while proving nothing.
func stOnTop(t *testing.T, hwnd uintptr) bool {
	l, tp, r, b := stRect(hwnd)
	pt := uintptr(uint32((l+r)/2)) | uintptr(uint32((tp+b)/2))<<32
	hit, _, _ := stWindowFromPoint.Call(pt)
	if hit == 0 {
		t.Errorf("BUG: nothing is under the dialog's centre - it is not on screen")
		return false
	}
	if hit == hwnd {
		return true
	}
	for _, c := range stChildren(hwnd) {
		if c.hwnd == hit {
			return true
		}
	}
	t.Errorf("BUG: %s is on top of the dialog at its centre, so a desktop grab measures the wrong window", stClassName(hit))
	return false
}

// stShotScreen grabs without touching the Z order, for a composite shot where
// raising one window would leave the other where it was.
func stShotScreen(t *testing.T, hwnd uintptr, name string) *image.RGBA {
	return stGrab(t, hwnd, name)
}

// stGrab copies the desktop pixels under hwnd's window rect into an RGBA bitmap,
// and writes a PNG when BADPROXY_GUI_SHOT=1 so a human can look at it too.
func stGrab(t *testing.T, hwnd uintptr, name string) *image.RGBA {
	l, tp, r, b := stRect(hwnd)
	w, h := int(r-l), int(b-tp)
	if w <= 0 || h <= 0 {
		t.Errorf("BUG: cannot capture a %dx%d window", w, h)
		return nil
	}
	srcDC, _, _ := stGetWindowDC.Call(0) // GetDC(NULL) == the whole screen
	if srcDC == 0 {
		t.Errorf("BUG: no screen device context")
		return nil
	}
	defer stReleaseDC.Call(0, srcDC)
	memDC, _, _ := stCreateCompDC.Call(srcDC)
	defer stDeleteDC.Call(memDC)
	bmp, _, _ := stCreateCompBmp.Call(srcDC, uintptr(w), uintptr(h))
	if memDC == 0 || bmp == 0 {
		t.Errorf("BUG: capture device failed")
		return nil
	}
	defer stDeleteObj.Call(bmp)
	stSelect.Call(memDC, bmp)
	// SRCCOPY.
	stBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), srcDC, uintptr(l), uintptr(tp), 0x00CC0020)

	bi := stBitmapInfoHeader{}
	bi.size = uint32(unsafe.Sizeof(bi))
	bi.width, bi.height = int32(w), int32(-h) // negative = top-down rows
	bi.planes, bi.bitCount = 1, 32
	pixels := make([]byte, w*h*4)
	if n, _, _ := stGetDIBits.Call(memDC, bmp, 0, uintptr(h),
		uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&bi)), 0); n == 0 {
		t.Error("BUG: GetDIBits returned no scanlines")
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4+0] = pixels[i*4+2] // BGRA -> RGBA
		img.Pix[i*4+1] = pixels[i*4+1]
		img.Pix[i*4+2] = pixels[i*4+0]
		img.Pix[i*4+3] = 0xFF
	}
	if os.Getenv("BADPROXY_GUI_SHOT") != "1" {
		return img
	}
	path := filepath.Join(os.TempDir(), name+".png")
	f, err := os.Create(path)
	if err != nil {
		t.Errorf("BUG: cannot write the capture: %v", err)
		return img
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Errorf("BUG: png encode failed: %v", err)
	}
	bb := img.Bounds()
	t.Logf("captured %dx%d -> %s", bb.Dx(), bb.Dy(), path)
	return img
}

type stBitmapInfoHeader struct {
	size             uint32
	width, height    int32
	planes, bitCount uint16
	compression      uint32
	sizeImage        uint32
	xPPM, yPPM       int32
	colorUsed        uint32
	colorImportant   uint32
}

// stInkFamily names the colour family of one captured border pixel. style.css
// allows exactly two: #2f6f9f (.form-grid input:focus) and #d2d7de (resting).
// Classifying by hue instead of exact RGB is deliberate: a capture of a
// DPI-unaware process resamples the 1px line toward white, which keeps blue
// above red but destroys an equality test, while a dark neutral - the 64/64/64
// a NULL_PEN mix-up paints - must not pass as the light grey border.
func stInkFamily(r, g, b uint32) string {
	hi, lo := r, r
	for _, v := range []uint32{g, b} {
		if v > hi {
			hi = v
		}
		if v < lo {
			lo = v
		}
	}
	switch {
	case b >= r+16 && b > g+8:
		return "accent"
	case hi-lo <= 24 && lo >= 150:
		return "border"
	}
	return "other"
}

// stCheckInk proves a field's drawn border is on screen and names its colour.
// The parent paints a rounded 1px line one frame-inset outside the EDIT, so the
// top scanline of that frame carries the straight run between the two corner
// arcs. It returns "accent" or "border" so the caller can check the :focus ring.
func stCheckInk(t *testing.T, img *image.RGBA, hwnd uintptr, c stChild, inset, cornerPad int) string {
	if img == nil {
		t.Logf("no capture available, cannot check field id=%d border pixels", c.id)
		return "unknown"
	}
	wl, wt, _, _ := stRect(hwnd)
	y := int(c.t) - int(wt) - inset
	x0 := int(c.l) - int(wl) - inset + cornerPad
	x1 := int(c.r) - int(wl) + inset - cornerPad
	span := x1 - x0 + 1
	accent, border, other := 0, 0, 0
	// The darkest inked pixel, so a failing check says which colour was drawn
	// rather than just that it was the wrong family.
	sample, sampleLum := "#ffffff", uint32(3*255)
	bounds := img.Bounds()
	for x := x0; x <= x1; x++ {
		if x < 0 || y < 0 || x >= bounds.Dx() || y >= bounds.Dy() {
			continue
		}
		r, g, b, _ := img.At(x, y).RGBA()
		r, g, b = r>>8, g>>8, b>>8
		if r >= 250 && g >= 250 && b >= 250 {
			continue // untouched white background: the frame is missing here
		}
		if lum := r + g + b; lum < sampleLum {
			sampleLum = lum
			sample = fmt.Sprintf("#%02x%02x%02x", r, g, b)
		}
		switch stInkFamily(r, g, b) {
		case "accent":
			accent++
		case "border":
			border++
		default:
			other++
		}
	}
	inked := accent + border + other
	if span <= 0 || inked*10 < span*8 {
		t.Errorf("UI BUG: field id=%d paints no border - only %d of %d pixels on its top edge are inked", c.id, inked, span)
		return "none"
	}
	if other*3 > inked*2 {
		t.Errorf("UI BUG: field id=%d border is painted %d/%d pixels in a colour style.css never uses, darkest is %v (focus %d, resting %d)",
			c.id, other, inked, sample, accent, border)
	}
	if border >= accent && border >= other {
		return "border"
	}
	if accent >= other {
		return "accent"
	}
	return "other"
}

// stCheckGrid proves a native dialog uses the same box model as
// frontend/src/style.css (.modal padding 16, .form-grid 110px label column +
// 10px gap, 26px fields, 8px row gap) scaled to this window's DPI, and returns
// the check boxes it found so a form that has them can measure those too.
// wantPrimary names that form's own action caption ("Save", "OK"). Without
// this the dialogs drift back to generic Win32 spacing whenever someone edits
// layout.
func stCheckGrid(t *testing.T, hwnd uintptr, kids []stChild, img *image.RGBA, wantPrimary string) []stChild {
	const tol = 2 // rounding only; a real mismatch is always several px
	dpi := int(ndDpi(hwnd))
	px := func(v int) int { return v * dpi / ndDefaultDpi }
	ox, oy := stClientOrigin(hwnd)
	var rc ndRect
	stGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))

	check := func(what string, got, want int) bool {
		if got < want-tol || got > want+tol {
			t.Errorf("UI BUG: %s measures %dpx, the HTML modal uses %dpx (dpi %d)", what, got, want, dpi)
			return false
		}
		return true
	}

	const (
		stSSCenterImg = 0x0200 // SS_CENTERIMAGE, which every .form-grid label carries
		stSSRight     = 0x0002 // SS_RIGHT, the alignment .form-grid no longer asks for
		stTypeMask    = 0x000F // BS_TYPEMASK: the button flavour lives in the low nibble
		stBSAutoCheck = 0x0003 // BS_AUTOCHECKBOX, the flavour of ndBSCheck
		stTabstop     = 0x00010000
	)

	var fields, labels, btns, checks []stChild
	for _, c := range kids {
		if !c.visible {
			continue
		}
		switch c.class {
		case "Edit", "ComboBox":
			fields = append(fields, c)
			if c.style&stTabstop == 0 {
				t.Errorf("BUG: field id=%d has no WS_TABSTOP, so Tab skips it", c.id)
			}
		case "Static":
			if c.style&stTabstop != 0 {
				t.Errorf("BUG: label %q is a tab stop: Tab lands on text", stText(c.hwnd))
			}
			// .form-grid labels are the SS_CENTERIMAGE Statics in the 110px
			// column, and they are left-aligned: the ragged right edge the old
			// SS_RIGHT version gave is what the CSS change got rid of.
			if c.style&stSSCenterImg != 0 {
				if c.style&stSSRight != 0 {
					t.Errorf("UI BUG: label %q is SS_RIGHT but .form-grid sets text-align: left", stText(c.hwnd))
				}
				labels = append(labels, c)
			}
		case "Button":
			switch c.style & stTypeMask {
			case ndBSOwner:
				btns = append(btns, c)
			case stBSAutoCheck:
				checks = append(checks, c)
			default:
				t.Errorf("BUG: button %q is neither BS_OWNERDRAW nor BS_AUTOCHECKBOX (style 0x%x)", stText(c.hwnd), c.style)
			}
		}
	}
	if len(fields) == 0 {
		t.Fatal("UI BUG: no visible fields to measure")
	}

	var accentFrames, restingFrames, checkedFrames int
	for _, c := range fields {
		// Every field is measured as the frame the user sees, so an EDIT is
		// inflated by the inset that keeps its border clear of the control.
		inset := 0
		if c.class == "Edit" {
			inset = px(wantFrameInset)
		}
		x := int(c.l) - ox - inset
		check(c.class+" left edge", x, px(wantFieldX))
		check(c.class+" width", int(c.r-c.l)+2*inset, px(wantFieldW))
		if c.class == "Edit" {
			check("field height", int(c.b-c.t)+2*inset, px(wantFieldH))
			// No edge style gives an EDIT the .form-grid look, so the parent draws
			// the border itself. Prove it lands on pixels: a style bit can be set
			// while the frame stays invisible, which is exactly how the fields
			// shipped frameless for a whole round (0x1000 is WS_EX_RIGHT, not
			// WS_EX_CLIENTEDGE).
			checkedFrames++
			switch stCheckInk(t, img, hwnd, c, inset, px(wantCornerPad)) {
			case "accent":
				accentFrames++
			case "border":
				restingFrames++
			}
			continue
		}
		// A dropdown whose window is only the closed control's height paints an
		// empty list: the arrow appears to do nothing.
		dh := stDropRect(c.hwnd)
		if dh < 3*px(wantFieldH) {
			t.Errorf("BUG: combo id=%d drops a %dpx list, too short to pick from (rows are %dpx)",
				c.id, dh, px(wantFieldH))
		}
		t.Logf("combo id=%-4d window=%dpx list=%dpx", c.id, c.b-c.t, dh)
	}
	// build() puts the caret in the Name field, so exactly one frame should carry
	// the :focus colour and the rest the resting one. EN_SETFOCUS/EN_KILLFOCUS live
	// in the high byte (0x0100/0x0200, not 1/2); read them as 1/2 and every frame
	// stays grey with nothing in the log to show for it.
	t.Logf("field frames: %d resting #d2d7de, %d focused #2f6f9f, %d checked",
		restingFrames, accentFrames, checkedFrames)
	if img != nil && checkedFrames >= 2 {
		if accentFrames == 0 {
			t.Errorf("UI BUG: no field paints the .form-grid input:focus border - the caret is in a field but every frame is grey")
		}
		if restingFrames == 0 {
			t.Errorf("UI BUG: every field paints the focus border - the resting #d2d7de frame is gone")
		}
	}
	for _, c := range labels {
		check("label left edge", int(c.l)-ox, px(wantCardPad))
		check("label width", int(c.r-c.l), px(wantLabelW))
	}
	t.Logf("styles ok: %d labels, %d fields, %d push buttons",
		len(labels), len(fields), len(btns))
	// Row pitch: consecutive fields sit px(wantFieldH)+px(wantRowGap) apart,
	// measured on the frames. EnumChildWindows walks the Z order, not the layout
	// order.
	frameTop := func(c stChild) int {
		if c.class == "Edit" {
			return int(c.t) - px(wantFrameInset)
		}
		return int(c.t)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].t < fields[j].t })
	for i := 1; i < len(fields); i++ {
		pitch := frameTop(fields[i]) - frameTop(fields[i-1])
		if !check("row pitch", pitch, px(wantFieldH)+px(wantRowGap)) {
			t.Logf("  rows: %s at y=%d, %s at y=%d", fields[i-1].class, frameTop(fields[i-1])-oy,
				fields[i].class, frameTop(fields[i])-oy)
		}
	}

	// The card names itself in the window caption ("Bad Proxy · Edit Node"), which
	// is where a native window puts a title. Neither implementation repeats it
	// inside the card now, so the only Statics left are the grid labels.
	var statics []stChild
	for _, c := range kids {
		if c.class == "Static" && c.visible {
			statics = append(statics, c)
		}
	}
	if len(labels) == 0 {
		t.Fatal("UI BUG: no grid labels - .form-grid lost its label column")
	}
	if len(statics) != len(labels) {
		t.Errorf("UI BUG: %d Statics drawn but only %d grid labels - something else, such as an in-card heading, is on the form",
			len(statics), len(labels))
	}
	if len(labels) != len(fields) {
		t.Errorf("UI BUG: %d grid labels for %d fields; every field needs its label cell", len(labels), len(fields))
	}

	// The card itself: 380 CSS px wide, 16px padding on the right edge, and the
	// primary action button flush right with Cancel to its left.
	check("client width", int(rc.right), px(wantClientW))
	if len(btns) != 2 {
		t.Fatalf("UI BUG: expected 2 push buttons, found %d", len(btns))
	}
	for _, b := range btns {
		check("button "+stText(b.hwnd)+" height", int(b.b-b.t), px(wantBtnH))
		if b.style&stTypeMask != ndBSOwner {
			t.Errorf("UI BUG: button %q is not BS_OWNERDRAW, so the OS theme paints it instead of the web styles", stText(b.hwnd))
		}
	}
	save, cancel := btns[0], btns[1]
	if stText(save.hwnd) != wantPrimary {
		save, cancel = btns[1], btns[0]
	}
	if stText(save.hwnd) != wantPrimary || stText(cancel.hwnd) != wantCancel {
		t.Errorf("UI BUG: buttons are %q and %q, want %q and %q",
			stText(btns[0].hwnd), stText(btns[1].hwnd), wantPrimary, wantCancel)
	}
	check("primary right edge", int(save.r)-ox, px(wantClientW)-px(wantCardPad))
	check("secondary right edge", int(cancel.r)-ox, int(save.l)-ox-px(wantBtnGap))
	if int(cancel.r)-ox >= int(save.l)-ox {
		t.Errorf("UI BUG: %q overlaps or sits right of %q; index.html orders them Cancel then the action", wantCancel, wantPrimary)
	}
	if w := int(save.r - save.l); w <= px(wantBtnH) {
		t.Errorf("UI BUG: %q is %dpx wide - an owner-drawn button sized from its caption must fit %q plus 16px padding", wantPrimary, w, wantPrimary)
	}
	t.Logf("grid ok: dpi=%d fields=%d labels=%d %s=%dx%d at x=%d..%d",
		dpi, len(fields), len(labels), wantPrimary, save.r-save.l, save.b-save.t, int(save.l)-ox, int(save.r)-ox)
	return checks
}

func TestNodeDialogSelfTest(t *testing.T) {
	if os.Getenv("BADPROXY_GUI_SELFTEST") != "1" {
		t.Skip("set BADPROXY_GUI_SELFTEST=1 to open the native editor")
	}

	in := Node{Name: "probe", Protocol: "vless", Server: "example.com", Port: "443",
		Secret: "uuid-here", Transport: "xhttp", Authority: "example.com", Path: "/bp"}

	done := make(chan struct{})
	var out Node
	var confirmed bool
	go func() {
		out, confirmed = runNodeDialog(in)
		close(done)
	}()

	hwnd := stTopWindow("BadProxyForm", 5*time.Second)
	if hwnd == 0 {
		t.Fatal("editor window never appeared")
	}
	l, tp, r, b := stRect(hwnd)
	t.Logf("window class=%s rect=(%d,%d)-(%d,%d) %dx%d", stClassName(hwnd), l, tp, r, b, r-l, b-tp)
	if l < -30000 || tp < -30000 {
		t.Errorf("BUG: window is off-screen at (%d,%d) - centring failed, user cannot use it", l, tp)
	}

	kids := stChildren(hwnd)
	t.Logf("child controls: %d", len(kids))
	for _, c := range kids {
		ex, _, _ := stGetWindowLongW.Call(c.hwnd, ndU(stGWLExStyle))
		var cr ndRect
		stGetClientRect.Call(c.hwnd, uintptr(unsafe.Pointer(&cr)))
		t.Logf("  %-9s id=%-4d style=0x%x ex=0x%x rect=(%d,%d,%d,%d) client=%dx%d vis=%v en=%v",
			c.class, c.id, c.style, ex, c.l, c.t, c.r, c.b, cr.right-cr.l, cr.bottom-cr.top, c.visible, c.enabled)
	}
	if len(kids) == 0 {
		t.Fatal("BUG: editor has no child controls at all")
	}
	// EM_POSFROMCHAR asks the control where character 0 sits. A field that
	// right-aligns its text is invisible to every geometry check - it happened
	// when 0x1000 (WS_EX_RIGHT) was mistaken for WS_EX_CLIENTEDGE.
	const stEmPosFromChar = 0x00D6
	for _, c := range kids {
		if c.class != "Edit" || stText(c.hwnd) == "" {
			continue
		}
		ret, _, _ := ndSendMsg.Call(c.hwnd, stEmPosFromChar, 0, 0)
		if x := int32(int16(uint16(ret & 0xFFFF))); x > 12 {
			t.Errorf("UI BUG: field id=%d starts its text at x=%d, it is not left-aligned", c.id, x)
		}
	}
	img := stShot(t, hwnd, "badproxy-node-dialog")
	if got := stText(hwnd); got != "Bad Proxy · "+wantTitle {
		t.Errorf("UI BUG: window caption is %q, want %q", got, "Bad Proxy · "+wantTitle)
	}
	stCheckGrid(t, hwnd, kids, img, wantSaveText)

	// Leave the window up so a person can look at it too: the automated checks
	// cover geometry and border colours, not whether the whole thing reads right.
	// Set BADPROXY_GUI_HOLD=20 and the dialog stays for 20s.
	if hold := os.Getenv("BADPROXY_GUI_HOLD"); hold != "" {
		d, err := time.ParseDuration(hold + "s")
		if err != nil || d <= 0 {
			d = 20 * time.Second
		}
		t.Logf("HOLD %v for an external screen grab of hwnd=%d", d, hwnd)
		stSetForeground.Call(hwnd)
		time.Sleep(d / 3)
		stShotScreen(t, hwnd, "badproxy-node-desktop")
		time.Sleep(2 * d / 3)
	}

	for _, c := range kids {
		if c.class == "Edit" || c.class == "ComboBox" {
			if c.r-c.l <= 0 || c.b-c.t <= 0 {
				t.Errorf("BUG: %s id=%d has zero size (%dx%d) - cannot be clicked or typed into",
					c.class, c.id, c.r-c.l, c.b-c.t)
			}
			if !c.enabled {
				t.Errorf("BUG: %s id=%d is disabled", c.class, c.id)
			}
			if c.class == "Edit" && c.style&0x400 != 0 {
				// 0x400 is ES_READONLY on EDIT, CBS_NOINTEGRALHEIGHT on COMBOBOX.
				t.Errorf("BUG: %s id=%d is ES_READONLY", c.class, c.id)
			}
		}
	}

	// 1. Text entry: the Edit control must hold what it is given.
	if c, ok := stFind(kids, ndIDName); !ok {
		t.Error("BUG: Name edit box missing")
	} else {
		stSet(c.hwnd, "renamed")
		if got := stGet(c.hwnd); got != "renamed" {
			t.Errorf("BUG: Name edit roundtrip gave %q", got)
		} else {
			t.Logf("EDIT write/read ok: %q", got)
		}
	}

	// 2. Dropdown: the combo must hold its items and track a selection.
	if c, ok := stFind(kids, ndIDProto); !ok {
		t.Error("BUG: Protocol combo missing")
	} else {
		n := stCombo(c.hwnd, stCBGetCount, 0)
		t.Logf("Protocol combo items=%d", n)
		if n != len(ndProtocols) {
			t.Errorf("BUG: Protocol combo holds %d items, want %d", n, len(ndProtocols))
		}
		stCombo(c.hwnd, stCBSetCur, 2)
		if sel := stCombo(c.hwnd, stCBGetCur, 0); sel != 2 {
			t.Errorf("BUG: CB_SETCURSEL(2) then CB_GETCURSEL gave %d", sel)
		}
	}

	// 2b. The transport list is the plaintext guard: it must offer only the
	// encrypted transports, so no user choice can dial a bare TCP hop.
	if c, ok := stFind(kids, ndIDTransport); !ok {
		t.Error("BUG: Transport combo missing")
	} else {
		n := stCombo(c.hwnd, stCBGetCount, 0)
		t.Logf("Transport combo items=%d (%v)", n, ndTransports)
		if n != len(ndTransports) {
			t.Errorf("BUG: Transport combo holds %d items, want %d", n, len(ndTransports))
		}
		for _, tr := range ndTransports {
			if encryptedTransport(tr) != tr {
				t.Errorf("BUG: ndTransports offers plaintext transport %q", tr)
			}
		}
	}

	// 3. Selection must drive the shared visibility rule like the HTML form does.
	if c, ok := stFind(kids, ndIDProto); ok {
		stCombo(c.hwnd, stCBSetCur, uintptr(indexOfStr(ndProtocols, "socks")))
		stCmd(hwnd, ndIDProto, ndCBNSel, c.hwnd)
		live := stChildren(hwnd)
		if s, ok := stFind(live, ndIDSecret); ok && s.visible {
			t.Error("BUG: switching to socks kept Secret visible - WM_COMMAND is not routed")
		} else {
			t.Log("WM_COMMAND routing ok: Secret hidden for socks")
		}
	}

	// 4. Activation/focus tells whether the user could actually type.
	var ti stGuiInfo
	ti.size = uint32(unsafe.Sizeof(ti))
	tid, _, _ := stGetWinThread.Call(hwnd, 0)
	if r, _, _ := stGetGuiThreadInfo.Call(tid, uintptr(unsafe.Pointer(&ti))); r != 0 {
		fg, _, _ := stGetForeground.Call()
		t.Logf("thread=%d active=0x%x focus=0x%x foreground=0x%x", tid, ti.hwndActive, ti.hwndFocus, fg)
		if ti.hwndFocus == 0 {
			t.Log("note: nothing focused in this thread (test binary is not the foreground app)")
		}
	}

	// 5. Save must close the dialog and hand back the edited node.
	for _, id := range []int{ndIDName, ndIDServer, ndIDPort} {
		if c, ok := stFind(stChildren(hwnd), id); ok {
			stSet(c.hwnd, map[int]string{ndIDName: "probe", ndIDServer: "example.com", ndIDPort: "443"}[id])
		}
	}
	stCmd(hwnd, ndIDOK, ndBNClick, 0)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("BUG: dialog never closed after BN_CLICKED on Save")
	}
	t.Logf("returned confirmed=%v node=%+v", confirmed, out)
	if !confirmed {
		t.Error("BUG: Save did not confirm the dialog")
		return
	}
	if out.Name != "probe" || out.Server != "example.com" || out.Port != "443" {
		t.Errorf("BUG: returned node lost required fields: %+v", out)
	}
	if out.Protocol != "socks" {
		t.Errorf("BUG: protocol change not read back: %q", out.Protocol)
	}
	if out.Secret != "" {
		t.Errorf("BUG: hidden Secret still written: %q", out.Secret)
	}
	// Everything the form showed must come back unchanged: a field that reads
	// empty on save is the "it did not save" the user sees.
	if out.Path != "/bp" || out.Authority != "example.com" {
		t.Errorf("BUG: visible optional fields lost on save: %+v", out)
	}

	// 6. Re-opening must work: the window class and the pump are reused.
	done2 := make(chan struct{})
	var ok2 bool
	go func() {
		_, ok2 = runNodeDialog(Node{Name: "second", Protocol: "trojan", Server: "a.b", Port: "1", Transport: "tls"})
		close(done2)
	}()
	h2 := stTopWindow("BadProxyForm", 5*time.Second)
	if h2 == 0 {
		t.Fatal("BUG: editor does not open a second time")
	}
	stPostMsg.Call(h2, ndWMClose, 0, 0)
	select {
	case <-done2:
	case <-time.After(5 * time.Second):
		t.Fatal("BUG: second editor instance did not close on WM_CLOSE")
	}
	if ok2 {
		t.Error("BUG: closing the window reported a confirmed edit")
	} else {
		t.Log("re-open + cancel ok")
	}
}

func indexOfStr(items []string, want string) int {
	for i, s := range items {
		if strings.EqualFold(s, want) {
			return i
		}
	}
	return 0
}

// stOwnerProc keeps the stand-in main window's callback alive: Windows stores
// the class's lpfnWndProc as a raw pointer and would call into freed memory
// once the Go value went away.
var stOwnerProc uintptr

// TestNodeDialogOwnerLeftAlign checks that the editor opens pinned to the
// owner's left edge - the same offset as the form grid's own padding - instead
// of floating in the centre, and stays vertically centred.
func TestNodeDialogOwnerLeftAlign(t *testing.T) {
	if os.Getenv("BADPROXY_GUI_SELFTEST") != "1" {
		t.Skip("set BADPROXY_GUI_SELFTEST=1 to open the native editor")
	}

	// A stand-in for the main window: same title, visible, at a known place.
	// It must live on a thread that pumps messages, because the dialog is
	// created owned by it - Windows sends the owner thread messages while
	// attaching and disabling it, and a thread that never pumps wedges
	// CreateWindowExW on the other side.
	type ownerWin struct {
		hwnd uintptr
		err  error
	}
	ownerReady := make(chan ownerWin, 1)
	ownerDone := make(chan struct{})
	const ownerX, ownerY, ownerW, ownerH = 300, 200, 900, 700
	stPostQuit := stUser32.NewProc("PostQuitMessage")
	go func() {
		defer close(ownerDone)
		runtime.LockOSThread()
		stOwnerProc = syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
			if msg == ndWMClose {
				stPostQuit.Call(0)
				return 0
			}
			r, _, _ := ndDefWndProc.Call(hwnd, msg, wParam, lParam)
			return r
		})
		hInst, _, _ := ndGetModHandle.Call(0)
		className, _ := syscall.UTF16PtrFromString("BadProxySelfTestMain")
		cls := wndClassEx{}
		cls.size = uint32(unsafe.Sizeof(cls))
		cls.lpfnWndProc = stOwnerProc
		cls.hInstance = hInst
		cls.hbrBackground, _, _ = ndCreateBrush.Call(ndBrushWhite)
		cls.lpszClassName = className
		if ok, _, err := ndRegisterClss.Call(uintptr(unsafe.Pointer(&cls))); ok == 0 {
			ownerReady <- ownerWin{err: err}
			return
		}
		title, _ := syscall.UTF16PtrFromString(windowTitle)
		hwnd, _, err := ndCreateWin.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
			0, ownerX, ownerY, ownerW, ownerH, 0, 0, hInst, 0)
		// ownerW/ownerH are the app's own window size, which is written in logical
		// pixels, but a DPI-aware process hands CreateWindowExW physical ones. Scale
		// the stand-in once its handle can be asked, or the placement checks below
		// compare a 700px window against a dialog laid out for 175%.
		if d := int(ndDpi(hwnd)); d != ndDefaultDpi {
			stSetWindowPos.Call(hwnd, 0, uintptr(ownerX), uintptr(ownerY),
				uintptr(ownerW*d/ndDefaultDpi), uintptr(ownerH*d/ndDefaultDpi), 0x0004)
		}
		ndShowWnd.Call(hwnd, ndSWShow)
		ownerReady <- ownerWin{hwnd: uintptr(hwnd), err: err}
		for {
			var m winMsg
			r, _, _ := ndGetMsg.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if r == 0 || int32(r) == -1 {
				return
			}
			ndTranslateMsg.Call(uintptr(unsafe.Pointer(&m)))
			ndDispatchMsg.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
	ow := <-ownerReady
	owner := ow.hwnd
	if owner == 0 {
		defer func() { stPostMsg.Call(owner, ndWMClose, 0, 0); <-ownerDone }()
		t.Fatalf("BUG: cannot create the stand-in main window: %v", ow.err)
	}
	defer func() {
		stPostMsg.Call(owner, ndWMClose, 0, 0)
		<-ownerDone
	}()
	if got := mainWindowHwnd(); got != owner {
		t.Fatalf("BUG: mainWindowHwnd() found 0x%x, want the stand-in 0x%x", got, owner)
	}

	done := make(chan struct{})
	go func() {
		runNodeDialog(Node{Name: "probe", Protocol: "vless", Server: "example.com", Port: "443",
			Secret: "u", Transport: "xhttp", Path: "/bp"})
		close(done)
	}()
	hwnd := stTopWindow("BadProxyForm", 5*time.Second)
	if hwnd == 0 {
		t.Fatal("editor window never appeared")
	}
	defer func() {
		stPostMsg.Call(hwnd, ndWMClose, 0, 0)
		<-done
	}()

	dpi := int(ndDpi(owner))
	ol, ot, oright, obottom := stRect(owner)
	dl, dt, dr, db := stRect(hwnd)
	wantL := int(ol) + ndPad*dpi/int(ndDefaultDpi)
	if absInt(int(dl)-wantL) > 2 {
		t.Errorf("UI BUG: dialog left=%d, want owner.left+pad=%d (dpi=%d) - the editor is not pinned to the left edge",
			dl, wantL, dpi)
	} else {
		t.Logf("left align ok: dialog.left=%d owner.left=%d pad*scale=%d", dl, ol, int(dl)-int(ol))
	}
	// Vertical: the form is created at an estimated height and re-centred when
	// layout() sizes it for real, so the finished dialog sits on the owner's
	// midpoint. 8px allows for the non-client rounding in outer().
	if dc, oc := (int(dt)+int(db))/2, (int(ot)+int(obottom))/2; absInt(dc-oc) > 8 {
		t.Errorf("UI BUG: dialog vertical centre=%d, owner centre=%d - the editor is [%d,%d] against owner [%d,%d]",
			dc, oc, dt, db, ot, obottom)
	} else {
		t.Logf("vertical ok: dialog [%d,%d] centre %d, owner centre %d", dt, db, dc, oc)
	}
	if int(dt) < int(ot) || int(db) > int(obottom) {
		t.Errorf("UI BUG: dialog vertical [%d,%d] falls outside the owner [%d,%d]", dt, db, ot, obottom)
	}
	if int(dr) > int(oright) {
		t.Errorf("UI BUG: dialog right=%d sticks out past the owner right=%d", dr, oright)
	}

	stShot(t, hwnd, "badproxy-node-leftalign")
	// The desktop grab is the only picture of what the user sees, and it is
	// worthless if the stand-in window is buried under another app.
	stSetForeground.Call(owner)
	time.Sleep(200 * time.Millisecond)
	stSetForeground.Call(hwnd)
	time.Sleep(200 * time.Millisecond)
	stShotScreen(t, owner, "badproxy-owner-plus-dialog")
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
