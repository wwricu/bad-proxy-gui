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
	// The same environment variable the macOS job answers to, so a run can be
	// pointed at a directory that survives the process and read afterwards.
	shots := os.Getenv("BADPROXY_SELFTEST_SHOTS")
	if shots == "" {
		var err error
		shots, err = os.MkdirTemp("", "bpg-form-selftest-shots-*")
		if err != nil {
			fmt.Println("shots dir:", err)
			return 1
		}
	}
	if err := os.MkdirAll(shots, 0o755); err != nil {
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
	w, h, minW, minH := mainWindowSize(len(st.Nodes))
	a.mainWindow = wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      mainWindowName,
		Title:     windowTitle,
		Width:     w,
		Height:    h,
		MinWidth:  minW,
		MinHeight: minH,
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
	_, _, minW, minH := mainWindowSize(len(a.st.Nodes))
	readouts := probeUntil(win, probes, `
		["roHttp","roSocks","roMode","roRouting"].map(id => {
			const el = document.getElementById(id);
			return id + "=" + el.tagName + ":" + el.textContent;
		}).join(" ") + " | forms=" +
		(document.getElementById("nodeSheet") === null && document.getElementById("settingsSheet") === null)`,
		// The bar is filled by the first state read, which lands whenever the
		// page's script has run and the binding has answered. On this machine
		// that is inside the sleep above; on a CI runner the cold start is
		// slower and the same read once came back with four empty spans. Polling
		// for the settled bar is the difference between a test and a race.
		func(s string) bool {
			return strings.Contains(s, "roHttp=SPAN:11909") &&
				strings.Contains(s, "roSocks=SPAN:11908") &&
				strings.Contains(s, "roMode=SPAN:Direct") &&
				strings.Contains(s, "forms=true")
		}, 20*time.Second)
	c.check(strings.Contains(readouts, "roHttp=SPAN:11909"), "the bar shows the HTTP port as text: %q", readouts)
	c.check(strings.Contains(readouts, "roSocks=SPAN:11908"), "the bar shows the SOCKS port as text")
	c.check(strings.Contains(readouts, "roMode=SPAN:Direct"), "the bar names the mode in words")
	c.check(strings.Contains(readouts, "forms=true"), "the form sheets are gone from the list window")

	// The minimum size is what the layout has to survive: the bar's three zones
	// stay reachable and the delete card still fits. The readouts are measured
	// with the longest words they can hold - "System" and "Bypass GFW" - because
	// the mode the app happens to start in is not what sets the width.
	win.SetSize(minW, minH)
	time.Sleep(800 * time.Millisecond)
	fits := probe(win, probes, `
		(() => {
			const d = document.documentElement;
			const bar = document.querySelector(".status-bar");
			const set = (id, v) => { document.getElementById(id).textContent = v; };
			const back = ["roMode","roRouting"].map(id => document.getElementById(id).textContent);
			set("roMode", "System"); set("roRouting", "Bypass GFW");
			const box = (id) => {
				const r = document.getElementById(id).getBoundingClientRect();
				return [Math.round(r.left), Math.round(r.right), Math.round(r.width)];
			};
			const facts = [...document.querySelectorAll(".readout")];
			// Both paddings are read from the page, not assumed: the last time the
			// bar's arithmetic was written down by hand it went stale and the
			// floor was 2 px narrower than the bar for a whole release.
			const pad = (id) => {
				const s = getComputedStyle(document.getElementById(id));
				return Math.round(parseFloat(s.paddingLeft) + parseFloat(s.paddingRight));
			};
			const out = {
				page: [d.scrollWidth, d.clientWidth, d.scrollHeight, d.clientHeight],
				bar: [bar.clientWidth, bar.scrollWidth],
				gear: box("gearBtn"), add: box("addNodeBtn"), status: box("statusText"),
				addLabel: document.getElementById("addNodeBtn").textContent.trim(),
				facts: Math.round(facts[0].getBoundingClientRect().left),
				factsRight: Math.round(facts[facts.length - 1].getBoundingClientRect().right),
				wide: facts.map(el => Math.round(el.getBoundingClientRect().width)),
				actions: Math.round(document.querySelector(".bar-actions").getBoundingClientRect().width),
				barPad: pad("statusBar"), statusPad: pad("statusText"),
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
		BarPad     int    `json:"barPad"`
		StatusPad  int    `json:"statusPad"`
		Inner      int    `json:"inner"`
	}
	c.check(json.Unmarshal([]byte(fits), &layout) == nil, "the window reports its own layout: %s", fits)
	// What the bar would ask for if every element got its own smallest legal box.
	// The floor is set from this number, so the two have to be compared.
	needs := layout.Actions + sum(layout.Wide) + layout.BarPad + layout.StatusPad
	fmt.Printf("  bar    actions=%d readouts=%v pads=%d+%d needs=%d of %d\n",
		layout.Actions, layout.Wide, layout.BarPad, layout.StatusPad, needs, layout.Inner)
	// The other number the floor is made of. It is not the same on a runner at
	// 100% as on a display at 175%, and the difference is what first failed here.
	fmt.Printf("  main   floor: window=%dx%d client=%dx%d chrome=%d\n",
		minW, minH, layout.Inner, layout.Page[3], minH-layout.Page[3])
	if layout.Inner > 0 {
		c.check(needs <= layout.Inner,
			"the minimum width is wide enough for the whole bar: needs %d, has %d", needs, layout.Inner)
		c.check(layout.Page[0] <= layout.Page[1]+1, "the page itself does not overflow: %v", layout.Page)
		c.check(layout.Bar[1] <= layout.Bar[0]+1, "the status bar is not wider than the window: %v", layout.Bar)
		c.check(layout.AddLabel == "", "the add button is an icon, not a word: %q", layout.AddLabel)
		c.check(layout.Gear[0] >= 0 && layout.Gear[2] <= layout.Add[0], "the gear comes first, the plus after it: %v then %v", layout.Gear, layout.Add)
		c.check(layout.Add[1] <= layout.Status[0], "the message space starts where the actions end: %d then %d", layout.Add[1], layout.Status[0])
		c.check(layout.Status[1] <= layout.Facts, "the message space stops before the facts: %d then %d", layout.Status[1], layout.Facts)
		c.check(layout.FactsRight >= layout.Inner-10 && layout.FactsRight <= layout.Inner,
			"the facts hold the right edge: %d of %d", layout.FactsRight, layout.Inner)
		c.check(layout.Status[2] > 0, "the message space still has room at the minimum width: %d px", layout.Status[2])
		// The floor promises rows you can see, and a window height is not a
		// client height: the caption and the border are charged first, which is
		// why this reads the page back instead of trusting the constant.
		listH := layout.Page[3] - mainWindowBarH
		c.check(listH >= mainWindowListH+mainWindowMinRows*mainWindowRowH,
			"the minimum height shows %d whole node rows: the list has %d px of %d",
			mainWindowMinRows, listH, layout.Page[3])
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
	c.check(waitOwner(node, hwndOf(a.mainWindow), 5*time.Second) == hwndOf(a.mainWindow),
		"the editor is owned by the main window: main=%x node=%x owner=%x",
		hwndOf(a.mainWindow), hwndOf(node), ownerOf(node))
	c.check(aboveInStack(node, win), "the editor starts out above the main window")
	// PrintWindow on a window that has only just been shown comes out black: the
	// page is loaded (the probe above answered) but the compositor has not drawn
	// a frame yet, so give it one before asking for the pixels.
	time.Sleep(1500 * time.Millisecond)
	// One shape for every read of the editor: which optional rows are on screen,
	// whether the page still fits, how much room a scrollbar has taken, and how
	// much window is left above the buttons and under them. The window is meant
	// to be the form, so those margins and that room are the whole test.
	const formShape = `
		(() => {
			const shown = (id) => getComputedStyle(document.getElementById(id)).display !== "none";
			const body = document.querySelector(".sheet-body");
			const actions = document.querySelector(".sheet-actions").getBoundingClientRect();
			const last = Math.max(...[...document.querySelectorAll(".sheet-body *")]
				.map((el) => el.getBoundingClientRect().bottom).filter((y) => y > 0));
			const rows = ["fSecret","fAuthority","fPath","fMode"].filter(shown);
			return {rows, n: rows.length,
					scroll: body.scrollHeight > body.clientHeight,
					gutter: body.offsetWidth - body.clientWidth,
					gap: Math.round(actions.top - last),
					below: Math.round(window.innerHeight - actions.bottom),
					inner: [window.innerWidth, window.innerHeight],
					fit: window.selffit};
		})()`
	setTransport := func(v string) string {
		return `(() => { const s = document.getElementById("fTransport"); s.value = "` + v +
			`"; s.dispatchEvent(new Event("change")); return "sent"; })()`
	}
	// The fit is a round trip through a binding, and the window may have to be
	// asked twice before it lands on the height the page wants, so read until it
	// has caught up with the page rather than trusting the first answer.
	fields := probeUntil(node, probes, formShape, func(s string) bool {
		return strings.Contains(s, `"n":4`) && fitted(s) && loopDone(s)
	}, 8*time.Second)
	c.check(strings.Contains(fields, `"rows":["fSecret","fAuthority","fPath","fMode"]`),
		"xhttp offers every optional field: %s", fields)
	c.check(strings.Contains(fields, `"scroll":false`),
		"the tallest form needs no scrollbar: %s", fields)
	// The pixel-level version of the same promise. A box one fraction too short
	// rounds scrollHeight and clientHeight to the same number, so the check above
	// passes while the browser has already taken a scrollbar; the space the bar
	// reserves is the one thing that cannot hide.
	c.check(gutterOf(fields) == 0,
		"the fitted editor keeps no scrollbar gutter: %s", fields)
	// The window is the form: the buttons sit just under the last field, so a
	// fixed-size dialog never shows a band of empty window in its middle.
	c.check(gapOf(fields) <= 32, "no dead band above the editor's buttons: %s", fields)
	// The other half of the same rule: a window taller than its form parks the
	// spare room under the buttons, which is the band of empty window the fixed
	// size exists to avoid. The form asks for the height it needs, so anything
	// more than a pixel of rounding here is a window that did not listen.
	c.check(belowOf(fields) >= 0 && belowOf(fields) <= 12,
		"the editor's buttons sit at the bottom edge: %s", fields)
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
			const out = {hidden: ["fSecret","fAuthority","fPath","fMode"].filter(hidden),
					fits: actions.bottom <= window.innerHeight + 1,
					scroll: body.scrollHeight > body.clientHeight};
			sel.value = "vless";
			sel.dispatchEvent(new Event("change"));
			return out;
		})()`)
	c.check(strings.Contains(trimmed, `"hidden":["fSecret"]`), "socks hides only the secret: %s", trimmed)
	c.check(strings.Contains(trimmed, `"fits":true`) && strings.Contains(trimmed, `"scroll":false`),
		"a shorter form keeps its buttons inside the window: %s", trimmed)

	// ---- the form that changes while it is open ----
	// tls drops three rows at once. The window was sized for xhttp, so unless it
	// follows the fields the buttons slide up off the bottom edge and leave a
	// band of bare window under them - the failure a fixed form height cannot
	// see coming, because the form is only tallest when it first opens.
	probe(node, probes, setTransport("tls"))
	shrunk := probeUntil(node, probes, formShape, func(s string) bool {
		return strings.Contains(s, `"n":2`) && settled(s) && loopDone(s)
	}, 8*time.Second)
	c.check(strings.Contains(shrunk, `"scroll":false`),
		"the short form needs no scrollbar: %s", shrunk)
	c.check(heightOf(shrunk) > 0 && heightOf(shrunk) < heightOf(fields),
		"the window shrank with its fields: %s after %s", shrunk, fields)
	c.check(belowOf(shrunk) >= 0 && belowOf(shrunk) <= 12,
		"the short form's buttons hold the bottom edge: %s", shrunk)
	c.shot(node, "node-tls")

	// Growing back is the same promise in the other direction, and waiting for it
	// here also settles the resize the socks pass started before the next probe.
	probe(node, probes, setTransport("xhttp"))
	grown := probeUntil(node, probes, formShape, func(s string) bool {
		return strings.Contains(s, `"n":4`) && heightOf(s) == heightOf(fields) && loopDone(s)
	}, 8*time.Second)
	c.check(heightOf(grown) == heightOf(fields),
		"putting the rows back puts the window back: %s vs %s", grown, fields)

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

	// ---- the row menu ----
	// Windows pops a native Win32 menu, which no probe can read, so the order
	// lives in two places: the string the page hands that menu and the fallback
	// menu drawn in the DOM for the other platforms. Both are checked here, and
	// the fallback is the one a screenshot of this window can show.
	// The DOM menu is drawn here only as the fallback, so it has to be opened by
	// hand to be photographed - the same three words the native menu gets.
	menu := probe(win, probes, `
		(() => {
			const items = [...document.querySelectorAll("#ctxMenu .ctx-item")]
				.map((el) => el.textContent.trim());
			const m = document.getElementById("ctxMenu");
			m.style.left = "60px"; m.style.top = "120px"; m.style.display = "block";
			return {items, native: window.selftest.menuLabels};
		})()`)
	c.check(strings.Contains(menu, `"Edit","Test","HTTP test","Delete"`),
		"the row menu offers Edit above Test: %s", menu)
	c.shot(win, "row-menu")
	probe(win, probes, `(() => { document.getElementById("ctxMenu").style.display = "none"; return "sent"; })()`)

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

	// ---- the HTTP test ----
	// The menu's third word is a different probe: it fetches a page through the
	// engine instead of dialling the latency check. The node is a dead one, so
	// what proves the call went down the other path is the URL in the reason.
	probe(win, probes, `(() => { window.selftest.testHttp(1); return "sent"; })()`)
	httpReason := probeUntil(win, probes, `
		(() => {
			const el = document.querySelector(".node-item:nth-child(2) .node-result");
			return el ? (el.getAttribute("title") || "") : "";
		})()`, func(s string) bool { return strings.Contains(s, "canonical.html") }, 90*time.Second)
	c.check(strings.Contains(httpReason, "canonical.html"),
		"the HTTP test reports the page it fetched: %s", httpReason)
	c.shot(win, "node-http")

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
			gutter: (b => b.offsetWidth - b.clientWidth)(document.querySelector(".sheet-body")),
			gap: (() => {
				const last = Math.max(...[...document.querySelectorAll(".sheet-body *")]
					.map((el) => el.getBoundingClientRect().bottom).filter((y) => y > 0));
				return Math.round(document.querySelector(".sheet-actions").getBoundingClientRect().top - last);
			})(),
			below: Math.round(window.innerHeight -
				document.querySelector(".sheet-actions").getBoundingClientRect().bottom),
			// The updater is in the button row and not in the form: it belongs to
			// the window. It holds the row's left edge as firmly as OK holds the
			// right, which is the one promise that survives the padding changing.
			updater: (() => {
				const b = document.getElementById("sUpdateRules");
				const row = document.querySelector(".sheet-actions");
				const rr = row.getBoundingClientRect();
				const br = b.getBoundingClientRect();
				const cr = document.getElementById("settingsCancel").getBoundingClientRect();
				const ok = document.getElementById("settingsOK").getBoundingClientRect();
				return {same: b.parentElement === row, first: br.right < cr.left,
						leftGap: Math.round(br.left - rr.left),
						rightGap: Math.round(rr.right - ok.right)};
			})(),
			inner: [window.innerWidth, window.innerHeight],
		}))()`, func(s string) bool {
		return strings.Contains(s, `"ports":"11909/11908"`) && fitted(s) && loopDone(s)
	}, 10*time.Second)
	c.check(strings.Contains(prefill, `"mode":"direct"`), "settings opens with the saved mode")
	var row struct {
		Updater struct {
			Same     bool `json:"same"`
			First    bool `json:"first"`
			LeftGap  int  `json:"leftGap"`
			RightGap int  `json:"rightGap"`
		} `json:"updater"`
	}
	c.check(json.Unmarshal([]byte(prefill), &row) == nil, "the settings page reports its button row: %s", prefill)
	c.check(row.Updater.Same && row.Updater.First,
		"the rules updater shares the button row, ahead of Cancel: %s", prefill)
	c.check(row.Updater.LeftGap == row.Updater.RightGap && row.Updater.LeftGap > 0,
		"the updater holds the row's left edge as firmly as OK holds the right: %d and %d",
		row.Updater.LeftGap, row.Updater.RightGap)
	c.check(strings.Contains(prefill, `"loginRow":"flex"`), "the start-at-login row is offered on Windows")
	c.check(strings.Contains(prefill, `"scroll":false`), "the settings form needs no scrollbar: %s", prefill)
	c.check(gutterOf(prefill) == 0, "the fitted settings window keeps no scrollbar gutter: %s", prefill)
	c.check(gapOf(prefill) <= 32, "no dead band above the settings buttons: %s", prefill)
	c.check(belowOf(prefill) >= 0 && belowOf(prefill) <= 12,
		"the settings buttons sit at the bottom edge: %s", prefill)
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

	w2, h2, _, _ := mainWindowSize(len(a.st.Nodes))
	win.SetSize(w2, h2)
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

