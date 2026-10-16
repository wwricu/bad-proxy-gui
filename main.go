package main

import (
	"fmt"
	"image/color"
	"log"
	"net"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type controller struct {
	fyneApp fyne.App
	win     fyne.Window
	store   *store
	eng     engine
	state   GUIState

	statusText *widget.Label
	nodeList   *widget.List
	primary    *widget.Button
	testBtn    *widget.Button
	socksPort  *widget.Entry
	httpPort   *widget.Entry
	routing    *widget.Select
	sysProxy   *widget.Button
	trayCheck  *widget.Check
	allowLAN   *widget.Check
	autoConn   *widget.Check
	startLogin *widget.Check
}

// routingOpts pair the user-facing labels with the (placeholder) routing modes.
var routingOpts = []struct {
	label string
	mode  string
}{
	{"Global", modeGlobal},
	{"Bypass CN whitelist", modeCNWhitelist},
	{"Bypass GFW blacklist", modeGFWBlacklist},
}

func routingLabel(mode string) string {
	for _, o := range routingOpts {
		if o.mode == mode {
			return o.label
		}
	}
	return routingOpts[0].label
}

func routingModeFor(label string) string {
	for _, o := range routingOpts {
		if o.label == label {
			return o.mode
		}
	}
	return modeGlobal
}

func routingLabels() []string {
	out := make([]string, len(routingOpts))
	for i, o := range routingOpts {
		out[i] = o.label
	}
	return out
}

func main() {
	// Single instance: a second launch surfaces the running window and exits,
	// so the app can run in the background (even with the tray icon hidden).
	lock, primary := acquireSingleton()
	if !primary {
		return
	}
	defer lock.Close()

	a := app.NewWithID("icu.wwr.badproxy.gui")
	a.Settings().SetTheme(newTheme())

	dir, err := appConfigDir()
	if err != nil {
		log.Fatalf("config dir: %v", err)
	}
	c := &controller{fyneApp: a, win: a.NewWindow("Bad Proxy"), store: newStore(dir), eng: engine{}}
	c.state = c.store.load()
	c.buildUI()
	c.setupTray()
	// Start as a foreground app (Dock icon + activated) so the initial window
	// surfaces normally; closing hides to the menu-bar item (becomes accessory).
	becomeForeground(c.win.Title())
	c.setStatus("Not connected", colMuted)
	if c.state.AutoConnect {
		if _, err := c.state.activeNode(); err == nil {
			c.connect()
		}
	}
	go c.watchReopen(lock)
	c.win.ShowAndRun()
}

// watchReopen accepts loopback pings from later launches and brings the window
// back on the UI thread.
func (c *controller) watchReopen(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
		fyne.Do(c.showWindow)
	}
}

func (c *controller) showWindow() {
	// Show first, THEN bring the app forward. becomeForeground orders the main
	// window (matched by title) key+front, which only works once that window is
	// actually in AppKit's window list — calling it before Show found nothing and
	// left the window hidden. No-op on Windows.
	c.win.Show()
	c.win.RequestFocus()
	becomeForeground(c.win.Title())
}

// hideToTray hides the window (proxy keeps running) and, on macOS, drops back to
// an accessory app with no Dock icon.
func (c *controller) hideToTray() {
	c.win.Hide()
	becomeBackground()
}

// sectionLabel is a flat, muted bold caption used to group controls without the
// rounded "card" chrome — keeps the surface calm and single-tone.
func sectionLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.TextStyle = fyne.TextStyle{Bold: true}
	l.Importance = widget.LowImportance
	return l
}

