//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wwricu/bad-proxy-gui/internal/config"
)

// The macOS half of the form-window self-test. It drives the app the way the
// buttons do - a click on the real node list opens the editor through a binding,
// so the window is created from inside the WebKit message callback rather than
// from a goroutine, which is the only way AppKit's re-entrancy rules get tested.
//
//	BADPROXY_GUI_SELFTEST=1 go test -tags production -count=1 -v .
//
// An ordinary "go test ./..." run skips it: it opens real windows and needs a
// window server. Everything is written to a temp directory seeded with a
// synthetic node, so a real state.json is never opened. The env var replaces the
// whole run, so the unit tests are skipped in that mode - same as the Windows
// harness.
//
// The Windows file has the same shape and the same probe protocol; the two are
// separate because stacking and pixels are read through Win32 there and through
// AppKit here.
const selfTestEnv = "BADPROXY_GUI_SELFTEST"

func TestMain(m *testing.M) {
	if os.Getenv(selfTestEnv) == "" {
		os.Exit(m.Run())
	}
	os.Exit(runFormSelfTest())
}

type checker struct {
	n     int
	fails []string
}

func (c *checker) ok(format string, args ...any) {
	fmt.Printf("  ok   %s\n", fmt.Sprintf(format, args...))
}

func (c *checker) fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("  FAIL %s\n", msg)
	c.fails = append(c.fails, msg)
}

func (c *checker) check(cond bool, format string, args ...any) {
	if cond {
		c.ok(format, args...)
		return
	}
	c.fail(format, args...)
}

func runFormSelfTest() int {
	dir, err := os.MkdirTemp("", "bpg-form-selftest-*")
	if err != nil {
		fmt.Println("temp dir:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	c := &checker{}

	// High ports: the runner may already hold 10808/10809.
	st := config.DefaultState()
	st.HttpPort, st.SocksPort = "11909", "11908"
	st.Nodes = []config.Node{{
		Name: "alpha", Protocol: "vless", Server: "alpha.example.invalid", Port: "443",
		Secret: "synthetic-uuid", Transport: "xhttp", Path: "/bp",
		Authority: "alpha.example.invalid", Mode: "packet-up",
	}}
	st.Active = 0
	seed, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		fmt.Println("encode seed state:", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), seed, 0o600); err != nil {
		fmt.Println("seed state:", err)
		return 1
	}

	a := &App{store: config.NewStore(dir)}
	if a.st, err = a.store.Load(); err != nil {
		fmt.Println("load seed state:", err)
		return 1
	}

	frontend, err := fs.Sub(bundled, "frontend")
	if err != nil {
		fmt.Println("assets:", err)
		return 1
	}
	wails := application.New(application.Options{
		Name:        windowTitle,
		Description: "Bad Proxy client",
		Mac: application.MacOptions{
			// Same as production: closing the list is not a request to leave.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Assets:   application.AssetOptions{Handler: application.BundledAssetFileServer(frontend)},
		Services: []application.Service{application.NewService(a)},
	})
	a.wails = wails
	w, h, minW, minH := mainWindowSize(len(st.Nodes))
	a.mainWindow = wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      mainWindowName,
		Title:     windowTitle,
		Width:     w,
		Height:    h,
		MinWidth:  minW,
		MinHeight: minH,
		URL:       "/?selftest=1",
	})

	probes := make(chan map[string]any, 256)
	wails.Event.On("selftest", func(e *application.CustomEvent) {
		if m, ok := e.Data.(map[string]any); ok {
			select {
			case probes <- m:
			default:
			}
		}
	})
	var finished atomic.Bool
	wails.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			checkForms(a, c, probes, dir)
			finished.Store(true)
			// The verdict leaves the process from in here, and nothing after it.
			// Quit is -[NSApp terminate:], which exits 0 inside the call, so a
			// tally reached only after Run() returns - or after checkForms has
			// called Quit - is Windows-only, and every failed check would be
			// reported green. A self-test that cannot fail is worse than none,
			// because it reads as coverage.
			os.Exit(c.exit())
		}()
	})

	if err := wails.Run(); err != nil {
		fmt.Println("run:", err)
		return 1
	}
	if !finished.Load() {
		fmt.Println("the checks never ran")
		return 1
	}
	return c.exit()
}

// exit turns the tally into a process status.
func (c *checker) exit() int {
	if len(c.fails) > 0 {
		fmt.Printf("\n%d check(s) failed\n", len(c.fails))
		return 1
	}
	fmt.Println("\nall form window checks passed")
	return 0
}

