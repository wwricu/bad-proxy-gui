//go:build windows

package main

import (
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// nodeDialogSupported tells the frontend to open the real OS editor window
// instead of the in-page HTML modal.
const nodeDialogSupported = true

var (
	ndUser32   = syscall.NewLazyDLL("user32.dll")
	ndGdi32    = syscall.NewLazyDLL("gdi32.dll")
	ndKernel32 = syscall.NewLazyDLL("kernel32.dll")

	ndCreateWin     = ndUser32.NewProc("CreateWindowExW")
	ndRegisterClss  = ndUser32.NewProc("RegisterClassExW")
	ndDefWndProc    = ndUser32.NewProc("DefWindowProcW")
	ndShowWnd       = ndUser32.NewProc("ShowWindow")
	ndMoveWnd       = ndUser32.NewProc("MoveWindow")
	ndDestroyWnd    = ndUser32.NewProc("DestroyWindow")
	ndSendMsg       = ndUser32.NewProc("SendMessageW")
	ndEnableWnd     = ndUser32.NewProc("EnableWindow")
	ndIsDialogMsg   = ndUser32.NewProc("IsDialogMessageW")
	ndGetWndRect    = ndUser32.NewProc("GetWindowRect")
	ndGetClientRect = ndUser32.NewProc("GetClientRect")
	ndAdjWndRect    = ndUser32.NewProc("AdjustWindowRectEx")
	ndPostQuit      = ndUser32.NewProc("PostQuitMessage")
	ndGetMsg        = ndUser32.NewProc("GetMessageW")
	ndTranslateMsg  = ndUser32.NewProc("TranslateMessage")
	ndDispatchMsg   = ndUser32.NewProc("DispatchMessageW")
	ndGetModHandle  = ndKernel32.NewProc("GetModuleHandleW")
	ndMoveMem       = ndKernel32.NewProc("RtlMoveMemory")
	ndCreateBrush   = ndGdi32.NewProc("CreateSolidBrush")
	ndSetBkMode     = ndGdi32.NewProc("SetBkMode")
	ndCreateFont    = ndGdi32.NewProc("CreateFontW")
	ndGetDpi        = ndUser32.NewProc("GetDpiForWindow")
	ndLoadCursor    = ndUser32.NewProc("LoadCursorW")
	ndSetFocus      = ndUser32.NewProc("SetFocus")
	ndSysParams     = ndUser32.NewProc("SystemParametersInfoW")
	ndGetDC         = ndUser32.NewProc("GetDC")
	ndReleaseDC     = ndUser32.NewProc("ReleaseDC")
	ndSetTextColor  = ndGdi32.NewProc("SetTextColor")
	ndSelectObj     = ndGdi32.NewProc("SelectObject")
	ndDeleteObj     = ndGdi32.NewProc("DeleteObject")
	ndCreatePen     = ndGdi32.NewProc("CreatePen")
	ndStockObj      = ndGdi32.NewProc("GetStockObject")
	ndRoundRect     = ndGdi32.NewProc("RoundRect")
	ndDrawText      = ndUser32.NewProc("DrawTextW")
	ndBeginPaint    = ndUser32.NewProc("BeginPaint")
	ndEndPaint      = ndUser32.NewProc("EndPaint")
	ndInvalidate    = ndUser32.NewProc("InvalidateRect")
)

const (
	ndStyleDlg  = 0x00C00000 | 0x00080000 // WS_CAPTION | WS_SYSMENU
	ndWSChild   = 0x40000000
	ndWSVisible = 0x10000000
	ndWSTabstop = 0x00010000 // not 0x20000, which is WS_GROUP
	ndWSExDlg   = 0x00000001 // WS_EX_DLGMODALFRAME
	ndSWHide    = 0
	ndSWShow    = 5

	ndWMCreate  = 0x0001
	ndWMDestroy = 0x0002
	ndWMPaint   = 0x000F
	ndWMClose   = 0x0010
	ndWMCommand = 0x0111
	ndWMSetTxt  = 0x000C
	ndWMGetTxt  = 0x000D
	ndWMGetLen  = 0x000E
	ndWMSetFont = 0x0030
	ndWMCTL     = 0x0138 // WM_CTLCOLORSTATIC
	ndWMCTLEdit = 0x0135 // WM_CTLCOLOREDIT
	ndWMCtlBtn  = 0x013B // WM_CTLCOLORBTN
	ndWMDrawItm = 0x002B

	ndWMKeyDown = 0x0100
	ndVKReturn  = 0x0D
	ndVKEscape  = 0x1B

	ndESAuto    = 0x0080
	ndBSOwner   = 0x000B // BS_OWNERDRAW (a low-byte style, not a 0x30000 bit)
	ndSSRight   = 0x0202 // SS_RIGHT | SS_CENTERIMAGE
	ndSSLeft    = 0x0000
	ndCBSList   = 0x0003 | 0x0040 | 0x0200 | 0x0400 // CBS_DROPDOWNLIST|AUTOHSCROLL|HASSTRINGS|NOINTEGRALHEIGHT
	ndODSSelect = 0x0001                            // ODS_SELECTED

	ndCBNSel  = 1
	ndBNClick = 0
	// EN_* notification codes live in the high byte, unlike CBN_/BN_.
	ndENFocus = 0x0100 // EN_SETFOCUS
	ndENKill  = 0x0200 // EN_KILLFOCUS
	ndCBAdd   = 0x0143 // CB_ADDSTRING (0x0140 is CB_GETEDITSEL)
	ndCBSetS  = 0x014E // CB_SETCURSEL
	ndCBGetS  = 0x0147 // CB_GETCURSEL

	ndBkTrans = 1

	ndNullPen   = 8 // NULL_PEN; 3 is GRAY_BRUSH
	ndNullBrush = 5 // NULL_BRUSH
	ndPsSolid   = 0

	ndDtCenter     = 0x01
	ndDtVCenter    = 0x04
	ndDtSingleLine = 0x20
	ndDtCalcRect   = 0x400

	// SPI_GETWORKAREA returns the primary monitor minus the taskbar.
	spiGetWorkArea = 0x0030

	ndBrushWhite = 0xFFFFFF
	ndIDCArrow   = 32512
	ndDefaultDpi = 96
	ndNoLen      = ^uintptr(0) // the -1 length DrawText understands
)

// Design tokens mirrored from frontend/src/style.css so the native editor reads
// as the same component as the HTML modal. Values are CSS pixels; ndForm.px
// scales them to this window's DPI.
const (
	ndClrText   = 0x2B2622 // #22262b, kept as COLORREF 0x00BBGGRR
	ndClrLabel  = 0x797069 // #697079
	ndClrBorder = 0xDED7D2 // #d2d7de
	ndClrAccent = 0x9F6F2F // #2f6f9f, .btn-primary
	ndClrDeep   = 0x8A6028 // #28608a, .btn-primary:hover
	ndClrHover  = 0xF4F1EE // #eef1f4, .btn-secondary:hover

	ndPad         = 16 // .modal padding
	ndClientW     = 380
	ndLabelW      = 110 // .form-grid grid-template-columns
	ndColGap      = 10
	ndRowGap      = 8  // .form-grid row-gap
	ndInputH      = 26 // 13px text + 6px padding + 1px border
	ndTitleH      = 20
	ndTitleGap    = 12 // .modal-title margin-bottom
	ndActionsGap  = 16 // .modal-actions margin-top
	ndBtnGap      = 8
	ndBtnH        = 28 // 13px text + 6px padding + 2px border
	ndBtnPadX     = 16 // .btn-* padding: 6px 16px
	ndCornerDia   = 8  // RoundRect ellipse = twice the 4px CSS radius
	ndFrameInset  = 2  // gap between a drawn field frame and the EDIT inside it
	ndBodyFontPx  = 13 // .form-grid input, .btn-* font-size
	ndLabelFontPx = 12 // .form-grid label font-size
	ndTitleFontPx = 15 // .modal-title font-size

	ndLabelX = ndPad
	ndInputX = ndPad + ndLabelW + ndColGap
	ndInputW = ndClientW - ndInputX - ndPad
)

// Control IDs for the addressable widgets. Save and Cancel use the system
// IDOK/IDCANCEL values. That is not enough to make Enter work, because
// BS_OWNERDRAW takes the same style bits as BS_DEFPUSHBUTTON and so no control
// here is a default button: the modal loop maps Enter and Esc onto these two
// actions itself.
const (
	ndIDName = 101 + iota
	ndIDProto
	ndIDServer
	ndIDPort
	ndIDSecret
	ndIDTransport
	ndIDAuthority
	ndIDPath
	ndIDPadding
)

const (
	ndIDOK     = 1 // IDOK
	ndIDCancel = 2 // IDCANCEL
)

var (
	ndProtocols  = []string{"vless", "btp", "trojan", "socks"}
	ndTransports = []string{"tls", "xhttp"}
)

// ndRow is one label+input pair in vertical layout order.
type ndRow struct {
	label   uintptr // STATIC handle
	input   uintptr // editable widget handle
	visID   int     // the control id whose visibility this row toggles
	visible bool
	drop    int  // extra height a COMBOBOX needs for its list (0 for others)
	frame   bool // the parent draws a .form-grid border around this row
}

// ndFrame is one field border to paint in the parent's WM_PAINT, in client
// coordinates, plus the control id it belongs to (for the :focus colour).
type ndFrame struct {
	r  ndRect
	id int
}

// ndBtn is an owner-drawn push button plus the caption it must paint.
type ndBtn struct {
	hwnd    uintptr
	text    string
	primary bool
	w       int // content width plus padding, in device pixels
}

// ndForm owns one open editor window. The modal loop guarantees a single live
// instance, so a package pointer routes the window-proc callback.
type ndForm struct {
	hwnd    uintptr
	dpi     uint32
	font    uintptr // body text: fields, buttons
	fontLbl uintptr // grid labels
	brush   uintptr
	rows    []*ndRow
	frames  []ndFrame // field borders to paint, rebuilt by layout
	focus   int       // control id holding the caret, for the :focus frame
	btns    [2]*ndBtn
	labelID map[uintptr]int // STATIC handle -> the field id it labels
	inputs  map[int]uintptr
	vis     map[int]bool
	in      Node
	out     Node
	proved  bool
	closed  bool
}

var (
	ndCurrent *ndForm

	// ndMu serialises the editor. Bound calls arrive on independent goroutines,
	// so a second open while one is live would overwrite ndCurrent and route both
	// windows' messages into the wrong form.
	ndMu        sync.Mutex
	ndClassOnce sync.Once
	ndClassProc uintptr // keeps the window class callback alive
)

func ndU(v int) uintptr { return uintptr(int64(v)) }

// px converts one CSS-pixel layout metric to this window's physical pixels.
func (f *ndForm) px(v int) int { return v * int(f.dpi) / ndDefaultDpi }

// ndDpi asks Windows for the DPI a window is rendered at. Passing 0 returns
// the system DPI, which is the best guess for a window not created yet.
func ndDpi(hwnd uintptr) uint32 {
	d, _, _ := ndGetDpi.Call(hwnd, 0)
	if d == 0 {
		return ndDefaultDpi
	}
	return uint32(d)
}

// ndFont builds a Segoe UI face at a CSS pixel size and weight. The
// DEFAULT_GUI_FONT stock object is not scaled for a PerMonitorV2 process, so
// asking for it leaves the form with 96-DPI text on a high-density display.
func ndFont(pixelHeight, weight int) uintptr {
	name, _ := syscall.UTF16PtrFromString("Segoe UI")
	h, _, _ := ndCreateFont.Call(
		uintptr(-pixelHeight), 0, 0, 0, uintptr(weight), 0, 0, 0,
		uintptr(1 /*DEFAULT_CHARSET*/), 0, 0, 5 /*CLEARTYPE_QUALITY*/, 0x20,
		uintptr(unsafe.Pointer(name)))
	return h
}

func (f *ndForm) setTxt(id int, s string) {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return
	}
	ndSendMsg.Call(f.inputs[id], ndWMSetTxt, 0, uintptr(unsafe.Pointer(p)))
}

