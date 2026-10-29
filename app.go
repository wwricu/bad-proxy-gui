package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wwricu/bad-proxy-gui/internal/config"
	"github.com/wwricu/bad-proxy-gui/internal/engine"
	"github.com/wwricu/bad-proxy-gui/internal/platform"
)

// App exposes backend methods to the frontend via Wails runtime binding.
//
// Each of those methods runs on a goroutine of its own: the runtime transport is
// an ordinary net/http handler, so two clicks are live at the same time - the
// list reading its state while an editor saves one, or a probe holding a state
// for the seconds its test engine takes to answer. So st is only reached through
// snapshot and edit, both under mu, and the copies they hand out own their node
// list.
type App struct {
	wails      *application.App
	mainWindow application.Window
	eng        engine.Engine
	st         config.State
	store      *config.Store
	mu         sync.Mutex // guards st
	quitting   atomic.Bool
	trayOn     atomic.Bool // a close hides the window rather than ending the app; the tray files say what this platform's answer is
}

// newApp loads the saved state. It runs before the window exists: the state
// decides whether there is a tray to hide into, and a state file that cannot be
// parsed has to be reported somewhere other than a window that does not exist
// yet.
func newApp() *App {
	a := &App{}
	a.store = config.OpenStore()
	st, err := a.store.Load()
	a.st = st
	if err != nil {
		logf("state: %v", err)
	}
	return a
}

// snapshot is the only read of the live state.
func (a *App) snapshot() config.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.st.Clone()
}

// edit is the only write of the live state. It returns the state as it was and
// as it now is, and the engine follows outside: a restart must not hold mu while
// core tears its listeners down, and a change that buildConfig cannot see must
// not cost a restart at all - that is what restartIfChanged is for.
//
// fn works on a draft. Nothing is touched until the save has succeeded, so a
// refused or failed edit leaves both the running state and the file alone.
func (a *App) edit(fn func(*config.State) error) (prev, next config.State, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	prev = a.st.Clone()
	draft := a.st.Clone()
	if err := fn(&draft); err != nil {
		return prev, prev, err
	}
	draft.Normalize()
	if err := a.store.Save(draft); err != nil {
		return prev, prev, err
	}
	// The live field is replaced rather than edited in place, which is what makes
	// the copy handed out here safe to hold on to.
	a.st = draft
	return prev, draft, nil
}

// restartIfChanged follows a saved change with a restart only when the proxy
// built for the two states actually differs.
func (a *App) restartIfChanged(prev, next config.State) error {
	if !engine.AffectsEngine(prev, next) {
		return nil
	}
	return a.restartEngine(next)
}

// ServiceStartup is Wails' hook for a bound service. It runs once the run loop
// is up, which is the first moment a window or a tray may be touched.
func (a *App) ServiceStartup(context.Context, application.ServiceOptions) error {
	st := a.snapshot()
	// The engine runs from launch: direct mode is always buildable, so the
	// local inbounds answer immediately and mode switches are cheap restarts.
	if err := a.eng.Start(st, ensureRulesDat()); err != nil {
		logf("engine start failed: %v", err)
		return nil
	}
	// Relaunching in system mode re-points the OS proxy at the (now-running)
	// inbound. Non-system launches must not touch it: a direct/proxy start
	// should leave whatever proxy config the user has alone.
	if st.ProxyMode == config.ModeSystem {
		if err := platform.SetSystemProxy(a.store.Dir(), st.HttpPort); err != nil {
			logf("system proxy not set on relaunch: %v", err)
		}
	}
	return nil
}

// showWindow re-opens the main window after it was hidden to the tray.
func (a *App) showWindow() {
	if a.mainWindow == nil {
		return
	}
	platform.BecomeForeground(windowTitle)
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
	platform.BecomeBackground()
	if a.mainWindow != nil {
		a.mainWindow.Hide()
	}
}

// quit stops the engine and exits, bypassing the hide-to-tray close guard.
func (a *App) quit() {
	a.quitting.Store(true)
	a.wails.Quit()
}

// GetState returns the current GUI state to the frontend.
func (a *App) GetState() config.State {
	return a.snapshot()
}

// AddNode adds a new node to the list.
func (a *App) AddNode(n config.Node) error {
	prev, next, err := a.edit(func(s *config.State) error {
		s.Nodes = append(s.Nodes, n)
		if s.Active < 0 {
			s.Active = 0
		}
		return nil
	})
	if err != nil {
		return err
	}
	return a.restartIfChanged(prev, next)
}