func (c *controller) buildUI() {
	// status line (no brand, no colored dot)
	c.statusText = widget.NewLabel("")
	c.statusText.Importance = widget.LowImportance

	// node list — each row carries its own (muted) Edit / Delete actions
	c.nodeList = widget.NewList(
		func() int { return len(c.state.Nodes) },
		func() fyne.CanvasObject {
			name := widget.NewLabel("name")
			name.Truncation = fyne.TextTruncateEllipsis
			name.TextStyle = fyne.TextStyle{Bold: true}
			sub := widget.NewLabel("detail")
			sub.Truncation = fyne.TextTruncateEllipsis
			sub.Importance = widget.MediumImportance
			texts := container.NewVBox(name, sub)
			edit := widget.NewButtonWithIcon("", theme.SettingsIcon(), nil)
			edit.Importance = widget.MediumImportance
			del := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			del.Importance = widget.MediumImportance
			actions := container.NewHBox(edit, del)
			return container.NewBorder(nil, nil, nil, actions, texts)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(c.state.Nodes) {
				return
			}
			n := c.state.Nodes[id]
			row := obj.(*fyne.Container)
			texts := row.Objects[0].(*fyne.Container)
			texts.Objects[0].(*widget.Label).SetText(n.Name)
			texts.Objects[1].(*widget.Label).SetText(nodeDetail(n))
			actions := row.Objects[1].(*fyne.Container)
			actions.Objects[0].(*widget.Button).OnTapped = func() {
				if nn, ok := c.nodeByID(id); ok {
					c.openNodeForm(&nn)
				}
			}
			actions.Objects[1].(*widget.Button).OnTapped = func() { c.confirmDelete(id) }
		},
	)
	c.nodeList.OnSelected = func(id widget.ListItemID) {
		c.state.Active = id
		c.persist()
	}
	if c.state.Active >= 0 && c.state.Active < len(c.state.Nodes) {
		c.nodeList.Select(c.state.Active)
	}

	addBtn := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() { c.openNodeForm(nil) })
	addBtn.Importance = widget.MediumImportance
	nodesHeader := container.NewBorder(nil, nil, nil, addBtn, sectionLabel("Nodes"))
	left := container.NewBorder(nodesHeader, nil, nil, nil, c.nodeList)

	// control panel
	c.primary = widget.NewButton("Connect", c.onPrimary)
	c.primary.Importance = widget.HighImportance
	c.testBtn = widget.NewButton("Test", c.onTest)
	c.testBtn.Importance = widget.MediumImportance
	c.socksPort = widget.NewEntry()
	c.socksPort.SetText(c.state.SocksPort)
	c.httpPort = widget.NewEntry()
	c.httpPort.SetText(c.state.HttpPort)

	c.routing = widget.NewSelect(routingLabels(), func(sel string) {
		c.state.RoutingMode = routingModeFor(sel)
		c.persist()
	})
	c.routing.SetSelected(routingLabel(c.state.RoutingMode))

	// system proxy is an action tied to connectivity, so it is a prominent
	// toggle button in the Connection group rather than a stray checkbox.
	c.sysProxy = widget.NewButton("", c.toggleSystemProxy)
	c.sysProxy.Importance = widget.HighImportance
	c.refreshSystemProxy()

	c.trayCheck = widget.NewCheck("Show tray icon", func(on bool) {
		c.state.HideTray = !on
		c.persist()
		dialog.ShowInformation("Restart required",
			"The tray / menu-bar icon change applies the next time Bad Proxy starts.", c.win)
	})
	c.trayCheck.SetChecked(!c.state.HideTray)

	c.allowLAN = widget.NewCheck("Allow devices on the local network", func(on bool) {
		c.state.AllowLAN = on
		c.persist()
		if c.eng.running() {
			dialog.ShowInformation("Reconnect to apply",
				"Changing the bind address takes effect the next time you connect.", c.win)
		}
	})
	c.allowLAN.SetChecked(c.state.AllowLAN)
	c.autoConn = widget.NewCheck("Connect automatically on launch", func(on bool) {
		c.state.AutoConnect = on
		c.persist()
	})
	c.autoConn.SetChecked(c.state.AutoConnect)
	c.startLogin = widget.NewCheck("Start at login", func(on bool) {
		if err := setStartAtLogin(on); err != nil {
			dialog.ShowError(err, c.win)
			c.startLogin.SetChecked(!on)
			return
		}
		c.state.StartLogin = on
		c.persist()
	})
	c.startLogin.SetChecked(c.state.StartLogin)

	portForm := widget.NewForm(
		widget.NewFormItem("SOCKS port", c.socksPort),
		widget.NewFormItem("HTTP port", c.httpPort),
	)

	connectionItems := []fyne.CanvasObject{
		sectionLabel("Connection"),
		container.NewGridWithColumns(2, c.primary, c.testBtn),
		c.statusText,
	}
	if systemProxySupported {
		connectionItems = append(connectionItems, c.sysProxy)
	}
	controlItems := []fyne.CanvasObject{
		container.NewVBox(connectionItems...),
		widget.NewSeparator(),
		sectionLabel("Local proxy ports"),
		portForm,
	}
	var optionRows []fyne.CanvasObject
	optionRows = append(optionRows,
		container.NewBorder(nil, nil, nil, c.routing, sectionLabel("Routing")),
		c.allowLAN, c.autoConn)
	if startAtLoginSupported {
		optionRows = append(optionRows, c.startLogin)
	}
	if trayCanHide {
		optionRows = append(optionRows, c.trayCheck)
	}
	controlItems = append(controlItems, widget.NewSeparator(), sectionLabel("Options"))
	controlItems = append(controlItems, optionRows...)
	control := container.NewPadded(container.NewVBox(controlItems...))

	rightCol := container.New(&fixedWidth{w: 330, gap: 24}, control)
	body := container.NewBorder(nil, nil, nil, rightCol, left)

	c.win.SetContent(container.NewPadded(body))
	c.win.Resize(fyne.NewSize(820, 500))
	// Platform-specific: on macOS/Windows closing hides to the tray so the proxy
	// keeps running (macOS also drops to accessory / no Dock icon); elsewhere the
	// close falls through to Fyne's default quit.
	if hideOnCloseToTray {
		c.win.SetCloseIntercept(c.hideToTray)
	}
}