func (f *ndForm) getTrim(id int) string {
	h := f.inputs[id]
	n, _, _ := ndSendMsg.Call(h, ndWMGetLen, 0, 0)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	// WM_GETTEXT's count includes the terminating NUL, so passing n would drop
	// the last character of every field.
	ndSendMsg.Call(h, ndWMGetTxt, n+1, uintptr(unsafe.Pointer(&buf[0])))
	return strings.TrimSpace(syscall.UTF16ToString(buf))
}

func (f *ndForm) comboSel(id int, items []string) string {
	i, _, _ := ndSendMsg.Call(f.inputs[id], ndCBGetS, 0, 0)
	if int(i) < 0 || int(i) >= len(items) {
		return items[0]
	}
	return items[int(i)]
}

func comboIndexOf(items []string, want string) int {
	for i, it := range items {
		if it == want {
			return i
		}
	}
	return 0
}

// runNodeDialog opens the native editor and returns the edited node and
// whether the user confirmed. The modal loop runs on the locked OS thread so
// the window has its own pump (bound calls land on a goroutine whose thread
// pumps no messages).
func runNodeDialog(in Node) (Node, bool) {
	ndMu.Lock()
	defer ndMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	f := &ndForm{in: in, out: in, inputs: make(map[int]uintptr),
		vis: make(map[int]bool), labelID: make(map[uintptr]int)}
	f.markVisibility(in.Protocol, in.Transport)
	ndCurrent = f
	defer func() { ndCurrent = nil }()

	if !f.createWindow() {
		return in, false
	}
	if owner := mainWindowHwnd(); owner != 0 {
		ndEnableWnd.Call(owner, 0)
		defer ndEnableWnd.Call(owner, 1)
	}

	var msg winMsg
	for {
		r, _, _ := ndGetMsg.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if msg.hwnd == f.hwnd || f.isMine(msg.hwnd) {
			// BS_OWNERDRAW occupies the same style bits as BS_DEFPUSHBUTTON, so the
			// dialog manager has no default button to press and Enter would do
			// nothing. Wire the two accelerators the HTML modal has here.
			if msg.message == ndWMKeyDown {
				switch msg.wParam {
				case ndVKReturn:
					f.accept()
					continue
				case ndVKEscape:
					f.close()
					continue
				}
			}
			if ok, _, _ := ndIsDialogMsg.Call(f.hwnd, uintptr(unsafe.Pointer(&msg))); ok != 0 {
				continue
			}
		}
		ndTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
		ndDispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
	}
	return f.out, f.proved
}

