//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/wwricu/bad-proxy-gui/tunplat"
)

// Drives the native settings window with Win32 messages, the way the node
// editor self-test drives its form, and checks it against the same CSS box
// model. Enable with:
//
//	BADPROXY_GUI_SELFTEST=1 go test -tags production -run TestSettingsDialogSelfTest -count=1 -v
//
// stIsWindow asks Windows whether a handle still names a window.
var stIsWindow = stUser32.NewProc("IsWindow")

const stCBGetLBText = 0x0148

// stAlive reports whether a window handle still names a live window: a dialog
// that closes itself on a rejected draft is the bug this checks for.
func stAlive(h uintptr) bool {
	r, _, _ := stIsWindow.Call(h)
	return r != 0
}

// stComboText reads one item out of a dropdown, so the labels can be proved to
// be the words the HTML card shows rather than the stored value.
func stComboText(h uintptr, idx int) string {
	buf := make([]uint16, 256)
	if int32(stSend(h, stCBGetLBText, uintptr(idx), uintptr(unsafe.Pointer(&buf[0])))) < 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// stMessageBox waits for the OS alert a rejected draft pops, reads the message
// off its Static, and presses OK so the dialog thread is not left waiting.
func stMessageBox(t *testing.T, d time.Duration) string {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		var box uintptr
		cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
			if stClassName(h) == "#32770" {
				if v, _, _ := stIsWinVisible.Call(h); v != 0 {
					box = h
					return 0
				}
			}
			return 1
		})
		stEnumWindows.Call(cb, 0)
		if box != 0 {
			// The alert has two Statics: the icon, which has no text and comes
			// first, and the message. Reading the first one sees only "".
			text := ""
			var parts []string
			child := syscall.NewCallback(func(h, _ uintptr) uintptr {
				if stClassName(h) == "Static" {
					if s := stText(h); s != "" {
						parts = append(parts, s)
						if text == "" {
							text = s
						}
					}
				}
				return 1
			})
			stEnumChildWindows.Call(box, child, 0)
			if text == "" {
				t.Errorf("BUG: an alert window appeared but says nothing the user can read (%v)", parts)
			}
			stPostMsg.Call(box, ndWMCommand, uintptr(ndIDOK), 0)
			time.Sleep(200 * time.Millisecond)
			return text
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Nothing answered in time: name the windows this process does own, so the
	// failure says whether the alert never existed or only went unrecognised.
	var own []string
	lcb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		var pid uint32
		stGetWinThread.Call(h, uintptr(unsafe.Pointer(&pid)))
		if int(pid) == os.Getpid() {
			v, _, _ := stIsWinVisible.Call(h)
			own = append(own, fmt.Sprintf("%s %q vis=%v", stClassName(h), stText(h), v != 0))
		}
		return 1
	})
	stEnumWindows.Call(lcb, 0)
	t.Logf("no alert window found; this process owns: %v", own)
	return ""
}

// stCheckState reads a check box, and stTick sets it the way the user would.
func stCheckState(t *testing.T, kids []stChild, id int) (bool, bool) {
	c, ok := stFind(kids, id)
	if !ok {
		t.Errorf("BUG: check box id=%d is missing", id)
		return false, false
	}
	return stSend(c.hwnd, ndBMGet, 0, 0) == ndBSTChecked, c.enabled
}

func stTick(h uintptr, on bool) {
	var s uintptr
	if on {
		s = ndBSTChecked
	}
	stSend(h, ndBMSet, s, 0)
}