// checkForms is the script. Every step starts from a click in a page, so each
// window below is created the way a user creates it.
func checkForms(a *App, c *checker, probes chan map[string]any, dir string) {
	// A WKWebView on a CI runner loads its first page slowly, and the bindings
	// only answer once the runtime has been injected.
	time.Sleep(4 * time.Second)
	win := a.mainWindow

	// ---- the main window ----
	// Reaching this at all proves the app survived being launched headless and
	// that the binding name the frontend tries first is the one this build
	// registers.
	state := probeUntil(win, probes, `
		(() => ({
			ports: ["roHttp","roSocks"].map(id => document.getElementById(id).textContent).join("/"),
			rows: document.querySelectorAll(".node-item").length,
		}))()`, func(s string) bool { return strings.Contains(s, "11909") }, 30*time.Second)
	c.check(strings.Contains(state, `"rows":1`), "the list window drew the seeded node: %s", state)

	// ---- the minimum size ----
	// The floor is a promise about the list, and a window height is not a client
	// height: AppKit charges the caption before the page sees anything, and it
	// charges a different amount than Win32 does. So the arithmetic in main.go is
	// read back off the page here rather than trusted.
	fw, fh, minW, minH := mainWindowSize(len(a.st.Nodes))
	// Height() reads the live frame: the creation must already honour the floor.
	c.check(win.Height() >= minH,
		"the window opens on or above its own floor: frame %d of %d", win.Height(), minH)
	win.SetSize(minW, minH)
	time.Sleep(800 * time.Millisecond)
	floor := probe(win, probes, `
		(() => {
			const d = document.documentElement;
			const bar = document.querySelector(".status-bar");
			return {client: [d.clientWidth, d.clientHeight],
					bar: [bar.clientWidth, bar.scrollWidth]};
		})()`)
	var floorBox struct {
		Client [2]int `json:"client"`
		Bar    [2]int `json:"bar"`
	}
	c.check(json.Unmarshal([]byte(floor), &floorBox) == nil, "the window reports its own floor: %s", floor)
	fmt.Printf("  main   floor: window=%dx%d client=%dx%d bar=%v\n",
		minW, minH, floorBox.Client[0], floorBox.Client[1], floorBox.Bar)
	if floorBox.Client[1] > 0 {
		c.check(floorBox.Bar[1] <= floorBox.Bar[0]+1,
			"the whole bar fits the floor width: %v", floorBox.Bar)
		listH := floorBox.Client[1] - mainWindowBarH
		c.check(listH >= mainWindowListH+mainWindowMinRows*mainWindowRowH,
			"the floor shows %d whole node rows: the list has %d px", mainWindowMinRows, listH)
		// The same half of the promise the Windows sheet makes: the caption is
		// charged once, from the side the convention reads it - MinHeight is a
		// frame, the page below it is what the design asked for. If the caption
		// grew past the constant, the difference shows here as the page coming
		// back shorter than the floor promised.
		c.check(minH-floorBox.Client[1] <= mainWindowChromeH,
			"the chrome constant covers what this display charges: %d of %d",
			minH-floorBox.Client[1], mainWindowChromeH)
	}
	win.SetSize(fw, fh)
	time.Sleep(400 * time.Millisecond)

	// ---- the row menu ----
	// macOS has no native shortcut menu behind this, so the DOM menu is what the
	// user gets, and the list the Windows build hands its OS menu is read from
	// the same page. Both are the same three words in the same order.
	menu := probe(win, probes, `
		(() => ({
			items: [...document.querySelectorAll("#ctxMenu .ctx-item")]
				.map((el) => el.textContent.trim()),
			native: window.selftest.menuLabels,
		}))()`)
	c.check(strings.Contains(menu, `"Edit","Test","HTTP test","Delete"`),
		"the row menu offers Edit above Test: %s", menu)

	// ---- opening the editor: the step that ships broken ----
	probe(win, probes, `(() => { document.getElementById("addNodeBtn").click(); return "sent"; })()`)
	node := waitForm(a, nodeWindowPrefix+"new", 10*time.Second)
	if node == nil {
		c.fail("the add button never opened the editor window")
		return
	}
	c.ok("the add button opened the editor in a window of its own")
	c.check(node.NativeWindow() != nil, "the editor has a native window behind it")

	// The second webview has to load its own page and reach the same bindings.
	// If the process dies here, the crash is in the second window's asset load
	// rather than in its creation.
	fields := probeUntil(node, probes, formGeometry, func(s string) bool {
		return strings.Contains(s, "xhttp") && fitted(s) && loopDone(s)
	}, 30*time.Second)
	c.check(strings.Contains(fields, `"fSecret","fAuthority","fPath","fMode"`),
		"xhttp offers every optional field: %s", fields)
	c.check(strings.Contains(fields, `"scroll":false`),
		"the tallest form needs no scrollbar: %s", fields)
	c.check(gapOf(fields) <= 32, "no dead band above the editor's buttons: %s", fields)
	// The form asks its window for exactly the height it needs, so anything more
	// than a pixel of rounding under the buttons is a window that did not listen.
	c.check(belowOf(fields) >= 0 && belowOf(fields) <= 12,
		"the editor's buttons sit at the bottom edge: %s", fields)
	nw, nh := node.Size()
	fmt.Printf("  form   node editor: window=%dx%d %s\n", nw, nh, fields)
	time.Sleep(1200 * time.Millisecond)
	c.shot("node-add")

	// ---- the form that changes while it is open ----
	// tls drops three rows at once. The window was sized for xhttp, so unless it
	// follows the fields the buttons slide up off the bottom edge and leave a
	// band of bare window under them - the failure a fixed form height cannot
	// see coming, because the form is only tallest when it first opens.
	probe(node, probes, `(() => { const s = document.getElementById("fTransport"); s.value = "tls"; s.dispatchEvent(new Event("change")); return "sent"; })()`)
	shrunk := probeUntil(node, probes, formGeometry, func(s string) bool {
		return strings.Contains(s, `"n":2`) && settled(s) && loopDone(s)
	}, 15*time.Second)
	c.check(strings.Contains(shrunk, `"scroll":false`),
		"the short form needs no scrollbar: %s", shrunk)
	c.check(heightOf(shrunk) > 0 && heightOf(shrunk) < heightOf(fields),
		"the window shrank with its fields: %s after %s", shrunk, fields)
	c.check(belowOf(shrunk) >= 0 && belowOf(shrunk) <= 12,
		"the short form's buttons hold the bottom edge: %s", shrunk)
	time.Sleep(1200 * time.Millisecond)
	c.shot("node-tls")

	// Growing back is the same promise in the other direction.
	probe(node, probes, `(() => { const s = document.getElementById("fTransport"); s.value = "xhttp"; s.dispatchEvent(new Event("change")); return "sent"; })()`)
	grown := probeUntil(node, probes, formGeometry, func(s string) bool {
		return strings.Contains(s, `"n":4`) && heightOf(s) == heightOf(fields) && loopDone(s)
	}, 15*time.Second)
	c.check(heightOf(grown) == heightOf(fields),
		"putting the rows back puts the window back: %s vs %s", grown, fields)

	// An owned window must be on screen the moment it exists, and it must not
	// have been shoved somewhere else by the ownership call.
	c.check(node.IsVisible(), "the editor is visible")
	x, y := node.Position()
	c.check(x != 0 || y != 0, "the editor landed on a screen: %d,%d", x, y)

	// ---- the refusal: the other macOS-only native call ----
	// A second request against an open form raises the window that has the floor
	// and asks the OS for attention. On macOS that is requestUserAttention, and
	// it is reached from a binding, so it runs while the main thread is inside a
	// WebKit callback.
	beforeCount := countForms(a, nodeWindowPrefix)
	probe(win, probes, `(() => { document.getElementById("addNodeBtn").click(); return "sent"; })()`)
	time.Sleep(1500 * time.Millisecond)
	c.check(countForms(a, nodeWindowPrefix) == beforeCount,
		"the plus pressed twice leaves one editor: %d", countForms(a, nodeWindowPrefix))
	c.check(formName(a.anyFormWindow()) == nodeWindowPrefix+"new",
		"the refusal kept the editor it refused: %q", formName(a.anyFormWindow()))

	// Cancel is the exit that writes nothing. Closing goes through the
	// WindowClosing hook, which emits at the list - the other direction across
	// windows.
	probe(node, probes, `(() => { document.getElementById("modalCancel").click(); return "sent"; })()`)
	c.check(waitClosed(a, nodeWindowPrefix+"new", 10*time.Second), "Cancel closes the editor")
	c.check(len(stateFromFile(dir).Nodes) == 1, "a cancelled editor wrote nothing")

	// ---- the settings window, from the other button ----
	probe(win, probes, `(() => { document.getElementById("gearBtn").click(); return "sent"; })()`)
	settings := waitForm(a, settingsWindowName, 10*time.Second)
	if settings == nil {
		c.fail("the gear never opened the settings window")
		return
	}
	c.ok("the gear opened the settings window")
	prefill := probeUntil(settings, probes, settingsGeometry,
		func(s string) bool {
			return strings.Contains(s, `"ports":"11909/11908"`) && fitted(s) && loopDone(s)
		}, 30*time.Second)
	c.check(strings.Contains(prefill, `"ports":"11909/11908"`),
		"the settings form filled itself from the draft: %s", prefill)
	c.check(strings.Contains(prefill, `"loginRow":"flex"`),
		"the start-at-login row is offered on macOS: %s", prefill)
	c.check(strings.Contains(prefill, `"scroll":false`),
		"the settings form needs no scrollbar: %s", prefill)
	c.check(gapOf(prefill) <= 32, "no dead band above the settings buttons: %s", prefill)
	c.check(belowOf(prefill) >= 0 && belowOf(prefill) <= 12,
		"the settings buttons sit at the bottom edge: %s", prefill)
	var row struct {
		Updater struct {
			Same     bool `json:"same"`
			First    bool `json:"first"`
			LeftGap  int  `json:"leftGap"`
			RightGap int  `json:"rightGap"`
			Beside   bool `json:"beside"`
			Clear    int  `json:"clear"`
		} `json:"updater"`
	}
	c.check(json.Unmarshal([]byte(prefill), &row) == nil,
		"the settings page reports its button row: %s", prefill)
	c.check(row.Updater.Same && row.Updater.First,
		"the rules updater shares the button row, ahead of Cancel: %s", prefill)
	c.check(row.Updater.LeftGap == row.Updater.RightGap && row.Updater.LeftGap > 0,
		"the updater holds the row's left edge as firmly as OK holds the right: %d and %d",
		row.Updater.LeftGap, row.Updater.RightGap)
	c.check(row.Updater.Beside && row.Updater.Clear > 0,
		"the updater's message sits to its right and stops short of Cancel: %d px clear",
		row.Updater.Clear)
	sw, sh := settings.Size()
	fmt.Printf("  form   settings: window=%dx%d %s\n", sw, sh, prefill)
	time.Sleep(1200 * time.Millisecond)
	c.shot("settings")
	probe(settings, probes, `(() => { document.getElementById("settingsCancel").click(); return "sent"; })()`)
	c.check(waitClosed(a, settingsWindowName, 10*time.Second), "Cancel closes the settings window")

	// ---- hide and reopen with a form still open ----
	// An owned window is not hidden along with its owner, so the app takes it
	// itself. platform.BecomeForeground/platform.BecomeBackground reach into NSApp from here.
	probe(win, probes, `(() => { document.getElementById("gearBtn").click(); return "sent"; })()`)
	settings = waitForm(a, settingsWindowName, 10*time.Second)
	if settings == nil {
		c.fail("the gear never reopened the settings window")
		return
	}
	// Measured, not asserted: AppKit will not release a child window while its
	// parent is on screen, so the form's own Hide is a no-op here and it is the
	// list going away that takes the form with it. The next check is the one the
	// app depends on.
	a.hideFormWindow()
	time.Sleep(500 * time.Millisecond)
	fmt.Printf("  note   the form outlives its own Hide while the list is up: form=%v\n",
		settings.IsVisible())
	a.hideToTray()
	c.check(waitVisible(false, 5*time.Second, win, settings),
		"hiding to the tray takes the form with the list: main=%v form=%v",
		win.IsVisible(), settings.IsVisible())
	a.showWindow()
	c.check(waitVisible(true, 5*time.Second, win, settings),
		"reopening brings both back: main=%v form=%v", win.IsVisible(), settings.IsVisible())

	c.shot("form-selftest-darwin")
	// No Quit: it would take the process with it before the tally is read.
}