func (f *ndForm) isMine(hwnd uintptr) bool {
	for _, h := range f.inputs {
		if h == hwnd {
			return true
		}
	}
	for _, b := range f.btns {
		if b != nil && b.hwnd == hwnd {
			return true
		}
	}
	return false
}

func (f *ndForm) createWindow() bool {
	className, _ := syscall.UTF16PtrFromString("BadProxyNodeForm")
	hInst, _, _ := ndGetModHandle.Call(0)
	// Register once: re-registering the same class name always fails after the
	// first call, so every later attempt leaked a fresh callback while the class
	// kept pointing at the original one.
	ndClassOnce.Do(func() {
		cls := wndClassEx{}
		cls.size = uint32(unsafe.Sizeof(cls))
		cls.lpfnWndProc = syscall.NewCallback(ndWndProc)
		ndClassProc = cls.lpfnWndProc
		cls.hInstance = hInst
		cls.hbrBackground, _, _ = ndCreateBrush.Call(ndBrushWhite)
		cls.hCursor, _, _ = ndLoadCursor.Call(0, uintptr(ndIDCArrow))
		cls.lpszClassName = className
		ndRegisterClss.Call(uintptr(unsafe.Pointer(&cls)))
	})

	// The window does not exist yet, so size it from the owner's DPI; WM_CREATE
	// repeats the question with the real handle.
	f.dpi = ndDpi(mainWindowHwnd())
	tp, _ := syscall.UTF16PtrFromString(nodeDialogTitle(f.in))
	outerW, outerH := f.outer(f.px(ndClientW), f.px(640))
	x, y := f.center(outerW, outerH)
	hwnd, _, _ := ndCreateWin.Call(
		uintptr(ndWSExDlg), uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(tp)),
		uintptr(ndStyleDlg), ndU(x), ndU(y), uintptr(outerW), uintptr(outerH),
		uintptr(mainWindowHwnd()), 0, hInst, 0)
	if hwnd == 0 {
		return false
	}
	f.hwnd = hwnd
	ndShowWnd.Call(hwnd, ndSWShow)
	// The dialog is shown from a worker thread that is not the foreground
	// app's thread, so it does not inherit activation. Without this the window
	// paints but never owns the caret and keystrokes stay with the webview.
	procBringToTop.Call(hwnd)
	procSetForeground.Call(hwnd)
	return true
}