// fixedWidth pins its single child to a constant width and lets it fill the
// available height, so the right panel stays put instead of using a thin,
// hard-to-grab splitter. `gap` is reserved as left gutter between the columns.
type fixedWidth struct{ w, gap float32 }

func (l *fixedWidth) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	objects[0].Resize(fyne.NewSize(l.w-l.gap, size.Height))
	objects[0].Move(fyne.NewPos(l.gap, 0))
}

func (l *fixedWidth) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(l.w, 0)
}

func (c *controller) setupTray() {
	if !hasTray {
		return
	}
	// The menu-bar / tray icon is only hidden where it's safe to: on Windows a
	// relaunch pokes the running instance back. On macOS there is no Dock, so the
	// icon must stay to remain reachable.
	if trayCanHide && c.state.HideTray {
		return
	}
	da, ok := c.fyneApp.Driver().(desktop.App)
	if !ok {
		return
	}
	// On Windows and macOS let the tray / menu-bar icon left-tap re-show the window.
	// macOS needs a custom tap handler (see customizeTrayTap) to activate the app and
	// keep close-to-tray; Windows uses Fyne's default as-is.
	if trayUsesNativeClickOpen {
		da.SetSystemTrayWindow(c.win)
		customizeTrayTap(c)
	}
	var items []*fyne.MenuItem
	if trayOpenMenuItem {
		items = append(items, fyne.NewMenuItem("Open Bad Proxy", c.showWindow))
	}
	if trayQuitMenuItem {
		if len(items) > 0 {
			items = append(items, fyne.NewMenuItemSeparator())
		}
		items = append(items, fyne.NewMenuItem("Quit", func() { c.shutdown() }))
	}
	if len(items) > 0 {
		da.SetSystemTrayMenu(fyne.NewMenu("", items...))
	}
}

func (c *controller) selectedNode() (Node, bool) {
	if c.state.Active < 0 || c.state.Active >= len(c.state.Nodes) {
		dialog.ShowInformation("No node", "Select or add a node first.", c.win)
		return Node{}, false
	}
	return c.state.Nodes[c.state.Active], true
}

// onPrimary toggles the engine from a single prominent button.
func (c *controller) onPrimary() {
	if c.eng.running() {
		c.disconnect()
		return
	}
	c.connect()
}

func (c *controller) connect() {
	if !c.applyPorts() {
		return
	}
	if _, ok := c.selectedNode(); !ok {
		return
	}
	c.setStatus("Connecting…", colAccent)
	if err := c.eng.start(c.state); err != nil {
		c.setStatus("Error: "+err.Error(), colErr)
		dialog.ShowError(err, c.win)
		return
	}
	c.setStatus(fmt.Sprintf("Connected · socks :%s · http :%s", c.state.SocksPort, c.state.HttpPort), colOK)
	if c.state.SystemProxy {
		if err := applySystemProxy(true, c.state); err != nil {
			dialog.ShowError(fmt.Errorf("connected, but the system proxy could not be set: %w", err), c.win)
		}
	}
	c.primary.SetText("Disconnect")
	c.primary.Importance = widget.DangerImportance
	c.primary.Refresh()
	c.persist()
}