// ---- talking to the page ----

// The two forms are fixed-size windows, so the page has to fit them exactly:
// too tall and a scrollbar appears, too short and the flex body swallows the
// spare room as a band of empty window between the last field and the buttons.
// Both are read from the DOM rather than from a screenshot, and the window's own
// size is printed beside them because the frame that Wails adds is what differs
// between an NSWindow title bar and a Win32 caption.
const formGeometry = `
	(() => {
		const body = document.querySelector(".sheet-body");
		const last = Math.max(...[...document.querySelectorAll(".sheet-body *")]
			.map((el) => el.getBoundingClientRect().bottom).filter((y) => y > 0));
		const shown = ["fSecret","fAuthority","fPath","fMode"].filter((id) =>
			getComputedStyle(document.getElementById(id)).display !== "none");
		return {
			transport: document.getElementById("fTransport") && document.getElementById("fTransport").value,
			shown, n: shown.length,
			scroll: body.scrollHeight > body.clientHeight,
			gutter: body.offsetWidth - body.clientWidth,
			gap: Math.round(document.querySelector(".sheet-actions").getBoundingClientRect().top - last),
			below: Math.round(window.innerHeight -
				document.querySelector(".sheet-actions").getBoundingClientRect().bottom),
			inner: [window.innerWidth, window.innerHeight],
			fit: window.selffit};
	})()`