// nodeDialogTitle is the native caption. The card title inside the window
// repeats the action alone, the way the HTML modal renders it.
func nodeDialogTitle(n Node) string {
	if nodeIsNew(n) {
		return "Bad Proxy · Add Node"
	}
	return "Bad Proxy · Edit Node"
}

func nodeCardTitle(n Node) string {
	if nodeIsNew(n) {
		return "Add Node"
	}
	return "Edit Node"
}

func nodeIsNew(n Node) bool {
	return n.Name == "" && n.Server == "" && n.Port == ""
}

func (f *ndForm) center(w, h int) (int, int) {
	if owner := mainWindowHwnd(); owner != 0 {
		var r ndRect
		ndGetWndRect.Call(owner, uintptr(unsafe.Pointer(&r)))
		return int(r.l) + f.px(ndPad), int(r.top) + (int(r.bottom)-int(r.top)-h)/2
	}
	// No owner to centre against: the main window is hidden in the tray, or this
	// runs outside the app. Centre on the desktop work area rather than at
	// -32000, which used to open the editor where nobody could reach it.
	var r ndRect
	if ok, _, _ := ndSysParams.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0); ok != 0 && r.right > r.l {
		x, y := int(r.l)+(int(r.right)-int(r.l)-w)/2, int(r.top)+(int(r.bottom)-int(r.top)-h)/2
		if x > int(r.l) && y > int(r.top) {
			return x, y
		}
	}
	return 64, 64
}

func (f *ndForm) outer(cw, ch int) (int, int) {
	r := ndRect{0, 0, int32(cw), int32(ch)}
	ndAdjWndRect.Call(uintptr(unsafe.Pointer(&r)), uintptr(ndStyleDlg), 0, uintptr(ndWSExDlg))
	return int(r.right - r.l), int(r.bottom - r.top)
}

type ndRect struct{ l, top, right, bottom int32 }

func ndWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	f := ndCurrent
	if f == nil {
		r, _, _ := ndDefWndProc.Call(hwnd, msg, wParam, lParam)
		return r
	}
	switch msg {
	case ndWMCreate:
		// WM_CREATE is delivered inside CreateWindowExW, before createWindow
		// gets a chance to store the handle. Every child is parented to
		// f.hwnd, so it has to be set here or the form comes up empty.
		f.hwnd = hwnd
		if d := ndDpi(hwnd); d != 0 {
			f.dpi = d
		}
		f.brush, _, _ = ndCreateBrush.Call(ndBrushWhite)
		f.font = ndFont(f.px(ndBodyFontPx), 400)
		f.fontLbl = ndFont(f.px(ndLabelFontPx), 400)
		f.build()
		return 0
	case ndWMCTL, ndWMCTLEdit, ndWMCtlBtn:
		// One colour scheme for every text the dialog draws: muted grid labels
		// and body-coloured fields on the card's white rather than the OS dialog
		// grey (which is what they use when visual styles are off). The
		// owner-drawn push buttons ignore this; drawButton fills them itself.
		ndSetBkMode.Call(wParam, ndBkTrans)
		ndSetTextColor.Call(wParam, f.colorFor(lParam))
		return f.brush
	case ndWMDrawItm:
		f.drawButton(lParam)
		return 1
	case ndWMPaint:
		f.paintFrames()
		return 0
	case ndWMCommand:
		f.onCommand(wParam)
		return 0
	case ndWMClose:
		f.close()
		return 0
	case ndWMDestroy:
		ndPostQuit.Call(0)
		return 0
	}
	r, _, _ := ndDefWndProc.Call(hwnd, msg, wParam, lParam)
	return r
}

// colorFor maps a STATIC to its CSS colour: grid labels are muted, the card
// title uses the body text colour.
func (f *ndForm) colorFor(ctrl uintptr) uintptr {
	if _, ok := f.labelID[ctrl]; ok {
		return ndClrLabel
	}
	return ndClrText
}

// create makes one child control, applies the font, and records its handle by
// id in the inputs map. WS_TABSTOP matters here: without it IsDialogMessage
// leaves the control out of the tab order and the dialog opens with nothing
// focused, so typing goes nowhere until the mouse clicks a field.
func (f *ndForm) create(class, text string, id int, style uintptr, font uintptr) uintptr {
	return f.createEx(class, text, id, style, 0, font, true)
}

// createStatic makes a non-interactive label. Statics deliberately get no
// WS_TABSTOP: the dialog manager walks every tab stop it can find, so a label
// carrying the bit would take focus away from the fields.
func (f *ndForm) createStatic(text string, style, font uintptr) uintptr {
	return f.createEx("STATIC", text, 0, style, 0, font, false)
}

func (f *ndForm) createEx(class, text string, id int, style, exStyle uintptr, font uintptr, tab bool) uintptr {
	cls, _ := syscall.UTF16PtrFromString(class)
	tp, _ := syscall.UTF16PtrFromString(text)
	s := uintptr(ndWSChild | ndWSVisible)
	// Statics are painted text, not controls: giving one WS_TABSTOP lets the
	// dialog manager stop on a label and take focus away from the field beside it.
	if tab {
		s |= ndWSTabstop
	}
	h, _, _ := ndCreateWin.Call(exStyle,
		uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(tp)),
		s|style,
		ndU(0), ndU(0), uintptr(f.px(ndInputW)), uintptr(f.px(ndInputH)),
		f.hwnd, uintptr(id), 0, 0)
	if h != 0 && font != 0 {
		ndSendMsg.Call(h, ndWMSetFont, font, 1)
	}
	if id != 0 {
		f.inputs[id] = h
	}
	return h
}

// createLabel makes a right-aligned grid label and records which field it
// belongs to, so WM_CTLCOLORSTATIC can paint it in the muted label colour.
func (f *ndForm) createLabel(text string, forID int) uintptr {
	h := f.createStatic(text, ndSSRight, f.fontLbl)
	f.labelID[h] = forID
	return h
}

// build creates every control once; layout() then positions the visible ones.
func (f *ndForm) build() {
	// The card title is the window caption itself ("Bad Proxy · Edit Node"), so
	// there is no in-card "Edit Node" heading to duplicate it.

	f.addRow("Name", f.editRow(ndIDName), ndIDName)
	f.setTxt(ndIDName, f.in.Name)

	f.addComboRow("Protocol", ndIDProto, ndProtocols, f.in.Protocol)
	f.addRow("Address", f.editRow(ndIDServer), ndIDServer)
	f.setTxt(ndIDServer, f.in.Server)
	f.addRow("Port", f.editRow(ndIDPort), ndIDPort)
	f.setTxt(ndIDPort, f.in.Port)
	f.addRow("Secret", f.editRow(ndIDSecret), ndIDSecret)
	f.setTxt(ndIDSecret, f.in.Secret)
	f.addComboRow("Transport", ndIDTransport, ndTransports, f.in.Transport)
	f.addRow("Domain (SNI/Host)", f.editRow(ndIDAuthority), ndIDAuthority)
	f.setTxt(ndIDAuthority, f.in.Authority)
	f.addRow("Path", f.editRow(ndIDPath), ndIDPath)
	f.setTxt(ndIDPath, f.in.Path)
	f.addRow("Padding range", f.editRow(ndIDPadding), ndIDPadding)
	f.setTxt(ndIDPadding, f.in.Padding)

	f.btns[0] = &ndBtn{hwnd: f.create("BUTTON", "Save", ndIDOK, ndBSOwner, f.font), text: "Save", primary: true}
	f.btns[1] = &ndBtn{hwnd: f.create("BUTTON", "Cancel", ndIDCancel, ndBSOwner, f.font), text: "Cancel"}
	for _, b := range f.btns {
		b.w = 2*f.px(ndBtnPadX) + f.textWidth(b.text)
	}

	f.layout()
	ndSetFocus.Call(f.inputs[ndIDName])
}