func (c *controller) disconnect() {
	c.eng.stop()
	if c.state.SystemProxy {
		_ = applySystemProxy(false, c.state)
	}
	c.setStatus("Not connected", colMuted)
	c.primary.SetText("Connect")
	c.primary.Importance = widget.HighImportance
	c.primary.Refresh()
}

// refreshSystemProxy relabels the toggle button to reflect the desired state.
func (c *controller) refreshSystemProxy() {
	if c.state.SystemProxy {
		c.sysProxy.SetText("System proxy: on")
		c.sysProxy.Importance = widget.HighImportance
	} else {
		c.sysProxy.SetText("System proxy: off")
		c.sysProxy.Importance = widget.MediumImportance
	}
	c.sysProxy.Refresh()
}

// toggleSystemProxy flips the desired system-proxy state. It is applied live
// only while connected; setting it while disconnected just records the intent so
// the OS is never pointed at a dead port.
func (c *controller) toggleSystemProxy() {
	on := !c.state.SystemProxy
	if c.eng.running() {
		if err := applySystemProxy(on, c.state); err != nil {
			dialog.ShowError(err, c.win)
			return
		}
	}
	c.state.SystemProxy = on
	c.refreshSystemProxy()
	c.persist()
}

func (c *controller) onTest() {
	if !c.eng.running() {
		dialog.ShowInformation("Not connected", "Connect first, then test.", c.win)
		return
	}
	c.setStatus("Testing…", colAccent)
	go func() {
		latency, err := c.state.dialThroughProxy()
		if err != nil {
			c.setStatus("Test failed: "+err.Error(), colErr)
			return
		}
		c.setStatus(fmt.Sprintf("Connected · %s · socks :%s · http :%s",
			latency.Round(time.Millisecond), c.state.SocksPort, c.state.HttpPort), colOK)
	}()
}

func (c *controller) nodeByID(id int) (Node, bool) {
	if id < 0 || id >= len(c.state.Nodes) {
		return Node{}, false
	}
	return c.state.Nodes[id], true
}

// confirmDelete pops a lightweight yes/no before removing the node at id.
func (c *controller) confirmDelete(id int) {
	n, ok := c.nodeByID(id)
	if !ok {
		return
	}
	if c.eng.running() && id == c.state.Active {
		dialog.ShowConfirm("Disconnect first?",
			"Delete "+n.Name+"? The current connection will be stopped.", func(yes bool) {
				if yes {
					c.disconnect()
					c.deleteNodeAt(id)
				}
			}, c.win)
		return
	}
	dialog.ShowConfirm("Delete node?", "Remove "+n.Name+"?", func(yes bool) {
		if yes {
			c.deleteNodeAt(id)
		}
	}, c.win)
}

func (c *controller) deleteNodeAt(id int) {
	if id < 0 || id >= len(c.state.Nodes) {
		return
	}
	if c.eng.running() {
		c.disconnect()
	}
	c.state.Nodes = append(c.state.Nodes[:id:id], c.state.Nodes[id+1:]...)
	c.state.normalize()
	c.nodeList.UnselectAll()
	c.nodeList.Refresh()
	if c.state.Active >= 0 {
		c.nodeList.Select(c.state.Active)
	}
	c.persist()
}