// UpdateNode replaces the node at index, keeping its position.
func (a *App) UpdateNode(index int, n config.Node) error {
	prev, next, err := a.edit(func(s *config.State) error {
		return setNode(s, index, n)
	})
	if err != nil {
		return err
	}
	return a.restartIfChanged(prev, next)
}

// DeleteNode removes a node by index. Which node the traffic goes through after
// that is RemoveNode's decision, not this one's: deleting a row the user is not
// using must not change the row they are.
func (a *App) DeleteNode(index int) error {
	prev, next, err := a.edit(func(s *config.State) error {
		return s.RemoveNode(index)
	})
	if err != nil {
		return err
	}
	return a.restartIfChanged(prev, next)
}

// setNode replaces one entry of the list, refusing an index the list does not
// have. The check is made against the draft the write goes to, so a delete
// cannot slip in between the two and move a node onto a row someone else asked
// for.
func setNode(s *config.State, index int, n config.Node) error {
	if index < 0 || index >= len(s.Nodes) {
		return fmt.Errorf("invalid index: %d", index)
	}
	s.Nodes[index] = n
	return nil
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
	prev, next, err := a.edit(func(s *config.State) error {
		// Refuse rather than normalise: Normalize folds an out-of-range index
		// onto the first node, and a click that landed on a row the list no
		// longer has would otherwise switch the user to a different node and
		// tell them nothing happened.
		if index < 0 || index >= len(s.Nodes) {
			return fmt.Errorf("invalid index: %d", index)
		}
		s.Active = index
		return nil
	})
	if err != nil {
		return err
	}
	return a.restartIfChanged(prev, next)
}

// TestConnection runs a connectivity test through the proxy.
func (a *App) TestConnection() (string, error) {
	st := a.snapshot()
	if !a.eng.Running() {
		return "", fmt.Errorf("proxy not running")
	}
	latency, err := engine.DialThroughProxy(st)
	if err != nil {
		return "", err
	}
	return latency.Round(1e6).String(), nil
}

// TestNode probes one node's reachability: does the tunnel to it come up and
// can it carry a request, measured as the latency of a 204 ping through a
// throwaway engine that uses only that node.
func (a *App) TestNode(index int) (string, error) {
	d, err := engine.ProbeNode(a.snapshot(), index, ensureRulesDat(), engine.DialThroughProxy)
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
	d, err := engine.ProbeNode(a.snapshot(), index, ensureRulesDat(), engine.DialHTTPThroughProxy)
	if err != nil {
		return "", err
	}
	return "HTTP " + d.Round(1e6).String(), nil
}

// restartEngine rebuilds and restarts the embedded core proxy. The state is
// already on disk when this runs, so a failure here means "saved, proxy not
// running" rather than "not saved" - the message has to say which.
func (a *App) restartEngine(st config.State) error {
	if err := a.eng.Restart(st, ensureRulesDat()); err != nil {
		return fmt.Errorf("proxy restart failed: %w", err)
	}
	return nil
}

// shutdown releases the OS proxy last: the engine has to stay up until the
// resolver no longer points at it.
func (a *App) shutdown() {
	a.eng.Stop()
	if a.snapshot().ProxyMode == config.ModeSystem {
		if err := platform.ClearSystemProxy(a.store.Dir()); err != nil {
			logf("system proxy left pointing at a stopped engine: %v", err)
		}
	}
}

// GetPlatformInfo returns the platform capabilities the frontend builds its
// windows from. The keys are the names settings.js and app.js read, and
// platforminfo_test.go fails if they stop matching: a key the frontend cannot
// see reads as false, which is how the tray row and the System mode both
// disappeared from the settings window without any error anywhere.
func (a *App) GetPlatformInfo() map[string]bool {
	return map[string]bool{
		"startLoginSupported":  platform.StartAtLoginSupported,
		"trayCanHide":          platform.TrayCanHide,
		"trayIsMenuBar":        platform.TrayIsMenuBar,
		"systemProxySupported": platform.SystemProxySupported,
		"nativeContextMenu":    platform.NativeContextMenuSupported,
	}
}

// ShowContextMenu pops a native OS shortcut menu with the given newline
// separated labels at the mouse cursor and returns the selected index, or -1
// when dismissed. The frontend only calls it when GetPlatformInfo reports
// nativeContextMenu.
func (a *App) ShowContextMenu(labelsNL string) int {
	sel, err := platform.ShowNativeContextMenu(strings.Split(labelsNL, "\n"))
	if err != nil {
		logf("native context menu unavailable: %v", err)
		return -1
	}
	return sel
}
