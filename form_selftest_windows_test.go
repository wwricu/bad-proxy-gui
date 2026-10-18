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

// Drives the app the way the buttons do, checks what the page actually
// rendered, and writes a screenshot of each state. The forms are sheets inside
// the one window now, so every probe goes to that window and every step starts
// from a real click on a real control.
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
		URL:       "/",
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
		}).join(" ") + " | closed=" +
		(document.getElementById("nodeSheet").hidden && document.getElementById("settingsSheet").hidden)`)
	c.check(strings.Contains(readouts, "roHttp=SPAN:11909"), "the bar shows the HTTP port as text: %q", readouts)
	c.check(strings.Contains(readouts, "roSocks=SPAN:11908"), "the bar shows the SOCKS port as text")
	c.check(strings.Contains(readouts, "roMode=SPAN:Direct"), "the bar names the mode in words")
	c.check(strings.Contains(readouts, "closed=true"), "both sheets start closed")

	// The minimum size is what the layout has to survive: the bar's three zones
	// stay reachable and the tallest sheet fits without a scrollbar.
	win.SetSize(mainWindowMinW, mainWindowMinH)
	time.Sleep(800 * time.Millisecond)
	fits := probe(win, probes, `
		(() => {
			const d = document.documentElement;
			const bar = document.querySelector(".status-bar");
			const box = (id) => {
				const r = document.getElementById(id).getBoundingClientRect();
				return [Math.round(r.left), Math.round(r.right)];
			};
			return {page: [d.scrollWidth, d.clientWidth, d.scrollHeight, d.clientHeight],
					bar: [bar.clientWidth, bar.scrollWidth],
					add: box("addNodeBtn"), gear: box("gearBtn"),
					addLabel: document.getElementById("addNodeBtn").textContent.trim(),
					facts: Math.round(document.querySelector(".readout").getBoundingClientRect().left),
					statusRight: Math.round(document.getElementById("statusText").getBoundingClientRect().right),
					inner: d.clientWidth};
		})()`)
	var layout struct {
		Page        [4]int `json:"page"`
		Bar         [2]int `json:"bar"`
		Add         [2]int `json:"add"`
		AddLabel    string `json:"addLabel"`
		Gear        [2]int `json:"gear"`
		Facts       int    `json:"facts"`
		StatusRight int    `json:"statusRight"`
		Inner       int    `json:"inner"`
	}
	c.check(json.Unmarshal([]byte(fits), &layout) == nil, "the window reports its own layout: %s", fits)
	if layout.Inner > 0 {
		c.check(layout.Page[0] <= layout.Page[1]+1, "the page itself does not overflow: %v", layout.Page)
		c.check(layout.Bar[1] <= layout.Bar[0]+1, "the status bar is not wider than the window: %v", layout.Bar)
		c.check(layout.AddLabel == "", "the add button is an icon, not a word: %q", layout.AddLabel)
		c.check(layout.Add[0] >= 0 && layout.Add[1] <= layout.Gear[0], "the actions keep their order: %v then %v", layout.Add, layout.Gear)
		c.check(layout.Gear[1] <= layout.Facts, "the actions are the left group, the facts follow: gear=%v facts=%d", layout.Gear, layout.Facts)
		c.check(layout.StatusRight <= layout.Inner+1 && layout.StatusRight > layout.Inner-40,
			"the status line holds the right edge: %d of %d", layout.StatusRight, layout.Inner)
	}
	c.shot(win, "main-minimum")

	// ---- the node editor ----
	// The click is the whole entry point; the sheet appears in this window.
	probe(win, probes, `(() => { document.getElementById("addNodeBtn").click(); return "sent"; })()`)
	opened := probeUntil(win, probes, `document.getElementById("nodeSheet").hidden`,
		func(s string) bool { return s == "false" }, 5*time.Second)
	if opened != "false" {
		c.fail("the add button never opened the editor")
		a.quit()
		return
	}
	c.ok("the add button opened the editor in the same window")
	fields := probe(win, probes, `
		(() => {
			const shown = (id) => getComputedStyle(document.getElementById(id)).display !== "none";
			const body = document.querySelector("#nodeSheet .sheet-body");
			const r = document.querySelector("#nodeSheet .sheet").getBoundingClientRect();
			return {shown: ["fSecret","fAuthority","fPath","fPadding"].filter(shown),
					title: document.getElementById("nodeSheetTitle").textContent,
					scroll: body.scrollHeight > body.clientHeight,
					sheet: [Math.round(r.width), Math.round(r.height)],
					room: body.clientHeight};
		})()`)
	c.check(strings.Contains(fields, `"fSecret","fAuthority","fPath","fPadding"`),
		"xhttp offers every optional field: %s", fields)
	c.check(strings.Contains(fields, `"title":"Add node"`), "the sheet says what it is: %s", fields)
	c.check(strings.Contains(fields, `"scroll":false`),
		"the tallest form needs no scrollbar at the minimum window size: %s", fields)
	c.shot(win, "node-add")

	// A protocol that hides a row has to take the buttons with it: the form
	// flows from the top, so the spare room belongs under the actions, not in
	// the middle of the fields.
	trimmed := probe(win, probes, `
		(() => {
			const sel = document.getElementById("fProtocol");
			sel.value = "socks";
			sel.dispatchEvent(new Event("change"));
			const hidden = (id) => getComputedStyle(document.getElementById(id)).display === "none";
			const actions = document.querySelector("#nodeSheet .sheet-actions").getBoundingClientRect();
			const body = document.querySelector("#nodeSheet .sheet-body");
			const out = {hidden: ["fSecret","fAuthority","fPath","fPadding"].filter(hidden),
					fits: actions.bottom <= window.innerHeight,
					scroll: body.scrollHeight > body.clientHeight};
			sel.value = "vless";
			sel.dispatchEvent(new Event("change"));
			return out;
		})()`)
	c.check(strings.Contains(trimmed, `"hidden":["fSecret"]`), "socks hides only the secret: %s", trimmed)
	c.check(strings.Contains(trimmed, `"fits":true`) && strings.Contains(trimmed, `"scroll":false`),
		"a shorter form keeps its buttons in view: %s", trimmed)

	// The validation is synchronous, so the click and the read are one pass.
	refusal := probe(win, probes, `
		(() => {
			document.getElementById("modalSave").click();
			return document.getElementById("fMsg").textContent;
		})()`)
	c.check(strings.Contains(refusal, "required"), "an empty form refuses in its own line: %q", refusal)
	c.check(strings.Contains(probe(win, probes, `document.getElementById("nodeSheet").hidden`), "false"),
		"a refused save leaves the editor open")

	probe(win, probes, `
		(() => {
			const set = (id, v) => { document.getElementById(id).value = v; };
			set("fName","gamma"); set("fServer","gamma.example.invalid"); set("fPort","2053");
			set("fSecret","synthetic-2"); set("fAuthority","gamma.example.invalid");
			document.getElementById("modalSave").click();
			return "sent";
		})()`)
	closed := probeUntil(win, probes, `document.getElementById("nodeSheet").hidden`,
		func(s string) bool { return s == "true" }, 8*time.Second)
	c.check(closed == "true", "a good save closes the editor")
	c.check(len(stateFromFile(dir).Nodes) == 2, "the new node reached state.json: %d nodes", len(stateFromFile(dir).Nodes))
	rows := probeUntil(win, probes, `document.querySelectorAll(".node-item").length`,
		func(s string) bool { return s == "2" }, 8*time.Second)
	c.check(rows == "2", "the closed editor told the list to reread: %q rows", rows)

	// ---- the settings sheet ----
	probe(win, probes, `(() => { document.getElementById("gearBtn").click(); return "sent"; })()`)
	// The sheet fills itself from two bindings, so the read has to wait for the
	// draft to land rather than read the empty form it starts with.
	prefill := probeUntil(win, probes, `
		(() => ({
			open: document.getElementById("settingsSheet").hidden === false,
			ports: ["sHttpPort","sSocksPort"].map(id => document.getElementById(id).value).join("/"),
			mode: document.getElementById("sMode").value,
			tunRow: getComputedStyle(document.getElementById("sTunRow")).display,
			loginRow: getComputedStyle(document.getElementById("sLoginRow")).display,
			scroll: document.querySelector("#settingsSheet .sheet-body").scrollHeight >
			        document.querySelector("#settingsSheet .sheet-body").clientHeight,
		}))()`, func(s string) bool { return strings.Contains(s, `"ports":"11909/11908"`) }, 10*time.Second)
	c.check(strings.Contains(prefill, `"open":true`), "the gear opened the settings sheet in the same window")
	c.check(strings.Contains(prefill, `"mode":"direct"`), "settings opens with the saved mode")
	c.check(strings.Contains(prefill, `"tunRow":"flex"`), "the TUN row is offered where the platform supports it")
	c.check(strings.Contains(prefill, `"loginRow":"flex"`), "the start-at-login row is offered on Windows")
	c.check(strings.Contains(prefill, `"scroll":false`), "the settings form needs no scrollbar: %s", prefill)
	c.shot(win, "settings")

	// Equal ports: the refusal belongs on this sheet's message line, and the
	// sheet has to stay so the draft can be fixed.
	probe(win, probes, `
		(() => {
			document.getElementById("sSocksPort").value = "11909";
			document.getElementById("settingsOK").click();
			return "sent";
		})()`)
	errText := probeUntil(win, probes, `document.getElementById("sMsg").textContent`,
		func(s string) bool { return strings.Contains(s, "differ") }, 10*time.Second)
	c.check(strings.Contains(errText, "differ"), "a bad draft is reported inside the settings sheet: %q", errText)
	c.check(strings.Contains(probe(win, probes, `document.getElementById("settingsSheet").hidden`), "false"),
		"a refused draft leaves the settings sheet open")
	c.check(stateFromFile(dir).SocksPort == "11908", "a refused draft did not reach state.json")

	// A good draft closes the sheet and reaches both the file and the bar.
	probe(win, probes, `
		(() => {
			document.getElementById("sHttpPort").value = "11911";
			document.getElementById("sSocksPort").value = "11910";
			document.getElementById("settingsOK").click();
			return "sent";
		})()`)
	closed = probeUntil(win, probes, `document.getElementById("settingsSheet").hidden`,
		func(s string) bool { return s == "true" }, 15*time.Second)
	c.check(closed == "true", "a good draft closes the settings sheet")
	applied := stateFromFile(dir)
	c.check(applied.HttpPort == "11911" && applied.SocksPort == "11910",
		"OK applied the draft: %s/%s", applied.HttpPort, applied.SocksPort)
	after := probeUntil(win, probes, `document.getElementById("roHttp").textContent`,
		func(s string) bool { return s == "11911" }, 8*time.Second)
	c.check(after == "11911", "the bar followed the applied setting: %q", after)

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
	saved := probeUntil(win, probes, `
		(() => ({title: document.getElementById("nodeSheetTitle").textContent,
				name: document.getElementById("fName").value,
				server: document.getElementById("fServer").value}))()`,
		func(s string) bool { return strings.Contains(s, "alpha") }, 8*time.Second)
	c.check(strings.Contains(saved, `"title":"Edit node"`) && strings.Contains(saved, `"name":"alpha"`) &&
		strings.Contains(saved, `"server":"alpha.example.invalid"`),
		"a double click opens the editor prefilled with that node: %s", saved)
	c.shot(win, "node-edit")

	// Esc is the third way out, and it must not half-close: the sheet goes and
	// nothing is written.
	before := stateFromFile(dir)
	probe(win, probes, `
		(() => { document.getElementById("fName").value = "typo";
			document.dispatchEvent(new KeyboardEvent("keydown", {key: "Escape", bubbles: true}));
			return "sent"; })()`)
	esc := probeUntil(win, probes, `document.getElementById("nodeSheet").hidden`,
		func(s string) bool { return s == "true" }, 5*time.Second)
	c.check(esc == "true", "Esc dismisses the sheet")
	c.check(len(stateFromFile(dir).Nodes) == len(before.Nodes), "a dismissed sheet wrote nothing")

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

func stateFromFile(dir string) GUIState {
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return GUIState{}
	}
	var s GUIState
	_ = json.Unmarshal(b, &s)
	return s
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