const settingsGeometry = `
	(() => {
		const body = document.querySelector(".sheet-body");
		const last = Math.max(...[...document.querySelectorAll(".sheet-body *")]
			.map((el) => el.getBoundingClientRect().bottom).filter((y) => y > 0));
		// The updater belongs to the window, so it shares the button row instead of
		// costing the form a line of its own. Its message is measured with the
		// longest thing the updater can say - the case where the row could overflow
		// - and empty until this probe fills it, since a runner has never replaced
		// its bundled dataset. The text goes back out so the fit numbers around it
		// stay the ones the window was sized for.
		const updater = (() => {
			const b = document.getElementById("sUpdateRules");
			const m = document.getElementById("sRulesMsg");
			const had = m.textContent;
			m.textContent = "download was only 512 bytes, too small to be the " +
				"rules dataset - keeping the current one";
			const rr = document.querySelector(".sheet-actions").getBoundingClientRect();
			const br = b.getBoundingClientRect();
			const cr = document.getElementById("settingsCancel").getBoundingClientRect();
			const mr = m.getBoundingClientRect();
			const out = {same: b.parentElement.parentElement === document.querySelector(".sheet-actions"),
				first: br.right < cr.left,
				leftGap: Math.round(br.left - rr.left),
				rightGap: Math.round(rr.right - document.getElementById("settingsOK").getBoundingClientRect().right),
				beside: mr.width > 0 && mr.left >= br.right,
				clear: Math.round(cr.left - mr.right)};
			m.textContent = had;
			return out;
		})();
		// The line the window opens with is read too, but only as far as the row
		// it costs: a runner has never replaced its bundled dataset, so the slot
		// is empty here and the updater's own text is what gets measured.
		return {
			ports: ["sHttpPort","sSocksPort"].map(id => document.getElementById(id).value).join("/"),
			loginRow: getComputedStyle(document.getElementById("sLoginRow")).display,
			scroll: body.scrollHeight > body.clientHeight,
			gutter: body.offsetWidth - body.clientWidth,
			gap: Math.round(document.querySelector(".sheet-actions").getBoundingClientRect().top - last),
			below: Math.round(window.innerHeight -
				document.querySelector(".sheet-actions").getBoundingClientRect().bottom),
			updater,
			inner: [window.innerWidth, window.innerHeight],
			fit: window.selffit};
	})()`

