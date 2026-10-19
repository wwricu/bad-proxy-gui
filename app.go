package main

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// App exposes backend methods to the frontend via Wails runtime binding.
type App struct {
	wails      *application.App
	mainWindow application.Window
	tray       *application.SystemTray
	eng        engine
	st         GUIState
	store      *store
	trayOn     bool
	quitting   bool
}

// newApp loads the saved state. It runs before the window exists: the state
// decides whether there is a tray to hide into.
func newApp() *App {
	a := &App{}
	a.store = newStoreFromConfigDir()
	a.st = a.store.load()
	return a
}

// ServiceStartup is Wails' hook for a bound service. It runs once the run loop
// is up, which is the first moment a window or a tray may be touched.
func (a *App) ServiceStartup(context.Context, application.ServiceOptions) error {
	// The engine runs from launch: direct mode is always buildable, so the
	// local inbounds answer immediately and mode switches are cheap restarts.
	if err := a.eng.start(a.st); err != nil {
		println("Engine start failed:", err.Error())
		return nil
	}
	// Relaunching in system mode re-points the OS proxy at the (now-running)
	// inbound. Non-system launches must not touch it: a direct/proxy start
	// should leave whatever proxy config the user has alone.
	if a.st.ProxyMode == modeSystem {
		_ = setSystemProxy(a.st.HttpPort)
	}
	return nil
}

// showWindow re-opens the main window after it was hidden to the tray.
func (a *App) showWindow() {
	if a.mainWindow == nil {
		return
	}
	becomeForeground(windowTitle)
	a.mainWindow.UnMinimise()
	a.mainWindow.Show()
	a.mainWindow.Focus()
	a.showFormWindow()
}

// hideToTray takes the windows off the screen and leaves the app running. The
// form is asked first because on Windows an owned window is not hidden along
// with its owner, and left behind it would float on the desktop with no taskbar
// button of its own to reach it. macOS is the other way round: AppKit keeps a
// child window with its parent and will not let it hide alone, so the call
// lands as a no-op and the list taking the form down with it is what works.
func (a *App) hideToTray() {
	a.hideFormWindow()
	becomeBackground()
	if a.mainWindow != nil {
		a.mainWindow.Hide()
	}
}

// quit stops the engine and exits, bypassing the hide-to-tray close guard.
func (a *App) quit() {
	a.quitting = true
	a.wails.Quit()
}

// GetState returns the current GUI state to the frontend.
func (a *App) GetState() GUIState {
	return a.st
}

// AddNode adds a new node to the list.
func (a *App) AddNode(n Node) error {
	a.st.Nodes = append(a.st.Nodes, n)
	if a.st.Active < 0 {
		a.st.Active = 0
	}
	a.st.normalize()
	if err := a.store.save(a.st); err != nil {
		return err
	}
	return a.restartEngine()
}

// UpdateNode replaces the node at index, keeping its position.
func (a *App) UpdateNode(index int, n Node) error {
	if index < 0 || index >= len(a.st.Nodes) {
		return fmt.Errorf("invalid index: %d", index)
	}
	a.st.Nodes[index] = n
	a.st.normalize()
	if err := a.store.save(a.st); err != nil {
		return err
	}
	return a.restartEngine()
}

// DeleteNode removes a node by index.
func (a *App) DeleteNode(index int) error {
	if index < 0 || index >= len(a.st.Nodes) {
		return fmt.Errorf("invalid index: %d", index)
	}
	a.st.Nodes = append(a.st.Nodes[:index:index], a.st.Nodes[index+1:]...)
	a.st.normalize()
	if err := a.store.save(a.st); err != nil {
		return err
	}
	return a.restartEngine()
}

// AddNodeUI opens the editor for a new node in its own window. The window
// writes through AddNode itself, so there is nothing to collect here.
func (a *App) AddNodeUI() error {
	return a.openNodeWindow(-1)
}

// EditNodeUI opens the editor for the node at index.
func (a *App) EditNodeUI(index int) error {
	return a.openNodeWindow(index)
}

// SelectNode sets the active node.
func (a *App) SelectNode(index int) error {
	a.st.Active = index
	a.st.normalize()
	if err := a.store.save(a.st); err != nil {
		return err
	}
	if a.st.ProxyMode != modeDirect {
		return a.restartEngine()
	}
	return nil
}

// TestConnection runs a connectivity test through the proxy.
func (a *App) TestConnection() (string, error) {
	if !a.eng.running() {
		return "", fmt.Errorf("proxy not running")
	}
	latency, err := a.st.dialThroughProxy()
	if err != nil {
		return "", err
	}
	return latency.Round(1e6).String(), nil
}

// TestNode probes one node's reachability: does the tunnel to it come up and
// can it carry a request, measured as the latency of a 204 ping through a
// throwaway engine that uses only that node.
func (a *App) TestNode(index int) (string, error) {
	d, err := probeNode(a.st, index, GUIState.dialThroughProxy)
	if err != nil {
		return "", err
	}
	return d.Round(1e6).String(), nil
}