func TestSettingsDialogSelfTest(t *testing.T) {
	if os.Getenv("BADPROXY_GUI_SELFTEST") != "1" {
		t.Skip("set BADPROXY_GUI_SELFTEST=1 to open the native settings window")
	}

	in := Settings{HttpPort: "10809", SocksPort: "10808", ProxyMode: modeProxy,
		RoutingMode: modeCNWhitelist, TunEnabled: true, HideTray: true}

	// The window applies its own draft, so the self-test owns what "apply" means:
	// fail makes it report a tunnel that could not come up, which has to be read
	// on the window rather than in an alert or on the main window's bar.
	var fail atomic.Bool
	apply := func(Settings) error {
		if fail.Load() {
			return errors.New("TUN failed: the adapter needs elevation")
		}
		return nil
	}

	done := make(chan struct{})
	var out Settings
	var confirmed bool
	go func() {
		out, confirmed = runSettingsDialog(in, apply)
		close(done)
	}()

	hwnd := stTopWindow("BadProxyForm", 5*time.Second)
	if hwnd == 0 {
		t.Fatal("settings window never appeared")
	}
	if got := stText(hwnd); got != "Bad Proxy · Settings" {
		t.Errorf("UI BUG: window caption is %q, want %q", got, "Bad Proxy · Settings")
	}

	kids := stChildren(hwnd)
	t.Logf("child controls: %d", len(kids))
	for _, c := range kids {
		t.Logf("  %-9s id=%-4d style=0x%x rect=(%d,%d,%d,%d) vis=%v en=%v text=%q",
			c.class, c.id, c.style, c.l, c.t, c.r, c.b, c.visible, c.enabled, stText(c.hwnd))
	}
	if len(kids) == 0 {
		t.Fatal("BUG: the settings window has no child controls at all")
	}
	// The message line is part of the window but says nothing until OK is
	// refused: an empty row the user stares at is its own UI bug.
	msgC, ok := stFind(kids, sdIDMsg)
	if !ok {
		t.Error("BUG: the settings window has no inline message line to report with")
	} else if msgC.visible || stText(msgC.hwnd) != "" {
		t.Errorf("BUG: the message line opens visible and empty (vis=%v text=%q)", msgC.visible, stText(msgC.hwnd))
	}
	img := stShot(t, hwnd, "badproxy-settings-dialog")
	checks := stCheckGrid(t, hwnd, kids, img, "OK")

	wantChecks := 3 // LAN, start at login, hide tray
	if tunplat.Supported() {
		wantChecks++
	}
	if len(checks) != wantChecks {
		t.Errorf("UI BUG: %d check boxes, want %d", len(checks), wantChecks)
	}

	// The options block spans the card: 16px padding on each side, no label
	// column, the way a native dialog groups its checks.
	dpi := int(ndDpi(hwnd))
	px := func(v int) int { return v * dpi / ndDefaultDpi }
	ox, _ := stClientOrigin(hwnd)
	for _, c := range checks {
		if got, want := int(c.l)-ox, px(ndPad); got < want-2 || got > want+2 {
			t.Errorf("UI BUG: check box %q starts at x=%d, the card pads it at %d", stText(c.hwnd), got, want)
		}
		if got, want := int(c.r)-ox, px(ndClientW)-px(ndPad); got < want-2 || got > want+2 {
			t.Errorf("UI BUG: check box %q ends at x=%d, the card is %dpx wide", stText(c.hwnd), got, px(ndClientW))
		}
	}

	// Draft in: every widget must open with what the app is actually running.
	for id, want := range map[int]string{sdIDHTTP: in.HttpPort, sdIDSOCKS: in.SocksPort} {
		c, ok := stFind(kids, id)
		if !ok {
			t.Errorf("BUG: port field id=%d missing", id)
			continue
		}
		if got := stGet(c.hwnd); got != want {
			t.Errorf("BUG: field id=%d holds %q, the running settings say %q", id, got, want)
		}
	}
	for _, pair := range []struct {
		id     int
		labels []string
		cur    string
	}{
		{sdIDMode, sdModeLabels, in.ProxyMode},
		{sdIDRouting, sdRouteLabels, in.RoutingMode},
	} {
		c, ok := stFind(kids, pair.id)
		if !ok {
			t.Errorf("BUG: dropdown id=%d missing", pair.id)
			continue
		}
		if n := stCombo(c.hwnd, stCBGetCount, 0); n != len(pair.labels) {
			t.Errorf("BUG: dropdown id=%d holds %d items, want %d", pair.id, n, len(pair.labels))
		}
		idx := comboIndexOf(pairValues(pair.labels, pair.id), pair.cur)
		if sel := stCombo(c.hwnd, stCBGetCur, 0); sel != idx {
			t.Errorf("BUG: dropdown id=%d opens on %d, want %d (%q)", pair.id, sel, idx, pair.cur)
		}
		for i, want := range pair.labels {
			if got := stComboText(c.hwnd, i); got != want {
				t.Errorf("UI BUG: dropdown id=%d item %d reads %q, the HTML card shows %q", pair.id, i, got, want)
			}
		}
	}
	for id, want := range map[int]bool{sdIDTun: true, sdIDLAN: false, sdIDLogin: false, sdIDTray: true} {
		if !tunplat.Supported() && id == sdIDTun {
			continue
		}
		got, enabled := stCheckState(t, kids, id)
		if got != want {
			t.Errorf("BUG: check box id=%d is %v, the running settings say %v", id, got, want)
		}
		if !enabled {
			t.Errorf("BUG: check box id=%d is disabled but the setting is reachable", id)
		}
	}

	// A tunnel over a direct engine carries traffic nowhere, so the mode owns
	// that box: direct unticks and disables it, WM_COMMAND has to be routed.
	if tunplat.Supported() {
		modeC, ok := stFind(kids, sdIDMode)
		if !ok {
			t.Fatal("BUG: Mode dropdown missing, cannot check the TUN coupling")
		}
		stCombo(modeC.hwnd, stCBSetCur, uintptr(comboIndexOf(sdModes, modeDirect)))
		stCmd(hwnd, sdIDMode, ndCBNSel, modeC.hwnd)
		if got, enabled := stCheckState(t, stChildren(hwnd), sdIDTun); enabled || got {
			t.Errorf("BUG: direct mode left TUN enabled=%v checked=%v - OK would silently drop it", enabled, got)
		} else {
			t.Log("WM_COMMAND routing ok: direct disabled and unticked TUN")
		}
		// The untick is not re-applied on the way back: the box stays where the
		// user can see it is off rather than pretending the mode switch kept it on.
		stCombo(modeC.hwnd, stCBSetCur, uintptr(comboIndexOf(sdModes, modeProxy)))
		stCmd(hwnd, sdIDMode, ndCBNSel, modeC.hwnd)
		if _, enabled := stCheckState(t, stChildren(hwnd), sdIDTun); !enabled {
			t.Error("BUG: TUN stayed disabled after going back to proxy mode")
		}
	}

	// A draft the engine cannot bind must be refused in place: the reason on the
	// window's own message line, no alert, and the edits still there to fix.
	msgC, _ = stFind(stChildren(hwnd), sdIDMsg)
	portC, _ := stFind(stChildren(hwnd), sdIDHTTP)
	stSet(portC.hwnd, in.SocksPort)
	stCmd(hwnd, ndIDOK, ndBNClick, 0)
	if !stAlive(hwnd) {
		t.Fatal("BUG: a rejected draft closed the settings window and lost the edits")
	}
	if got := stText(msgC.hwnd); !strings.Contains(strings.ToLower(got), "differ") {
		t.Errorf("BUG: two identical ports were refused with %q on the message line", got)
	} else {
		t.Logf("rejected draft ok: message=%q, window alive", got)
	}
	// An alert here would be both a second window to hunt and, historically, the
	// thing that disabled the WebView2 host and killed the app.
	if box := stMessageBox(t, 300*time.Millisecond); box != "" {
		t.Errorf("BUG: a validation failure still raises an alert: %q", box)
	}

	// An apply that fails - the tunnel needing elevation is the real case - must
	// also stay on this window, in red, with the draft intact.
	fail.Store(true)
	stSet(portC.hwnd, "18081")
	stCmd(hwnd, ndIDOK, ndBNClick, 0)
	if !stAlive(hwnd) {
		t.Fatal("BUG: a failed apply closed the settings window; the reason went to the status bar")
	}
	if c, ok := stFind(stChildren(hwnd), sdIDMsg); !ok {
		t.Error("BUG: the message line vanished from the window")
	} else if got := stText(c.hwnd); !strings.Contains(got, "TUN failed") {
		t.Errorf("BUG: the failed apply reported %q, not the tunnel error", got)
	}
	if got := stGet(portC.hwnd); got != "18081" {
		t.Errorf("BUG: the failed apply threw the port away, it holds %q", got)
	}
	if confirmed {
		t.Error("BUG: a failed apply reported a confirmed window")
	}

	// Fix the draft, tick LAN, pick another routing preset, then OK.
	fail.Store(false)
	stSet(portC.hwnd, "18080")
	lanC, _ := stFind(stChildren(hwnd), sdIDLAN)
	stTick(lanC.hwnd, true)
	routeC, _ := stFind(stChildren(hwnd), sdIDRouting)
	stCombo(routeC.hwnd, stCBSetCur, uintptr(comboIndexOf(sdRoutes, modeGFWBlacklist)))
	stCmd(hwnd, ndIDOK, ndBNClick, 0)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("BUG: the window never closed after OK on a valid draft")
	}
	t.Logf("returned confirmed=%v settings=%+v", confirmed, out)
	if !confirmed {
		t.Error("BUG: OK did not confirm the settings window")
		return
	}
	if out.HttpPort != "18080" || out.SocksPort != in.SocksPort {
		t.Errorf("BUG: ports read back as %q/%q", out.HttpPort, out.SocksPort)
	}
	if out.ProxyMode != modeProxy {
		t.Errorf("BUG: mode read back as %q", out.ProxyMode)
	}
	if out.RoutingMode != modeGFWBlacklist {
		t.Errorf("BUG: routing read back as %q", out.RoutingMode)
	}
	if !out.AllowLAN {
		t.Error("BUG: the LAN check box did not reach the draft")
	}
	if out.TunEnabled {
		t.Error("BUG: TUN came back ticked after direct mode unticked it")
	}
	if !out.HideTray || out.StartLogin {
		t.Errorf("BUG: untouched options changed: %+v", out)
	}
}