// editRow makes a text field. The 1px #d2d7de border is drawn by the parent
// (see ndFrame): an EDIT cannot be owner-drawn, and no edge style reproduces the
// CSS frame -- WS_EX_CLIENTEDGE looked promising and is actually 0x400, while
// 0x1000 (the value first guessed here) is WS_EX_RIGHT and just right-aligns the
// text without drawing anything.
func (f *ndForm) editRow(id int) uintptr {
	return f.createEx("EDIT", "", id, ndESAuto, 0, f.font, true)
}

// addRow appends a labeled input row (respecting the recorded visibility).
func (f *ndForm) addRow(labelText string, input uintptr, id int) {
	f.rows = append(f.rows, &ndRow{label: f.createLabel(labelText, id), input: input,
		visID: id, visible: f.visFor(id), frame: true})
}

// addComboRow makes a read-only dropdown. CBS_SORT must stay off: the list is
// addressed by index into the Go slice, and sorting would silently reassign
// every selection. The height handed to MoveWindow covers the closed control
// plus the whole list, otherwise the dropdown opens onto nothing.
func (f *ndForm) addComboRow(labelText string, id int, items []string, cur string) {
	combo := f.create("COMBOBOX", "", id, ndCBSList, f.font)
	for _, it := range items {
		pp, _ := syscall.UTF16PtrFromString(it)
		ndSendMsg.Call(combo, ndCBAdd, 0, uintptr(unsafe.Pointer(pp)))
	}
	ndSendMsg.Call(combo, ndCBSetS, uintptr(comboIndexOf(items, cur)), 0)
	f.rows = append(f.rows, &ndRow{
		label:   f.createLabel(labelText, id),
		input:   combo,
		visID:   id,
		visible: f.visFor(id),
		drop:    len(items) * f.px(ndInputH),
	})
}

// markVisibility records which optional rows show, using the shared
// visibleNodeFields rule. It is the single source of truth both before and
// after the rows exist, so the native editor and the HTML modal hide the same
// fields for the same protocol/transport pair.
func (f *ndForm) markVisibility(proto, trans string) {
	vf := visibleNodeFields(proto, trans)
	f.vis[ndIDSecret] = vf.Secret
	f.vis[ndIDAuthority] = vf.Authority
	f.vis[ndIDPath] = vf.Path
	f.vis[ndIDPadding] = vf.Padding
	for _, r := range f.rows {
		r.visible = f.visFor(r.visID)
	}
}

func (f *ndForm) visFor(id int) bool {
	if v, ok := f.vis[id]; ok {
		return v
	}
	return true // Name/Protocol/Address/Port/Transport always show
}

// layout reproduces .form-grid: a 110px right-aligned label column, the field
// filling the rest of the 380px card, 8px rows, then the actions flush right.
func (f *ndForm) layout() {
	rowH, gap := f.px(ndInputH), f.px(ndRowGap)
	labelX, labelW := f.px(ndLabelX), f.px(ndLabelW)
	inputX, inputW := f.px(ndInputX), f.px(ndInputW)
	inset := f.px(ndFrameInset)
	f.frames = f.frames[:0]

	y := f.px(ndPad)
	for _, r := range f.rows {
		if r.visible {
			if r.label != 0 {
				ndMoveWnd.Call(r.label, ndU(labelX), ndU(y), uintptr(labelW), uintptr(rowH), 1)
				ndShowWnd.Call(r.label, ndSWShow)
			}
			if r.frame {
				// The control sits inside the border the parent paints at this
				// rect, so the ring stays parent-only and can be drawn.
				f.frames = append(f.frames, ndFrame{
					r:  ndRect{int32(inputX), int32(y), int32(inputX + inputW), int32(y + rowH)},
					id: r.visID})
				ndMoveWnd.Call(r.input, ndU(inputX+inset), ndU(y+inset),
					uintptr(inputW-2*inset), uintptr(rowH-2*inset), 1)
			} else {
				ndMoveWnd.Call(r.input, ndU(inputX), ndU(y), uintptr(inputW), uintptr(rowH+r.drop), 1)
			}
			ndShowWnd.Call(r.input, ndSWShow)
			y += rowH + gap
		} else {
			if r.label != 0 {
				ndShowWnd.Call(r.label, ndSWHide)
			}
			ndShowWnd.Call(r.input, ndSWHide)
		}
	}
	y += f.px(ndActionsGap) - gap

	right := f.px(ndClientW) - f.px(ndPad)
	btnH := f.px(ndBtnH)
	// btns[0] is Save and goes flush right: index.html lists Cancel then Save,
	// so walking the slice in order and moving the right edge left each time
	// reproduces that arrangement.
	for _, b := range f.btns {
		if b == nil {
			continue
		}
		ndMoveWnd.Call(b.hwnd, ndU(right-b.w), ndU(y), uintptr(b.w), uintptr(btnH), 1)
		right -= b.w + f.px(ndBtnGap)
	}
	f.resize(y + btnH + f.px(ndPad))
	// The frames moved with the rows, so the whole card needs a repaint.
	ndInvalidate.Call(f.hwnd, 0, 1)
}