func gapOf(probeJSON string) int {
	var out struct {
		Gap int `json:"gap"`
	}
	_ = json.Unmarshal([]byte(probeJSON), &out)
	return out.Gap
}

// belowOf is the space a form page reports under its own buttons: what the
// window has left over once the form has taken what it needs.
func belowOf(probeJSON string) int {
	var out struct {
		Below int `json:"below"`
	}
	_ = json.Unmarshal([]byte(probeJSON), &out)
	return out.Below
}

// heightOf is the client height the page reports for itself.
func heightOf(probeJSON string) int {
	var out struct {
		Inner [2]int `json:"inner"`
	}
	_ = json.Unmarshal([]byte(probeJSON), &out)
	return out.Inner[1]
}

// settled is a form page whose window has finished answering: the buttons are at
// the bottom edge and nothing is clipped. The fit is a loop and AppKit animates
// the resize it asks for, so one read can land mid-flight.
func settled(probeJSON string) bool {
	var out struct {
		Gutter int `json:"gutter"`
	}
	_ = json.Unmarshal([]byte(probeJSON), &out)
	return belowOf(probeJSON) >= 0 && belowOf(probeJSON) <= 12 &&
		out.Gutter == 0 && !strings.Contains(probeJSON, `"scroll":true`)
}

// fitted is settled plus the mark the fit leaves behind: the window asked for
// exactly the form and its two pixels of rounding, so the buttons sit two pixels
// off the bottom edge. A window still at the height it was created at is further
// from that, and the first read of a form has to wait for the fit rather than
// compare the editor's opening size with its own.
func fitted(probeJSON string) bool {
	return settled(probeJSON) && belowOf(probeJSON) <= 4
}