// pairValues maps a dropdown's labels onto the values the draft stores.
func pairValues(labels []string, id int) []string {
	switch id {
	case sdIDMode:
		return sdModes
	case sdIDRouting:
		return sdRoutes
	}
	return labels
}

// TestSettingsDialogCancel proves the other half of apply-on-OK: dismissing the
// window reports nothing, so the caller leaves the running proxy alone.
func TestSettingsDialogCancel(t *testing.T) {
	if os.Getenv("BADPROXY_GUI_SELFTEST") != "1" {
		t.Skip("set BADPROXY_GUI_SELFTEST=1 to open the native settings window")
	}
	in := Settings{HttpPort: "10809", SocksPort: "10808", ProxyMode: modeDirect,
		RoutingMode: modeGlobal}
	done := make(chan struct{})
	var out Settings
	var confirmed bool
	go func() {
		out, confirmed = runSettingsDialog(in, func(Settings) error {
			t.Error("BUG: a dismissed window still applied its draft")
			return nil
		})
		close(done)
	}()
	hwnd := stTopWindow("BadProxyForm", 5*time.Second)
	if hwnd == 0 {
		t.Fatal("settings window never appeared")
	}
	// Typing without pressing OK must not reach the caller.
	if c, ok := stFind(stChildren(hwnd), sdIDHTTP); ok {
		stSet(c.hwnd, "19999")
	}
	stPostMsg.Call(hwnd, ndWMClose, 0, 0)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("BUG: the settings window did not close on WM_CLOSE")
	}
	if confirmed {
		t.Error("BUG: closing the window reported confirmed settings")
	}
	if out != in {
		t.Errorf("BUG: a dismissed window handed back %+v, want the untouched draft %+v", out, in)
	} else {
		t.Log("cancel ok: draft returned unchanged")
	}
}