func (f *ndForm) resize(clientH int) {
	var r ndRect
	ndGetWndRect.Call(f.hwnd, uintptr(unsafe.Pointer(&r)))
	// The window was created before its real DPI was known, so recompute the
	// width too: leaving it at the 96-DPI value clips the right-hand column of
	// every control on a scaled display.
	outerW, outerH := f.outer(f.px(ndClientW), clientH)
	ndMoveWnd.Call(f.hwnd, uintptr(r.l), uintptr(r.top), uintptr(outerW), uintptr(outerH), 1)
}

// textWidth measures a button caption in the body font, so the button is sized
// from its content plus padding the way CSS would.
func (f *ndForm) textWidth(s string) int {
	dc, _, _ := ndGetDC.Call(f.hwnd)
	if dc == 0 {
		return f.px(60)
	}
	old, _, _ := ndSelectObj.Call(dc, f.font)
	p, _ := syscall.UTF16PtrFromString(s)
	var r ndRect
	ndDrawText.Call(dc, uintptr(unsafe.Pointer(p)), ndNoLen,
		uintptr(unsafe.Pointer(&r)), uintptr(ndDtCalcRect|ndDtSingleLine))
	ndSelectObj.Call(dc, old)
	ndReleaseDC.Call(f.hwnd, dc)
	return int(r.right - r.l)
}

// ndDrawItem mirrors DRAWITEMSTRUCT as laid out on 64-bit Windows.
type ndDrawItem struct {
	ctrlType uint32
	ctrlID   uint32
	itemID   uint32
	action   uint32
	state    uint32
	hwnd     uintptr
	dc       uintptr
	rect     ndRect
	itemData uintptr
}

// drawButton paints one push button in the web styles: solid blue with white
// text for .btn-primary, white with a hairline border for .btn-secondary, both
// with a 4px radius.
func (f *ndForm) drawButton(lParam uintptr) {
	// lParam is a DRAWITEMSTRUCT pointer. Copying it out with MoveMemory rather
	// than casting keeps the buffer's lifetime in Go's hands: nothing in the
	// struct is aliased after the callback returns.
	var item ndDrawItem
	ndMoveMem.Call(uintptr(unsafe.Pointer(&item)), lParam, uintptr(unsafe.Sizeof(item)))
	di := &item
	var b *ndBtn
	for _, btn := range f.btns {
		if btn != nil && btn.hwnd == di.hwnd {
			b = btn
		}
	}
	if b == nil {
		return
	}
	hdc := di.dc
	oldFont, _, _ := ndSelectObj.Call(hdc, f.font)
	ndSetBkMode.Call(hdc, ndBkTrans)

	// rcItem is in the parent's client coordinates while hDC is the button's own,
	// so painting from rcItem would shift every button by its offset in the card.
	box := di.rect
	var cr ndRect
	if ok, _, _ := ndGetClientRect.Call(di.hwnd, uintptr(unsafe.Pointer(&cr))); ok != 0 && cr.right > 0 {
		box = cr
	}

	fill := uintptr(ndBrushWhite)
	pressed := di.state&ndODSSelect != 0
	switch {
	case b.primary && pressed:
		fill = ndClrDeep
	case b.primary:
		fill = ndClrAccent
	case pressed:
		fill = ndClrHover
	}
	brush, _, _ := ndCreateBrush.Call(fill)
	nullPen, _, _ := ndStockObj.Call(ndNullPen)
	oldBrush, _, _ := ndSelectObj.Call(hdc, brush)
	oldPen, _, _ := ndSelectObj.Call(hdc, nullPen)
	r := uintptr(f.px(ndCornerDia))
	ndRoundRect.Call(hdc, ndU(int(box.l)), ndU(int(box.top)),
		ndU(int(box.right)), ndU(int(box.bottom)), r, r)

	if !b.primary {
		// Hairline border on the same rounded path, filled with nothing.
		pen, _, _ := ndCreatePen.Call(ndPsSolid, 1, ndClrBorder)
		hollow, _, _ := ndStockObj.Call(ndNullBrush)
		ndSelectObj.Call(hdc, pen)
		ndSelectObj.Call(hdc, hollow)
		ndRoundRect.Call(hdc, ndU(int(box.l)), ndU(int(box.top)),
			ndU(int(box.right)), ndU(int(box.bottom)), r, r)
		ndDeleteObj.Call(pen)
	}

	// ndBrushWhite is 0xFFFFFF, which doubles as the COLORREF for white text.
	textColor := uintptr(ndClrText)
	if b.primary {
		textColor = ndBrushWhite
	}
	ndSetTextColor.Call(hdc, textColor)
	tp, _ := syscall.UTF16PtrFromString(b.text)
	ndDrawText.Call(hdc, uintptr(unsafe.Pointer(tp)), ndNoLen, uintptr(unsafe.Pointer(&box)),
		uintptr(ndDtCenter|ndDtVCenter|ndDtSingleLine))

	ndSelectObj.Call(hdc, oldBrush)
	ndSelectObj.Call(hdc, oldPen)
	ndSelectObj.Call(hdc, oldFont)
	ndDeleteObj.Call(brush)
}

