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

// Drives the v3 form windows the way the buttons do, checks what the pages
// actually rendered, and writes a screenshot of each window. This replaces the
// Win32 dialog self-tests: the forms are webviews now, so the checks read the
// DOM through the runtime instead of sending control messages to widgets, and
// the pixels come from PrintWindow because a background process cannot take the
// foreground.
//
//	BADPROXY_GUI_SELFTEST=1 go test -tags production -run TestFormWindows -count=1 -v
//
// An ordinary "go test ./..." run skips it: it opens real windows. Everything it
// writes goes to a temp directory seeded with a synthetic node, so the user's
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
		Width:     820,
		Height:    500,
		MinWidth:  640,
		MinHeight: 420,
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
// uses, and each assertion reads back what the page rendered or what landed on
// disk.
func checkForms(a *App, c *checker, probes chan map[string]any, dir string) {
	time.Sleep(2500 * time.Millisecond)

	// ---- the main window ----
	c.shot(a.mainWindow, "main")
	readouts := probe(a.mainWindow, probes, `
		["roHttp","roSocks","roMode","roRouting"].map(id => {
			const el = document.getElementById(id);
			return id + "=" + el.tagName + ":" + el.textContent;
		}).join(" ") + " | cards=" +
		(document.getElementById("modalOverlay") === null) + "," +
		(document.getElementById("settingsOverlay") === null)`)
	c.check(strings.Contains(readouts, "roHttp=SPAN:11909"), "the bar shows the HTTP port as text: %q", readouts)
	c.check(strings.Contains(readouts, "roSocks=SPAN:11908"), "the bar shows the SOCKS port as text")
	c.check(strings.Contains(readouts, "roMode=SPAN:Direct"), "the bar names the mode in words")
	c.check(strings.Contains(readouts, "cards=true,true"), "the in-page form cards are gone")

	// The minimum size is what the scrollbar report was about: nothing on the
	// bar may be pushed out of reach now that the forms are elsewhere.
	a.mainWindow.SetSize(640, 420)
	time.Sleep(800 * time.Millisecond)
	fits := probe(a.mainWindow, probes, `
		(() => {
			const d = document.documentElement;
			const bar = document.querySelector(".status-bar");
			return {page: [d.scrollWidth, d.clientWidth, d.scrollHeight, d.clientHeight],
					bar: [bar.clientWidth, bar.scrollWidth],
					add: Math.round(document.getElementById("addNodeBtn").getBoundingClientRect().right),
					inBar: !!document.querySelector(".status-bar #addNodeBtn"),
					gear: Math.round(document.getElementById("gearBtn").getBoundingClientRect().right),
					inner: d.clientWidth};
		})()`)
	var layout struct {
		Page  [4]int `json:"page"`
		Bar   [2]int `json:"bar"`
		Add   int    `json:"add"`
		InBar bool   `json:"inBar"`
		Gear  int    `json:"gear"`
		Inner int    `json:"inner"`
	}
	c.check(json.Unmarshal([]byte(fits), &layout) == nil, "the main window reports its own layout: %s", fits)
	c.check(layout.InBar, "both actions live in the status bar")
	if layout.Inner > 0 {
		c.check(layout.Page[0] <= layout.Page[1]+1, "the page itself does not overflow: %v", layout.Page)
		c.check(layout.Bar[1] <= layout.Bar[0]+1, "the status bar is not wider than the window: %v", layout.Bar)
		c.check(layout.Add <= layout.Inner+1 && layout.Add < layout.Gear, "the add button is reachable: right=%d gear=%d inner=%d", layout.Add, layout.Gear, layout.Inner)
		c.check(layout.Gear <= layout.Inner+1, "the gear is still reachable: right=%d inner=%d", layout.Gear, layout.Inner)
	}
	c.shot(a.mainWindow, "main-minimum")
	a.mainWindow.SetSize(820, 500)
	time.Sleep(500 * time.Millisecond)

	// ---- the node editor ----
	if err := a.AddNodeUI(); err != nil {
		c.fail("AddNodeUI: %v", err)
		a.quit()
		return
	}
	node := a.formByName(nodeWindowPrefix + "new")
	if node == nil {
		c.fail("the add-node window never appeared")
		a.quit()
		return
	}
	fields := probe(node, probes, `
		(() => {
			const shown = (id) => getComputedStyle(document.getElementById(id)).display !== "none";
			return {shown: ["fSecret","fAuthority","fPath","fPadding"].filter(shown),
					scroll: document.body.scrollHeight > document.body.clientHeight,
					viewport: [document.body.clientWidth, document.body.clientHeight]};
		})()`)
	c.check(strings.Contains(fields, `"fSecret","fAuthority","fPath","fPadding"`),
		"xhttp offers every optional field: %s", fields)
	c.check(strings.Contains(fields, `"scroll":false`),
		"the tallest form needs no scrollbar: %s", fields)
	// PrintWindow on a window that has only just been shown comes out black:
	// the page is loaded (the probes above answered) but the compositor has not
	// drawn a frame yet, so give it one before asking for the pixels.
	time.Sleep(1500 * time.Millisecond)
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
			const actions = document.querySelector(".modal-actions").getBoundingClientRect();
			const out = {hidden: ["fSecret","fAuthority","fPath","fPadding"].filter(hidden),
					fits: actions.bottom <= document.body.clientHeight,
					scroll: document.body.scrollHeight > document.body.clientHeight};
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

	probe(node, probes, `
		(() => {
			const set = (id, v) => { document.getElementById(id).value = v; };
			set("fName","gamma"); set("fServer","gamma.example.invalid"); set("fPort","2053");
			set("fSecret","synthetic-2"); set("fAuthority","gamma.example.invalid");
			document.getElementById("modalSave").click();
			return "sent";
		})()`)
	c.check(waitClosed(a, nodeWindowPrefix+"new", 8*time.Second), "a good save closes the editor")
	c.check(len(stateFromFile(dir).Nodes) == 2, "the new node reached state.json: %d nodes", len(stateFromFile(dir).Nodes))
	rows := probeUntil(a.mainWindow, probes, `document.querySelectorAll(".node-item").length`,
		func(s string) bool { return s == "2" }, 8*time.Second)
	c.check(rows == "2", "the closed editor told the list to reread: %q rows", rows)

	// ---- the settings window ----
	a.SettingsUI()
	settings := a.formByName(settingsWindowName)
	if settings == nil {
		c.fail("the settings window never appeared")
		a.quit()
		return
	}
	// The page fills itself from two bindings, so the read has to wait for the
	// draft to land rather than read the empty form the window starts with.
	prefill := probeUntil(settings, probes, `
		(() => ({
			ports: ["sHttpPort","sSocksPort"].map(id => document.getElementById(id).value).join("/"),
			mode: document.getElementById("sMode").value,
			tunRow: getComputedStyle(document.getElementById("sTunRow")).display,
			loginRow: getComputedStyle(document.getElementById("sLoginRow")).display,
			scroll: document.body.scrollHeight > document.body.clientHeight,
		}))()`, func(s string) bool { return strings.Contains(s, `"ports":"11909/11908"`) }, 10*time.Second)
	c.check(strings.Contains(prefill, `"ports":"11909/11908"`), "settings opens with the saved ports: %s", prefill)
	c.check(strings.Contains(prefill, `"mode":"direct"`), "settings opens with the saved mode")
	c.check(strings.Contains(prefill, `"tunRow":"flex"`), "the TUN row is offered where the platform supports it")
	c.check(strings.Contains(prefill, `"loginRow":"flex"`), "the start-at-login row is offered on Windows")
	c.check(strings.Contains(prefill, `"scroll":false`), "the settings form needs no scrollbar: %s", prefill)
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
	after := probeUntil(a.mainWindow, probes, `document.getElementById("roHttp").textContent`,
		func(s string) bool { return s == "11911" }, 8*time.Second)
	c.check(after == "11911", "the bar followed the applied setting: %q", after)

	// ---- clicking the list ----
	// A row is the only thing on the main page that writes, so both of its
	// gestures are worth the pass: a click selects, a double click opens the
	// editor for that one node.
	probe(a.mainWindow, probes, `
		(() => { document.querySelectorAll(".node-item")[1].click(); return "sent"; })()`)
	active := probeUntil(a.mainWindow, probes,
		`[...document.querySelectorAll(".node-item")].findIndex(el => el.classList.contains("active"))`,
		func(s string) bool { return s == "1" }, 8*time.Second)
	c.check(active == "1", "a click moves the highlight: row %s active", active)
	c.check(stateFromFile(dir).Active == 1, "the click reached state.json: active=%d", stateFromFile(dir).Active)

	probe(a.mainWindow, probes, `
		(() => { document.querySelectorAll(".node-item")[0].dispatchEvent(new MouseEvent("dblclick", {bubbles: true})); return "sent"; })()`)
	deadline := time.Now().Add(8 * time.Second)
	for a.formByName("node-0") == nil && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	edit := a.formByName("node-0")
	if edit == nil {
		c.fail("a double click never opened the editor for that node")
	} else {
		saved := probeUntil(edit, probes, `
			(() => ({name: document.getElementById("fName").value,
					server: document.getElementById("fServer").value}))()`,
			func(s string) bool { return strings.Contains(s, "alpha") }, 8*time.Second)
		c.check(strings.Contains(saved, `"name":"alpha"`) && strings.Contains(saved, `"server":"alpha.example.invalid"`),
			"the editor opens prefilled with the double-clicked node: %s", saved)
		c.shot(edit, "node-edit")
	}

	// Opening the gear twice must raise the one window, not stack a second.
	a.SettingsUI()
	a.SettingsUI()
	time.Sleep(1200 * time.Millisecond)
	c.shot(a.formByName(settingsWindowName), "settings-reopen")

	c.shot(a.mainWindow, "main-final")
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
