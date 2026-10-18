//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Drives the app the way the buttons do, checks what each page actually
// rendered, and writes a screenshot of every window. The node editor and the
// settings form are webview windows of their own, so the harness holds several
// windows at once and probes each one by itself; the delete confirmation stays
// inside the main window and is probed there.
//
//	BADPROXY_GUI_SELFTEST=1 go test -tags production -run TestFormWindows -count=1 -v
//
// An ordinary "go test ./..." run skips it: it opens a real window. Everything
// it writes goes to a temp directory seeded with a synthetic node, so the user's
// own state.json is never opened. Screenshots are left in %TEMP% to look at.
const selfTestEnv = "BADPROXY_GUI_SELFTEST"

func TestMain(m *testing.M) {
	if os.Getenv(selfTestEnv) == "" {
		os.Exit(m.Run())
	}
	os.Exit(runFormSelfTest())
}

// ---- reporting ----

type checker struct {
	dir   string
	n     int
	fails []string
}

func (c *checker) ok(format string, args ...any) {
	fmt.Printf("  ok   "+format+"\n", args...)
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

// shot captures one window into the screenshot directory.
func (c *checker) shot(w application.Window, label string) {
	if w == nil {
		c.fail("window %q is not open", label)
		return
	}
	c.n++
	path := filepath.Join(c.dir, fmt.Sprintf("%02d-%s.png", c.n, label))
	hwnd := w.NativeWindow()
	if hwnd == nil {
		c.fail("window %q has no native handle", label)
		return
	}
	if err := captureWindow(uintptr(hwnd), path); err != nil {
		c.fail("capture %s: %v", label, err)
		return
	}
	fmt.Printf("  shot %s -> %s\n", label, path)
}

// ---- the run ----

func runFormSelfTest() int {
	dir, err := os.MkdirTemp("", "bpg-form-selftest-*")
	if err != nil {
		fmt.Println("temp dir:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	shots, err := os.MkdirTemp("", "bpg-form-selftest-shots-*")
	if err != nil {
		fmt.Println("shots dir:", err)
		return 1
	}
	c := &checker{dir: shots}
	fmt.Println("screenshots:", shots)

	// High ports: a developer's own instance may already hold 10808/10809.
	st := defaultState()
	st.HttpPort, st.SocksPort = "11909", "11908"
	st.Nodes = []Node{{
		Name: "alpha", Protocol: "vless", Server: "alpha.example.invalid", Port: "443",
		Secret: "synthetic-uuid", Transport: "xhttp", Path: "/bp",
		Authority: "alpha.example.invalid", Padding: "50-200",
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

	a := &App{store: newStore(dir)}
	a.st = a.store.load()

	frontend, err := fs.Sub(bundled, "frontend")
	if err != nil {
		fmt.Println("assets:", err)
		return 1
	}
	wails := application.New(application.Options{
		Name:        windowTitle,
		Description: "Bad Proxy client",
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(frontend)},
		Services:    []application.Service{application.NewService(a)},
	})
	a.wails = wails
	a.mainWindow = wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      mainWindowName,
		Title:     windowTitle,
		Width:     mainWindowWidth,
		Height:    mainWindowHeight,
		MinWidth:  mainWindowMinW,
		MinHeight: mainWindowMinH,
		// The query is what hands the harness the two actions that a native OS
		// menu otherwise hides.
		URL: "/?selftest=1",
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
	wails.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go checkForms(a, c, probes, dir)
	})

	if err := wails.Run(); err != nil {
		fmt.Println("run:", err)
		return 1
	}
	if len(c.fails) > 0 {
		fmt.Printf("\n%d check(s) failed\n", len(c.fails))
		return 1
	}
	fmt.Println("\nall form window checks passed")
	return 0
}

// checkForms is the script. Every step goes through the same entry point the UI
// uses - a click on the real button - and each assertion reads back what the
// page rendered or what landed on disk.
func checkForms(a *App, c *checker, probes chan map[string]any, dir string) {
	time.Sleep(2500 * time.Millisecond)
	win := a.mainWindow

	// ---- the main window ----
	c.shot(win, "main")
	readouts := probe(win, probes, `
		["roHttp","roSocks","roMode","roRouting"].map(id => {
			const el = document.getElementById(id);
			return id + "=" + el.tagName + ":" + el.textContent;
		}).join(" ") + " | forms=" +
		(document.getElementById("nodeSheet") === null && document.getElementById("settingsSheet") === null)`)
	c.check(strings.Contains(readouts, "roHttp=SPAN:11909"), "the bar shows the HTTP port as text: %q", readouts)
	c.check(strings.Contains(readouts, "roSocks=SPAN:11908"), "the bar shows the SOCKS port as text")
	c.check(strings.Contains(readouts, "roMode=SPAN:Direct"), "the bar names the mode in words")
	c.check(strings.Contains(readouts, "forms=true"), "the form sheets are gone from the list window")

	// The minimum size is what the layout has to survive: the bar's three zones
	// stay reachable and the delete card still fits. The readouts are measured
	// with the longest words they can hold, because "Direct" is not what sets
	// the width.
	win.SetSize(mainWindowMinW, mainWindowMinH)
	time.Sleep(800 * time.Millisecond)
	fits := probe(win, probes, `
		(() => {
			const d = document.documentElement;
			const bar = document.querySelector(".status-bar");
			const set = (id, v) => { document.getElementById(id).textContent = v; };
			const back = ["roMode","roRouting"].map(id => document.getElementById(id).textContent);
			set("roMode","System proxy"); set("roRouting","Bypass GFW");
			const box = (id) => {
				const r = document.getElementById(id).getBoundingClientRect();
				return [Math.round(r.left), Math.round(r.right), Math.round(r.width)];
			};
			const facts = [...document.querySelectorAll(".readout")];
			const out = {
				page: [d.scrollWidth, d.clientWidth, d.scrollHeight, d.clientHeight],
				bar: [bar.clientWidth, bar.scrollWidth],
				gear: box("gearBtn"), add: box("addNodeBtn"), status: box("statusText"),
				addLabel: document.getElementById("addNodeBtn").textContent.trim(),
				facts: Math.round(facts[0].getBoundingClientRect().left),
				factsRight: Math.round(facts[facts.length - 1].getBoundingClientRect().right),
				wide: facts.map(el => Math.round(el.getBoundingClientRect().width)),
				actions: Math.round(document.querySelector(".bar-actions").getBoundingClientRect().width),
				inner: d.clientWidth};
			set("roMode", back[0]); set("roRouting", back[1]);
			return out;
		})()`)
	var layout struct {
		Page       [4]int `json:"page"`
		Bar        [2]int `json:"bar"`
		Gear       [3]int `json:"gear"`
		Add        [3]int `json:"add"`
		Status     [3]int `json:"status"`
		AddLabel   string `json:"addLabel"`
		Facts      int    `json:"facts"`
		FactsRight int    `json:"factsRight"`
		Wide       []int  `json:"wide"`
		Actions    int    `json:"actions"`
		Inner      int    `json:"inner"`
	}
	c.check(json.Unmarshal([]byte(fits), &layout) == nil, "the window reports its own layout: %s", fits)
	fmt.Printf("  bar    actions=%d readouts=%v needs=%d of %d\n",
		layout.Actions, layout.Wide, layout.Actions+sum(layout.Wide)+20, layout.Inner)
	if layout.Inner > 0 {
		c.check(layout.Page[0] <= layout.Page[1]+1, "the page itself does not overflow: %v", layout.Page)
		c.check(layout.Bar[1] <= layout.Bar[0]+1, "the status bar is not wider than the window: %v", layout.Bar)
		c.check(layout.AddLabel == "", "the add button is an icon, not a word: %q", layout.AddLabel)
		c.check(layout.Gear[0] >= 0 && layout.Gear[2] <= layout.Add[0], "the gear comes first, the plus after it: %v then %v", layout.Gear, layout.Add)
		c.check(layout.Add[1] <= layout.Status[0], "the message space starts where the actions end: %d then %d", layout.Add[1], layout.Status[0])
		c.check(layout.Status[1] <= layout.Facts, "the message space stops before the facts: %d then %d", layout.Status[1], layout.Facts)
		c.check(layout.FactsRight >= layout.Inner-10 && layout.FactsRight <= layout.Inner,
			"the facts hold the right edge: %d of %d", layout.FactsRight, layout.Inner)
		c.check(layout.Status[2] > 0, "the message space still has room at the minimum width: %d px", layout.Status[2])
	}
	c.shot(win, "main-minimum")

	// ---- the node editor ----
	// The click is the whole entry point: it goes through a binding and the
	// backend opens a window.
	probe(win, probes, `(() => { document.getElementById("addNodeBtn").click(); return "sent"; })()`)
	node := waitForm(a, nodeWindowPrefix+"new", 5*time.Second)
	if node == nil {
		c.fail("the add button never opened the editor window")
		a.quit()
		return
	}
	c.ok("the add button opened the editor in a window of its own")
	// The form is the main window's, not a stranger on the desktop: the window
	// manager is what keeps it on top, so check the relationship rather than
	// trusting that it looks right in a screenshot.
	c.check(ownerOf(node) == hwndOf(a.mainWindow),
		"the editor is owned by the main window: main=%x node=%x owner=%x",
		hwndOf(a.mainWindow), hwndOf(node), ownerOf(node))
	c.check(aboveInStack(node, win), "the editor starts out above the main window")
	// PrintWindow on a window that has only just been shown comes out black: the
	// page is loaded (the probe above answered) but the compositor has not drawn
	// a frame yet, so give it one before asking for the pixels.
	time.Sleep(1500 * time.Millisecond)
	fields := probe(node, probes, `
		(() => {
			const shown = (id) => getComputedStyle(document.getElementById(id)).display !== "none";
			const body = document.querySelector(".sheet-body");
			const last = Math.max(...[...document.querySelectorAll(".sheet-body *")]
				.map((el) => el.getBoundingClientRect().bottom).filter((y) => y > 0));
			return {shown: ["fSecret","fAuthority","fPath","fPadding"].filter(shown),
					scroll: body.scrollHeight > body.clientHeight,
					gap: Math.round(document.querySelector(".sheet-actions").getBoundingClientRect().top - last),
					inner: [window.innerWidth, window.innerHeight]};
		})()`)
	c.check(strings.Contains(fields, `"fSecret","fAuthority","fPath","fPadding"`),
		"xhttp offers every optional field: %s", fields)
	c.check(strings.Contains(fields, `"scroll":false`),
		"the tallest form needs no scrollbar: %s", fields)
	// The window is the form: the buttons sit just under the last field, so a
	// fixed-size dialog never shows a band of empty window in its middle.
	c.check(gapOf(fields) <= 32, "no dead band above the editor's buttons: %s", fields)
	fmt.Printf("  form   node editor: %s\n", fields)
	c.shot(node, "node-add")

	// A protocol that hides a row has to take the buttons with it: the form
	// flows from the top, so the spare room belongs under the actions, not in
	// the middle of the fields.
	trimmed := probe(node, probes, `
		(() => {
			const sel = document.getElementById("fProtocol");
			sel.value = "socks";
			sel.dispatchEvent(new Event("change"));
			const hidden = (id) => getComputedStyle(document.getElementById(id)).display === "none";
			const actions = document.querySelector(".sheet-actions").getBoundingClientRect();
			const body = document.querySelector(".sheet-body");
			const out = {hidden: ["fSecret","fAuthority","fPath","fPadding"].filter(hidden),
					fits: actions.bottom <= window.innerHeight + 1,
					scroll: body.scrollHeight > body.clientHeight};
			sel.value = "vless";
			sel.dispatchEvent(new Event("change"));
			return out;
		})()`)
	c.check(strings.Contains(trimmed, `"hidden":["fSecret"]`), "socks hides only the secret: %s", trimmed)
	c.check(strings.Contains(trimmed, `"fits":true`) && strings.Contains(trimmed, `"scroll":false`),
		"a shorter form keeps its buttons inside the window: %s", trimmed)

	// The validation is synchronous, so the click and the read are one pass.
	refusal := probe(node, probes, `
		(() => {
			document.getElementById("modalSave").click();
			return document.getElementById("fMsg").textContent;
		})()`)
	c.check(strings.Contains(refusal, "required"), "an empty form refuses in its own line: %q", refusal)
	c.check(a.formByName(nodeWindowPrefix+"new") != nil, "a refused save leaves the editor open")

	// Cancel is the way out that writes nothing - the check the sheet could only
	// make about a hidden div.
	before := stateFromFile(dir)
	probe(node, probes, `(() => { document.getElementById("modalCancel").click(); return "sent"; })()`)
	c.check(waitClosed(a, nodeWindowPrefix+"new", 5*time.Second), "Cancel closes the editor")
	c.check(len(stateFromFile(dir).Nodes) == len(before.Nodes), "a cancelled editor wrote nothing")

	probe(win, probes, `(() => { document.getElementById("addNodeBtn").click(); return "sent"; })()`)
	node = waitForm(a, nodeWindowPrefix+"new", 5*time.Second)
	if node == nil {
		c.fail("the add button never reopened the editor")
		a.quit()
		return
	}
	// A window exists the moment the backend creates it and has a page some
	// seconds later, so a script the page cannot answer with is sent again until
	// it can. The page is only ready once its own fill has run, which for a new
	// node is the transport select settling on xhttp.
	ready := probeUntil(node, probes, `document.getElementById("fTransport") &&
		document.getElementById("fTransport").value`,
		func(s string) bool { return s == "xhttp" }, 20*time.Second)
	c.check(ready == "xhttp", "the reopened editor finished its own fill: %q", ready)
	filled := probeUntil(node, probes, `
		(() => {
			const set = (id, v) => { document.getElementById(id).value = v; };
			set("fName","gamma"); set("fServer","gamma.example.invalid"); set("fPort","2053");
			set("fSecret","synthetic-2"); set("fAuthority","gamma.example.invalid");
			return "done";
		})()`, func(s string) bool { return s == "done" }, 10*time.Second)
	c.check(filled == "done", "the reopened editor took the draft: %q", filled)
	probe(node, probes, `(() => { document.getElementById("modalSave").click(); return "sent"; })()`)
	c.check(waitClosed(a, nodeWindowPrefix+"new", 8*time.Second), "a good save closes the editor")
	c.check(len(stateFromFile(dir).Nodes) == 2, "the new node reached state.json: %d nodes", len(stateFromFile(dir).Nodes))
	rows := probeUntil(win, probes, `document.querySelectorAll(".node-item").length`,
		func(s string) bool { return s == "2" }, 8*time.Second)
	c.check(rows == "2", "the closed editor told the list to reread: %q rows", rows)

	// ---- a failed test ----
	// gamma.example.invalid does not resolve, so this is a real failure with a
	// real reason attached. The row is not a place to read a dial error: the
	// corner says only that it failed and the reason goes on the chip.
	probe(win, probes, `(() => { window.selftest.testNode(1); return "sent"; })()`)
	chip := probeUntil(win, probes, `
		(() => {
			const el = document.querySelector(".node-item:nth-child(2) .node-result");
			return el ? {text: el.textContent, reason: el.getAttribute("title") || ""} : null;
		})()`, func(s string) bool { return strings.Contains(s, "Failed") }, 90*time.Second)
	var shown struct {
		Text   string `json:"text"`
		Reason string `json:"reason"`
	}
	c.check(json.Unmarshal([]byte(chip), &shown) == nil, "the row reports its own chip: %s", chip)
	c.check(shown.Text == "Failed", "a failed test shows one word in the row: %q", shown.Text)
	c.check(strings.HasPrefix(shown.Reason, "Failed:") && len(shown.Reason) > len("Failed:"),
		"the reason is kept, on the chip: %q", shown.Reason)
	c.shot(win, "node-failed")

	// ---- the settings window ----
	probe(win, probes, `(() => { document.getElementById("gearBtn").click(); return "sent"; })()`)
	settings := waitForm(a, settingsWindowName, 5*time.Second)
	if settings == nil {
		c.fail("the gear never opened the settings window")
		a.quit()
		return
	}
	// The page fills itself from two bindings, so the read has to wait for the
	// draft to land rather than read the empty form the window starts with.
	prefill := probeUntil(settings, probes, `
		(() => ({
			ports: ["sHttpPort","sSocksPort"].map(id => document.getElementById(id).value).join("/"),
			mode: document.getElementById("sMode").value,
			loginRow: getComputedStyle(document.getElementById("sLoginRow")).display,
			scroll: document.querySelector(".sheet-body").scrollHeight >
			        document.querySelector(".sheet-body").clientHeight,
			gap: (() => {
				const last = Math.max(...[...document.querySelectorAll(".sheet-body *")]
					.map((el) => el.getBoundingClientRect().bottom).filter((y) => y > 0));
				return Math.round(document.querySelector(".sheet-actions").getBoundingClientRect().top - last);
			})(),
			inner: [window.innerWidth, window.innerHeight],
		}))()`, func(s string) bool { return strings.Contains(s, `"ports":"11909/11908"`) }, 10*time.Second)
	c.check(strings.Contains(prefill, `"mode":"direct"`), "settings opens with the saved mode")
	c.check(strings.Contains(prefill, `"loginRow":"flex"`), "the start-at-login row is offered on Windows")
	c.check(strings.Contains(prefill, `"scroll":false`), "the settings form needs no scrollbar: %s", prefill)
	c.check(gapOf(prefill) <= 32, "no dead band above the settings buttons: %s", prefill)
	fmt.Printf("  form   settings: %s\n", prefill)
	time.Sleep(1200 * time.Millisecond)
	c.shot(settings, "settings")

	// Equal ports: the refusal belongs on this window's message line, and the
	// window has to stay so the draft can be fixed.
	probe(settings, probes, `
		(() => {
			document.getElementById("sSocksPort").value = "11909";
			document.getElementById("settingsOK").click();
			return "sent";
		})()`)
	errText := probeUntil(settings, probes, `document.getElementById("sMsg").textContent`,
		func(s string) bool { return strings.Contains(s, "differ") }, 10*time.Second)
	c.check(strings.Contains(errText, "differ"), "a bad draft is reported inside the settings window: %q", errText)
	c.check(a.formByName(settingsWindowName) != nil, "a refused draft leaves the settings window open")
	c.check(stateFromFile(dir).SocksPort == "11908", "a refused draft did not reach state.json")

	// A good draft closes the window and reaches both the file and the bar.
	probe(settings, probes, `
		(() => {
			document.getElementById("sHttpPort").value = "11911";
			document.getElementById("sSocksPort").value = "11910";
			document.getElementById("settingsOK").click();
			return "sent";
		})()`)
	c.check(waitClosed(a, settingsWindowName, 15*time.Second), "a good draft closes the settings window")
	applied := stateFromFile(dir)
	c.check(applied.HttpPort == "11911" && applied.SocksPort == "11910",
		"OK applied the draft: %s/%s", applied.HttpPort, applied.SocksPort)
	after := probeUntil(win, probes, `document.getElementById("roHttp").textContent`,
		func(s string) bool { return s == "11911" }, 8*time.Second)
	c.check(after == "11911", "the bar followed the applied setting: %q", after)

	// Two clicks on the same action leave one window, not two writers of the
	// same draft stacked on top of each other.
	probe(win, probes, `(() => { document.getElementById("gearBtn").click(); return "sent"; })()`)
	if waitForm(a, settingsWindowName, 5*time.Second) == nil {
		c.fail("the gear never reopened the settings window")
	}
	probe(win, probes, `(() => { document.getElementById("gearBtn").click(); return "sent"; })()`)
	time.Sleep(1200 * time.Millisecond)
	c.check(countForms(a, settingsWindowName) == 1,
		"the gear pressed twice leaves one settings window: %d", countForms(a, settingsWindowName))

	// A *different* form is refused too. One window holds the state at a time,
	// so the plus must not smuggle a second writer in beside the settings one.
	form := a.anyFormWindow()
	if form == nil {
		c.fail("no form window is open to refuse a request")
	} else {
		beforeX, beforeY := form.Position()
		probe(win, probes, `(() => { document.getElementById("addNodeBtn").click(); return "sent"; })()`)
		// Catch the blink while it is running: the shot is for the human, since
		// FlashWindowEx is drawn by the window manager and has no state to read.
		time.Sleep(120 * time.Millisecond)
		c.shot(form, "form-blink")
		c.check(countForms(a, nodeWindowPrefix) == 0,
			"the plus does not open an editor while the settings window is open: %d", countForms(a, nodeWindowPrefix))
		c.check(countForms(a, settingsWindowName) == 1,
			"the refused request leaves the settings window alone: %d", countForms(a, settingsWindowName))
		c.check(formName(a.anyFormWindow()) == settingsWindowName,
			"the one form still open is the settings window: %q", formName(a.anyFormWindow()))

		// The answer is a blink, not a shove: a window that ends up anywhere
		// other than where it was means something is animating it by hand.
		time.Sleep(600 * time.Millisecond)
		afterX, afterY := form.Position()
		c.check(afterX == beforeX && afterY == beforeY,
			"the refusal does not move the window: %d,%d -> %d,%d", beforeX, beforeY, afterX, afterY)

		// The other half of the classic relationship: pushing the list to the
		// top of the z-order must not bury the form that is waiting for an
		// answer. SetWindowPos rather than Focus because the foreground lock can
		// veto SetForegroundWindow silently, and a veto would make this a test
		// of nothing.
		raiseToTop(win)
		time.Sleep(400 * time.Millisecond)
		c.check(aboveInStack(form, win),
			"the settings window is still above the main window after the main window was raised")
		c.shot(form, "form-above")

		// Ownership ties stacking but not visibility: an owned window stays on
		// screen when its owner is hidden, and this app hides the list to the
		// tray. A form left behind would float there with no taskbar button of
		// its own, so the app takes it along. Drive the app's own entry points
		// rather than Show/Hide on the windows.
		a.hideToTray()
		time.Sleep(400 * time.Millisecond)
		c.check(!win.IsVisible() && !form.IsVisible(),
			"hiding to the tray takes the form with the list: main=%v form=%v",
			win.IsVisible(), form.IsVisible())
		a.showWindow()
		time.Sleep(400 * time.Millisecond)
		c.check(win.IsVisible() && form.IsVisible() && aboveInStack(form, win),
			"reopening brings both back, the form on top: main=%v form=%v",
			win.IsVisible(), form.IsVisible())

		// With the settings window closed, the plus gets its editor again.
		form.Close()
		if !waitClosed(a, settingsWindowName, 5*time.Second) {
			c.fail("the settings window never closed")
		}
	}
	probe(win, probes, `(() => { document.getElementById("addNodeBtn").click(); return "sent"; })()`)
	if waitForm(a, nodeWindowPrefix+"new", 5*time.Second) == nil {
		c.fail("the plus never opened the editor once no form was open")
	}
	probe(win, probes, `(() => { document.getElementById("addNodeBtn").click(); return "sent"; })()`)
	time.Sleep(600 * time.Millisecond)
	c.check(countForms(a, nodeWindowPrefix) == 1,
		"the plus pressed twice leaves one editor: %d", countForms(a, nodeWindowPrefix))
	probe(win, probes, `(() => { document.getElementById("addNodeBtn").click(); return "sent"; })()`)
	time.Sleep(600 * time.Millisecond)
	c.check(countForms(a, nodeWindowPrefix) == 1,
		"and it is still one after a third click: %d", countForms(a, nodeWindowPrefix))
	// Close what is left so the list is the only window again, which is what
	// the remaining steps assume.
	if w := a.formByName(nodeWindowPrefix + "new"); w != nil {
		w.Close()
	}
	time.Sleep(800 * time.Millisecond)

	// ---- clicking the list ----
	// A row is the only thing on the main page that writes, so both of its
	// gestures are worth the pass: a click selects, a double click opens the
	// editor for that one node.
	probe(win, probes, `
		(() => { document.querySelectorAll(".node-item")[1].click(); return "sent"; })()`)
	active := probeUntil(win, probes,
		`[...document.querySelectorAll(".node-item")].findIndex(el => el.classList.contains("active"))`,
		func(s string) bool { return s == "1" }, 8*time.Second)
	c.check(active == "1", "a click moves the highlight: row %s active", active)
	c.check(stateFromFile(dir).Active == 1, "the click reached state.json: active=%d", stateFromFile(dir).Active)

	win.SetSize(mainWindowWidth, mainWindowHeight)
	time.Sleep(600 * time.Millisecond)
	probe(win, probes, `
		(() => { document.querySelectorAll(".node-item")[0].dispatchEvent(new MouseEvent("dblclick", {bubbles: true})); return "sent"; })()`)
	edit := waitForm(a, nodeWindowPrefix+"0", 8*time.Second)
	if edit == nil {
		c.fail("a double click never opened the editor for that node")
	} else {
		saved := probeUntil(edit, probes, `
			(() => ({name: document.getElementById("fName").value,
					server: document.getElementById("fServer").value}))()`,
			func(s string) bool { return strings.Contains(s, "alpha") }, 8*time.Second)
		c.check(strings.Contains(saved, `"name":"alpha"`) &&
			strings.Contains(saved, `"server":"alpha.example.invalid"`),
			"a double click opens the editor prefilled with that node: %s", saved)
		time.Sleep(1200 * time.Millisecond)
		c.shot(edit, "node-edit")
		// Typing into it and walking away is the case a fixed window has to
		// survive: the cross closes the window, not the app.
		probe(edit, probes, `
			(() => { document.getElementById("fName").value = "typo";
				document.getElementById("modalCancel").click(); return "sent"; })()`)
		c.check(waitClosed(a, nodeWindowPrefix+"0", 5*time.Second), "Cancel closes the editor for that node")
		c.check(stateFromFile(dir).Nodes[0].Name == "alpha", "a cancelled edit renamed nothing")
	}

	// ---- deleting a node ----
	// The confirmation is a card in this window because the OS one brought a
	// title bar and a frame around a single sentence. Two lines is the whole
	// design: the question, then the answer.
	probe(win, probes, `(() => { window.selftest.askDelete(1); return "sent"; })()`)
	asked := probeUntil(win, probes, `
		(() => {
			const p = document.getElementById("deleteQuestion");
			const card = document.querySelector("#deleteSheet .sheet").getBoundingClientRect();
			return {open: document.getElementById("deleteSheet").hidden === false,
					q: p.textContent,
					lines: Math.round(p.offsetHeight / parseFloat(getComputedStyle(p).lineHeight)),
					qw: Math.round(p.getBoundingClientRect().width),
					aw: Math.round(document.querySelector("#deleteSheet .sheet-actions").getBoundingClientRect().width),
					card: [Math.round(card.width), Math.round(card.height)]};
		})()`, func(s string) bool { return strings.Contains(s, `"open":true`) }, 5*time.Second)
	var ask struct {
		Open  bool   `json:"open"`
		Q     string `json:"q"`
		Lines int    `json:"lines"`
		Text  int    `json:"qw"`
		Btns  int    `json:"aw"`
		Card  [2]int `json:"card"`
	}
	c.check(json.Unmarshal([]byte(asked), &ask) == nil, "the card reports itself: %s", asked)
	c.check(ask.Open, "the delete card is up in this window: %s", asked)
	c.check(ask.Q == `Delete node "gamma"?`, "the card names what is about to go: %q", ask.Q)
	c.check(ask.Lines == 1, "the question is a single line: %s", asked)
	widest := ask.Text
	if ask.Btns > widest {
		widest = ask.Btns
	}
	// Two lines, and no third thing between them: the card is only as wide as
	// the wider of the sentence and its buttons, plus the padding.
	c.check(ask.Card[0]-widest <= 34 && ask.Card[0] >= widest,
		"the card hugs its content: %d wide against %d of text and %d of buttons", ask.Card[0], ask.Text, ask.Btns)
	c.check(ask.Card[1] < 130, "the card is short: %d px tall", ask.Card[1])
	// Same reason as the form windows: the card is in the DOM (the probe read
	// it) but the compositor has not necessarily drawn it yet.
	time.Sleep(600 * time.Millisecond)
	c.shot(win, "delete-ask")

	probe(win, probes, `(() => { document.getElementById("deleteCancel").click(); return "sent"; })()`)
	c.check(probeUntil(win, probes, `document.getElementById("deleteSheet").hidden`,
		func(s string) bool { return s == "true" }, 5*time.Second) == "true", "Cancel closes the card")
	c.check(len(stateFromFile(dir).Nodes) == 2, "Cancel deleted nothing")

	// Delete is the same two clicks with the other answer.
	probe(win, probes, `(() => { window.selftest.askDelete(1); return "sent"; })()`)
	if probeUntil(win, probes, `document.getElementById("deleteSheet").hidden`,
		func(s string) bool { return s == "false" }, 5*time.Second) != "false" {
		c.fail("the card never reopened for the delete itself")
	} else {
		probe(win, probes, `(() => { document.getElementById("deleteOK").click(); return "sent"; })()`)
		deadline := time.Now().Add(10 * time.Second)
		for len(stateFromFile(dir).Nodes) != 1 && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
		}
		c.check(len(stateFromFile(dir).Nodes) == 1, "Delete took that node out of state.json: %d left",
			len(stateFromFile(dir).Nodes))
		c.check(probeUntil(win, probes, `document.querySelectorAll(".node-item").length`,
			func(s string) bool { return s == "1" }, 8*time.Second) == "1", "the list dropped the row")
	}

	c.shot(win, "main-final")
	a.quit()
}

// ---- talking to the page ----

var probeID atomic.Int64

// probe evaluates an expression inside a window and brings the answer back.
// ExecJS is fire and forget, so the page posts the value over the event channel
// and only the reply carrying this probe's id is accepted.
func probe(w application.Window, probes chan map[string]any, expr string) string {
	id := probeID.Add(1)
	w.ExecJS(fmt.Sprintf(
		`Promise.resolve(%s).then((out) => import("/wails/runtime.js").then((m) => m.Events.Emit("selftest", {id: %d, out: out})));`, expr, id))
	deadline := time.Now().Add(8 * time.Second)
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

// probeUntil re-reads an expression until it satisfies want. The async ones -
// a save, an apply, a reload signalled across windows - settle some time after
// the click that caused them.
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

// render turns whatever the page sent into text an assertion can read.
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

// gapOf is the space a form page reports between its last visible field and its
// buttons. It is the one number that says the window is the form.
func gapOf(probeJSON string) int {
	var out struct {
		Gap int `json:"gap"`
	}
	_ = json.Unmarshal([]byte(probeJSON), &out)
	return out.Gap
}

// waitForm polls until a window with the given name exists and has a native
// handle. The click that opens one goes through a binding, so the window appears
// some time after the probe that sent the click - and it is filed in the window
// list a moment before the UI thread gets around to creating the HWND, which is
// exactly when a stacking assertion would read 0 and call it a bug.
func waitForm(a *App, name string, limit time.Duration) application.Window {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if w := a.formByName(name); w != nil && hwndOf(w) != 0 {
			return w
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// waitClosed polls the other way: a form window finishes its work by closing,
// and the save or apply that let it do so is the slow part.
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

// formName keeps a closed window out of a failure message.
func formName(w application.Window) string {
	if w == nil {
		return "(none)"
	}
	return w.Name()
}

// countForms is how many open windows carry a name from one form's namespace.
// Two of them would be two drafts writing the same state behind each other.
func countForms(a *App, prefix string) int {
	n := 0
	for _, w := range a.wails.Window.GetAll() {
		if strings.HasPrefix(w.Name(), prefix) {
			n++
		}
	}
	return n
}

func stateFromFile(dir string) GUIState {
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return GUIState{}
	}
	var s GUIState
	_ = json.Unmarshal(b, &s)
	return s
}

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

// ---- stacking ----

var stGetWindowProc = stUser32.NewProc("GetWindow")

// hwndOf, ownerOf and raiseToTop live in owner_windows.go.
//
// gwHwndNext walks DOWN the z-order: the list is headed by the topmost window,
// so the window beneath a given one is its "next".
const gwHwndNext = 2

// aboveInStack reports whether upper sits above lower in the real window order
// by walking the "window beneath me" chain from upper until it reaches lower.
// That chain crosses other processes' windows, so what matters is whether lower
// turns up at all, not how far away it is.
func aboveInStack(upper, lower application.Window) bool {
	u, l := hwndOf(upper), hwndOf(lower)
	if u == 0 || l == 0 {
		return false
	}
	h := u
	for i := 0; i < 4096; i++ {
		r, _, _ := stGetWindowProc.Call(h, gwHwndNext)
		if r == 0 {
			return false
		}
		if r == l {
			return true
		}
		h = r
	}
	return false
}

// ---- pixels ----

var (
	stUser32              = syscall.NewLazyDLL("user32.dll")
	stGdi32               = syscall.NewLazyDLL("gdi32.dll")
	stPrintWindow         = stUser32.NewProc("PrintWindow")
	stGetWindowRect       = stUser32.NewProc("GetWindowRect")
	stGetDC               = stUser32.NewProc("GetDC")
	stReleaseDC           = stUser32.NewProc("ReleaseDC")
	stCreateCompatibleDC  = stGdi32.NewProc("CreateCompatibleDC")
	stCreateCompatibleBmp = stGdi32.NewProc("CreateCompatibleBitmap")
	stSelectObject        = stGdi32.NewProc("SelectObject")
	stGetDIBits           = stGdi32.NewProc("GetDIBits")
	stDeleteDC            = stGdi32.NewProc("DeleteDC")
	stDeleteObject        = stGdi32.NewProc("DeleteObject")
)

// pwRenderFullContent is what makes a WebView2 window print its web content;
// without it only the native frame comes out.
const pwRenderFullContent = 2

type stRect struct {
	left, top, right, bottom int32
}

type stBitmapInfoHeader struct {
	size                   uint32
	width, height          int32
	planes, bitCount       uint16
	compression, sizeImage uint32
	xPels, yPels           int32
	clrUsed, clrImportant  uint32
}

type stBitmapInfo struct {
	header  stBitmapInfoHeader
	bitmask [4]uint32
}

// captureWindow writes one window's pixels to a PNG. A process in the
// background cannot take the foreground, so a desktop BitBlt would miss it;
// PrintWindow asks the window to draw itself instead.
func captureWindow(hwnd uintptr, path string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var r stRect
	if ok, _, _ := stGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return fmt.Errorf("GetWindowRect failed")
	}
	width, height := int(r.right-r.left), int(r.bottom-r.top)
	if width <= 0 || height <= 0 {
		return fmt.Errorf("window is %dx%d", width, height)
	}
	screen, _, _ := stGetDC.Call(0)
	memDC, _, _ := stCreateCompatibleDC.Call(screen)
	bmp, _, _ := stCreateCompatibleBmp.Call(screen, uintptr(width), uintptr(height))
	stSelectObject.Call(memDC, bmp)
	if ok, _, _ := stPrintWindow.Call(hwnd, memDC, pwRenderFullContent); ok == 0 {
		return fmt.Errorf("PrintWindow failed")
	}
	var info stBitmapInfo
	info.header.size = uint32(unsafe.Sizeof(info.header))
	info.header.width = int32(width)
	info.header.height = -int32(height)
	info.header.planes = 1
	info.header.bitCount = 32
	buf := make([]byte, width*height*4)
	if lines, _, _ := stGetDIBits.Call(memDC, bmp, 0, uintptr(height),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&info)), 0); lines == 0 {
		return fmt.Errorf("GetDIBits returned no lines")
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		img.Pix[i*4+0] = buf[i*4+2]
		img.Pix[i*4+1] = buf[i*4+1]
		img.Pix[i*4+2] = buf[i*4+0]
		img.Pix[i*4+3] = 255 // PrintWindow leaves the alpha byte empty
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	stDeleteDC.Call(memDC)
	stDeleteObject.Call(bmp)
	stReleaseDC.Call(0, screen)
	return nil
}