// ndPaint mirrors PAINTSTRUCT.
type ndPaint struct {
	hdc                 uintptr
	ferase              uint32
	_                   uint32
	rect                ndRect
	frestore, fincupdat uint32
	reserved            [32]byte
}

// paintFrames draws the .form-grid field borders. An EDIT cannot be owner-drawn
// and no edge style gives a 1px #d2d7de rounded frame, so the parent paints the
// ring that its children deliberately leave uncovered.
func (f *ndForm) paintFrames() {
	var ps ndPaint
	hdc, _, _ := ndBeginPaint.Call(f.hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	penW := uintptr(1)
	d := uintptr(f.px(ndCornerDia))
	for _, fr := range f.frames {
		col := uintptr(ndClrBorder)
		if fr.id == f.focus {
			col = ndClrAccent
		}
		pen, _, _ := ndCreatePen.Call(ndPsSolid, penW, col)
		oldPen, _, _ := ndSelectObj.Call(hdc, pen)
		nullBrush, _, _ := ndStockObj.Call(ndNullBrush)
		oldBrush, _, _ := ndSelectObj.Call(hdc, nullBrush)
		ndRoundRect.Call(hdc, ndU(int(fr.r.l)), ndU(int(fr.r.top)),
			ndU(int(fr.r.right)), ndU(int(fr.r.bottom)), d, d)
		ndSelectObj.Call(hdc, oldPen)
		ndSelectObj.Call(hdc, oldBrush)
		ndDeleteObj.Call(pen)
	}
	ndEndPaint.Call(f.hwnd, uintptr(unsafe.Pointer(&ps)))
}

func (f *ndForm) onCommand(wParam uintptr) {
	notify := wParam >> 16
	id := int(wParam & 0xFFFF)
	switch {
	case id == ndIDProto || id == ndIDTransport:
		if notify == ndCBNSel {
			f.markVisibility(f.comboSel(ndIDProto, ndProtocols), f.comboSel(ndIDTransport, ndTransports))
			f.layout()
		}
	case id == ndIDOK && notify == ndBNClick:
		f.accept()
	case id == ndIDCancel && notify == ndBNClick:
		f.close()
	case notify == ndENFocus || notify == ndENKill:
		// .form-grid input:focus turns the border accent-coloured; the frame is
		// drawn by the parent, so it has to be told when the caret moves.
		if notify == ndENFocus {
			f.focus = id
		} else if f.focus == id {
			f.focus = 0
		}
		ndInvalidate.Call(f.hwnd, 0, 0)
	}
}

func (f *ndForm) accept() {
	p, t := f.comboSel(ndIDProto, ndProtocols), f.comboSel(ndIDTransport, ndTransports)
	vf := visibleNodeFields(p, t)
	n := Node{
		Name:      f.getTrim(ndIDName),
		Protocol:  p,
		Server:    f.getTrim(ndIDServer),
		Port:      f.getTrim(ndIDPort),
		Transport: t,
	}
	if vf.Secret {
		n.Secret = f.getTrim(ndIDSecret)
	}
	if vf.Authority {
		n.Authority = f.getTrim(ndIDAuthority)
	}
	if vf.Path {
		n.Path = f.getTrim(ndIDPath)
	}
	if vf.Padding {
		n.Padding = f.getTrim(ndIDPadding)
	}
	if n.Name == "" || n.Server == "" || n.Port == "" {
		showMessageBox("Bad Proxy", "Name, address and port are required", false)
		return
	}
	f.out = n
	f.proved = true
	f.destroy()
}

func (f *ndForm) close() {
	f.proved = false
	f.destroy()
}

func (f *ndForm) destroy() {
	if f.closed {
		return
	}
	f.closed = true
	owner := mainWindowHwnd()
	// Bring the owner to top before destroying the dialog, so there is no
	// visible flicker: the main window is already on top when the child disappears.
	if owner != 0 {
		procBringToTop.Call(owner)
		procSetForeground.Call(owner)
	}
	ndDestroyWnd.Call(f.hwnd)
}