// belowOf is the space a form page reports under its own buttons: what the
// window has left over once the form has taken what it needs.
func belowOf(probeJSON string) int {
	var out struct {
		Below int `json:"below"`
	}
	_ = json.Unmarshal([]byte(probeJSON), &out)
	return out.Below
}

// heightOf is the client height the page reports, which is the window's side of
// the bargain: a form that lost three rows has to lose the window's three rows
// too, or the buttons end up floating above bare window.
func heightOf(probeJSON string) int {
	var out struct {
		Inner [2]int `json:"inner"`
	}
	_ = json.Unmarshal([]byte(probeJSON), &out)
	return out.Inner[1]
}

// gutterOf is how much of the form's width a scrollbar has taken. Zero is the
// only answer that means the window is as wide as its form.
func gutterOf(probeJSON string) int {
	var out struct {
		Gutter int `json:"gutter"`
	}
	_ = json.Unmarshal([]byte(probeJSON), &out)
	return out.Gutter
}

// settled is a form page whose window has finished answering: the buttons are at
// the bottom edge, nothing is clipped, and no scrollbar has been given the room
// the fields wanted. The fit is a loop now, so a single read can land in the
// middle of it - and on macOS in the middle of an animation.
func settled(probeJSON string) bool {
	return belowOf(probeJSON) >= 0 && belowOf(probeJSON) <= 12 &&
		gutterOf(probeJSON) == 0 && !strings.Contains(probeJSON, `"scroll":true`)
}

// fitted is settled plus the mark the fit leaves behind: the window asked for
// exactly the form and its two pixels of rounding, so the buttons sit two pixels
// off the bottom edge. The first read of a window needs that, or it can be told
// the window is the wrong height by the height it opened at.
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

// waitOwner polls until w reports want as its owner. keepAbove runs after the
// window is created and filed, so a form is briefly a stranger on the desktop;
// reading the owner once turns that moment into a failed assertion on a machine
// that starts windows more slowly than this one does.
func waitOwner(w application.Window, want uintptr, limit time.Duration) uintptr {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if owner := ownerOf(w); owner == want {
			return owner
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ownerOf(w)
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