// openNodeForm shows the add/edit dialog. edit is nil for a new node. The form
// only renders the fields the core engine actually reads for the current
// protocol+transport, so changing either rebuilds the dialog (the same widget
// instances are reused, so typed values survive).
func (c *controller) openNodeForm(edit *Node) {
	proto := widget.NewSelect([]string{"btp", "vless", "trojan", "socks"}, nil)
	transport := widget.NewSelect([]string{"tcp", "tls", "xhttp"}, nil)
	name := widget.NewEntry()
	server := widget.NewEntry()
	port := widget.NewEntry()
	secret := widget.NewEntry()
	path := widget.NewEntry()
	authority := widget.NewEntry()
	padding := widget.NewEntry()
	tls := widget.NewCheck("", nil)

	if edit != nil {
		proto.SetSelected(edit.Protocol)
		transport.SetSelected(edit.Transport)
		name.SetText(edit.Name)
		server.SetText(edit.Server)
		port.SetText(edit.Port)
		secret.SetText(edit.Secret)
		path.SetText(edit.Path)
		authority.SetText(edit.Authority)
		padding.SetText(edit.Padding)
		tls.SetChecked(edit.TLS)
	} else {
		proto.SetSelected("vless")
		transport.SetSelected("xhttp")
	}

	// save is shared by every rebuild; it reads from the persistent widgets.
	save := func(ok bool) {
		if !ok {
			return
		}
		f := visibleNodeFields(proto.Selected, transport.Selected)
		n := Node{
			Name: name.Text, Protocol: proto.Selected, Server: server.Text, Port: port.Text,
			Transport: transport.Selected,
		}
		if f.Secret {
			n.Secret = secret.Text
		}
		if f.Authority {
			n.Authority = authority.Text
		}
		if f.Path {
			n.Path = path.Text
		}
		if f.Padding {
			n.Padding = padding.Text
		}
		if f.HTTPS {
			n.TLS = tls.Checked
		}
		if n.Name == "" || n.Server == "" || n.Port == "" {
			dialog.ShowInformation("Missing details",
				"Name, server and port are required.", c.win)
			return
		}
		if edit != nil {
			c.state.Nodes = replaceNode(c.state.Nodes, edit.Name, n)
		} else {
			c.state.Nodes = append(c.state.Nodes, n)
		}
		c.state.Active = indexOfNode(c.state.Nodes, n.Name)
		c.nodeList.Refresh()
		if c.state.Active >= 0 {
			c.nodeList.Select(c.state.Active)
		}
		c.persist()
	}

	var current *dialog.FormDialog
	ready := false

	build := func() {
		if current != nil {
			current.Hide()
		}
		f := visibleNodeFields(proto.Selected, transport.Selected)
		items := []*widget.FormItem{
			widget.NewFormItem("Name", name),
			widget.NewFormItem("Protocol", proto),
			widget.NewFormItem("Server", server),
			widget.NewFormItem("Port", port),
		}
		if f.Secret {
			items = append(items, widget.NewFormItem("Secret/UUID", secret))
		}
		items = append(items, widget.NewFormItem("Transport", transport))
		if f.Authority {
			items = append(items, widget.NewFormItem("Authority/SNI", authority))
		}
		if f.Path {
			items = append(items, widget.NewFormItem("Path", path))
		}
		if f.Padding {
			items = append(items, widget.NewFormItem("Padding", padding))
		}
		if f.HTTPS {
			items = append(items, widget.NewFormItem("HTTPS", tls))
		}
		current = dialog.NewForm("Node", "Save", "Cancel", items, save, c.win)
		current.Resize(fyne.NewSize(440, 520))
		current.Show()
	}

	build()
	// Changing protocol or transport rebuilds the visible field set. Attach the
	// handlers only after the first show so the initial SetSelected above does
	// not trigger a rebuild.
	proto.OnChanged = func(string) {
		if ready {
			build()
		}
	}
	transport.OnChanged = func(string) {
		if ready {
			build()
		}
	}
	ready = true
}

func (c *controller) applyPorts() bool {
	if !validPort(c.socksPort.Text) || !validPort(c.httpPort.Text) {
		c.setStatus("Ports must be 1-65535", colErr)
		return false
	}
	c.state.SocksPort = c.socksPort.Text
	c.state.HttpPort = c.httpPort.Text
	return true
}

func (c *controller) setStatus(text string, _ color.NRGBA) {
	c.statusText.SetText(text)
}

func (c *controller) persist() {
	if err := c.store.save(c.state); err != nil {
		log.Printf("persist state: %v", err)
	}
}

func (c *controller) shutdown() {
	if c.eng.running() {
		c.eng.stop()
	}
	if c.state.SystemProxy {
		_ = applySystemProxy(false, c.state)
	}
	c.fyneApp.Quit()
}