// TestHttp probes one node's end-to-end web reachability: beyond the tunnel
// coming up, it fetches a real plain-HTTP page through the node and requires a
// 2xx with a body, so it answers "can I actually load a site through this node"
// rather than just "is the server there".
func (a *App) TestHttp(index int) (string, error) {
	d, err := probeNode(a.st, index, GUIState.dialHTTPThroughProxy)
	if err != nil {
		return "", err
	}
	return "HTTP " + d.Round(1e6).String(), nil
}

// probeNode starts a throwaway engine with index as its only outbound on random
// high ports, runs probe through that engine's http inbound, and stops it. The
// running proxy is untouched: the throwaway engine cannot collide with it and a
// failing node cannot take the app down.
//
// The port is bound here first, before core is handed it. Windows carves large
// slices of the dynamic range away for Hyper-V and winnat, and a bind into one
// of those is a permanent "access forbidden" that core writes to its log and
// then starts the rest of the engine around: start() returns no error, nothing
// ever listens, and the user is told their node timed out. Asking the OS for
// the port turns that into a different port instead of a false verdict about
// somebody's network.
func probeNode(st GUIState, index int, probe func(GUIState) (time.Duration, error)) (time.Duration, error) {
	if index < 0 || index >= len(st.Nodes) {
		return 0, fmt.Errorf("invalid index: %d", index)
	}
	httpPort, socksPort, err := freePortPair()
	if err != nil {
		return 0, err
	}
	st.ProxyMode = modeProxy
	st.Active = index
	st.HttpPort = httpPort
	st.SocksPort = socksPort
	var e engine
	if err := e.start(st); err != nil {
		return 0, fmt.Errorf("start test engine: %w", err)
	}
	defer e.stop()
	// core binds its inbounds on its own clock, so probing the instant start
	// returns can find nothing listening yet. That failure says "connection
	// refused" about a port of our own choosing, which reads as a broken node.
	if err := waitListening(st.HttpPort); err != nil {
		return 0, err
	}
	return probe(st)
}

// freePortPair returns two adjacent loopback ports this machine will let us
// bind, released again so core can have them. A leftover listener from an
// earlier probe holding one is as good a reason to move on as a reserved range.
func freePortPair() (httpPort, socksPort string, err error) {
	p, err := pickFreePort(20000, 40000, 8)
	if err != nil {
		return "", "", err
	}
	return strconv.Itoa(p), strconv.Itoa(p + 1), nil
}

// pickFreePort draws up to attempts ports from [start, start+span) and returns
// the first that can be bound along with its neighbour.
func pickFreePort(start, span, attempts int) (int, error) {
	for i := 0; i < attempts; i++ {
		if p := start + rand.Intn(span); portsFree(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free local port for the test engine")
}

// portsFree reports whether p and p+1 can both be bound right now. This is the
// only question worth asking about a Windows excluded port range: a port inside
// one never binds, however long the caller is willing to wait for it.
func portsFree(p int) bool {
	a, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		return false
	}
	defer a.Close()
	b, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p+1))
	if err != nil {
		return false
	}
	b.Close()
	return true
}

// waitListening returns once something accepts connections on the local port,
// or gives up after eight seconds of asking. The budget is not for the common
// case - a refused connection comes back in milliseconds - but for the machine
// that is busy: core binds its inbounds on its own clock, and on a loaded box
// that is later than anything this loop could predict. Two seconds of that was
// a "the test engine never came up" on a machine that was merely slow, which
// reads to the user as a broken node.
func waitListening(port string) error {
	for attempt := 0; attempt < 160; attempt++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 250*time.Millisecond)
		if err == nil {
			return conn.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("the test engine never came up on port %s", port)
}

// restartEngine rebuilds and restarts the embedded core proxy. The state is
// already on disk when this runs, so a failure here means "saved, proxy not
// running" rather than "not saved" - the message has to say which.
func (a *App) restartEngine() error {
	if a.eng.running() {
		a.eng.stop()
	}
	if err := a.eng.start(a.st); err != nil {
		return fmt.Errorf("proxy restart failed: %w", err)
	}
	return nil
}

// shutdown releases the OS proxy last: the engine has to stay up until the
// resolver no longer points at it.
func (a *App) shutdown() {
	if a.eng.running() {
		a.eng.stop()
	}
	if a.st.ProxyMode == modeSystem {
		_ = clearSystemProxy()
	}
}

// GetPlatformInfo returns platform-specific capabilities.
func (a *App) GetPlatformInfo() map[string]bool {
	return map[string]bool{
		"startLoginSupported":  startAtLoginSupported,
		"trayCanHide":          trayCanHide,
		"systemProxySupported": systemProxySupported,
		"nativeContextMenu":    nativeContextMenuSupported,
	}
}

// ShowContextMenu pops a native OS shortcut menu with the given newline
// separated labels at the mouse cursor and returns the selected index, or -1
// when dismissed. The frontend only calls it when GetPlatformInfo reports
// nativeContextMenu.
func (a *App) ShowContextMenu(labelsNL string) int {
	sel, err := showNativeContextMenu(strings.Split(labelsNL, "\n"))
	if err != nil {
		println("native context menu unavailable: " + err.Error())
		return -1
	}
	return sel
}
