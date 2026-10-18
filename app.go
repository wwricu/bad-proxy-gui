package main

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wwricu/bad-proxy-gui/tunplat"
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
	tun        tunRunner
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
	// A run that was killed rather than closed left the system resolver pointed
	// at this app's DNS port. Fix that before anything else touches the network.
	a.recoverTunToken()
	// The engine runs from launch: direct mode is always buildable, so the
	// local inbounds answer immediately and mode switches are cheap restarts.
	if err := a.eng.start(a.st); err != nil {
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

// TestNode probes one node without touching the running proxy: it starts a
// throwaway engine on random high ports with that node as the only outbound,
// dials the 204 probe through it, and stops the engine.
func (a *App) TestNode(index int) (string, error) {
	if index < 0 || index >= len(a.st.Nodes) {
		return "", fmt.Errorf("invalid index: %d", index)
	}
	st := a.st
	st.ProxyMode = modeProxy
	st.Active = index
	var e engine
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		p1 := 20000 + rand.Intn(40000)
		st.HttpPort = fmt.Sprintf("%d", p1)
		st.SocksPort = fmt.Sprintf("%d", p1+1)
		err = e.start(st)
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("start test engine: %w", err)
	}
	defer e.stop()
	latency, err := st.dialThroughProxy()
	if err != nil {
		return "", err
	}
	return latency.Round(1e6).String(), nil
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
	// The tunnel dials this engine, so a new port or a new routing preset has
	// to reach it too. It is re-pointed rather than rebuilt: tearing the adapter
	// down would drop every connection for a settings change.
	a.tun.refresh(a.st)
	return nil
}

// shutdown stops the tunnel first and the engine second. The tunnel's resolver
// answers through the engine's SOCKS inbound and it owns the machine's DNS
// settings, so both have to be released before the process leaves the building.
func (a *App) shutdown() {
	if a.tun.running() {
		a.tun.down()
		a.st.TunEnabled = false
	}
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
		"tunSupported":         tunplat.Supported(),
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

// ConfirmDialog pops a real OS yes/no dialog owned by the app window and
// reports the user's choice.
func (a *App) ConfirmDialog(title, message string) bool {
	return showMessageBox(title, message, true)
}

// InfoDialog pops a real OS alert. The frontend cannot use window.alert
// because the WebView2 host suppresses it.
func (a *App) InfoDialog(title, message string) bool {
	return showMessageBox(title, message, false)
}