// loopDone is the fit saying it has stopped asking. A page can look right in the
// middle of the loop - the window already at the height the last ask wanted, the
// judgement that accepts it still 150 ms away - and then the printed numbers are
// an in-flight state rather than the answer.
func loopDone(probeJSON string) bool {
	return !strings.Contains(probeJSON, `"done":false`)
}

var probeID atomic.Int64

// probe evaluates an expression inside a window and brings the answer back.
// ExecJS is fire and forget, so the page posts the value over the event channel
// and only the reply carrying this probe's id is accepted.
func probe(w application.Window, probes chan map[string]any, expr string) string {
	id := probeID.Add(1)
	w.ExecJS(fmt.Sprintf(
		`Promise.resolve(%s).then((out) => import("/wails/runtime.js").then((m) => m.Events.Emit("selftest", {id: %d, out: out})));`, expr, id))
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case m := <-probes:
			if got, _ := m["id"].(float64); int64(got) != id {
				continue
			}
			return render(m["out"])
		case <-time.After(200 * time.Millisecond):
		}
	}
	return ""
}

// probeUntil re-reads an expression until it satisfies want. The async ones - a
// page that fills itself from a binding - settle some time after the click that
// caused them, and on a CI runner that is far longer than on a desktop.
func probeUntil(w application.Window, probes chan map[string]any, expr string, want func(string) bool, limit time.Duration) string {
	deadline := time.Now().Add(limit)
	var last string
	for {
		last = probe(w, probes, expr)
		if want(last) || time.Now().After(deadline) {
			return last
		}
	}
}

func render(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// ---- form windows ----

// waitForm polls until a window with the given name exists and has a native
// window behind it. The click that opens one goes through a binding, so the
// window appears some time after the probe that sent the click - and it is
// filed in the window list before AppKit gets around to creating it.
func waitForm(a *App, name string, limit time.Duration) application.Window {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if w := a.formByName(name); w != nil && w.NativeWindow() != nil {
			return w
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func waitClosed(a *App, name string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if a.formByName(name) == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// waitVisible polls until every window reports `want`, and gives up at limit.
// The tray checks need this rather than a sleep: the form disappears because
// AppKit releases it along with its owner, and on a loaded runner that
// handover has been measured taking longer than any sleep worth waiting
// through. A form that never hides still fails, five seconds later.
func waitVisible(want bool, limit time.Duration, windows ...application.Window) bool {
	deadline := time.Now().Add(limit)
	for {
		done := true
		for _, w := range windows {
			if w.IsVisible() != want {
				done = false
				break
			}
		}
		if done || time.Now().After(deadline) {
			return done
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func formName(w application.Window) string {
	if w == nil {
		return "(none)"
	}
	return w.Name()
}

func countForms(a *App, prefix string) int {
	n := 0
	for _, w := range a.wails.Window.GetAll() {
		if strings.HasPrefix(w.Name(), prefix) {
			n++
		}
	}
	return n
}

func stateFromFile(dir string) config.State {
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return config.State{}
	}
	var s config.State
	_ = json.Unmarshal(b, &s)
	return s
}

// ---- pixels ----

// shot is the macOS answer to the Windows window capture: there is no
// cross-platform "render this one window to a file" call, so the job takes the
// whole screen and a human reads it. The assertions above are all DOM reads, so
// nothing in the pass/fail depends on this.
func (c *checker) shot(label string) {
	dir := os.Getenv("BADPROXY_SELFTEST_SHOTS")
	if dir == "" {
		return
	}
	c.n++
	path := filepath.Join(dir, fmt.Sprintf("%02d-%s.png", c.n, label))
	out, err := exec.Command("screencapture", "-x", path).CombinedOutput()
	if err != nil {
		c.fail("capture %s: %v %s", label, err, strings.TrimSpace(string(out)))
		return
	}
	fmt.Printf("  shot %s -> %s\n", label, path)
}
