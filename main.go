package main

import (
	"fmt"
	"image/color"
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type controller struct {
	fyneApp fyne.App
	win     fyne.Window
	store   *store
	eng     engine
	state   GUIState

	status    *widget.Label
	socksPort *widget.Entry
	httpPort  *widget.Entry
	nodeRadio *widget.RadioGroup
	nodeBox   *fyne.Container
	sysProxy  *widget.Check
	connect   *widget.Button
	disconnect *widget.Button
	testBtn   *widget.Button
}

func main() {
	a := app.NewWithID("icu.wwr.badproxy.gui")
	dir, err := appConfigDir()
	if err != nil {
		log.Fatalf("config dir: %v", err)
	}
	c := &controller{fyneApp: a, win: a.NewWindow("Bad Proxy"), store: newStore(dir), eng: engine{}}
	c.state = c.store.load()
	c.buildUI()
	c.setupTray()
	c.win.ShowAndRun()
}

// newRadio builds the single-active-node selector from the current state. The
// node list changes shape (add/delete), and Fyne's RadioGroup renderer caches
// its items, so we rebuild and swap the widget rather than mutating Options.
func (c *controller) newRadio() *widget.RadioGroup {
	ng := widget.NewRadioGroup(nodeNames(c.state.Nodes), func(selected string) {
		c.state.Active = indexOfNode(c.state.Nodes, selected)
		c.persist()
	})
	if c.state.Active >= 0 && c.state.Active < len(c.state.Nodes) {
		ng.SetSelected(c.state.Nodes[c.state.Active].Name)
	}
	return ng
}

// refreshNodes swaps in a freshly-built radio group reflecting current nodes.
// A VBox lays out straight from its Objects slice, so replacing the single
// child and refreshing is enough (RadioGroup caches its item renderer).
func (c *controller) refreshNodes() {
	fresh := c.newRadio()
	c.nodeRadio = fresh
	c.nodeBox.Objects = []fyne.CanvasObject{fresh}
	c.nodeBox.Refresh()
}

func (c *controller) buildUI() {
	c.status = widget.NewLabel("status: stopped")
	c.socksPort = widget.NewEntry()
	c.socksPort.SetText(c.state.SocksPort)
	c.httpPort = widget.NewEntry()
	c.httpPort.SetText(c.state.HttpPort)

	c.nodeRadio = c.newRadio()

	c.sysProxy = widget.NewCheck("Set as system proxy", func(on bool) {
		c.state.SystemProxy = on
		if err := applySystemProxy(on, c.state); err != nil {
			dialog.ShowError(err, c.win)
			c.sysProxy.SetChecked(false)
			c.state.SystemProxy = false
		}
		c.persist()
	})
	c.sysProxy.SetChecked(c.state.SystemProxy)

	c.connect = widget.NewButton("Connect", c.onConnect)
	c.disconnect = widget.NewButton("Disconnect", c.onDisconnect)
	c.testBtn = widget.NewButton("Test", c.onTest)
	c.disconnect.Disable()

	portForm := widget.NewForm(
		widget.NewFormItem("SOCKS port", c.socksPort),
		widget.NewFormItem("HTTP port", c.httpPort),
	)

	nodeBar := container.NewHBox(
		widget.NewButton("Add node", func() { c.openNodeForm(nil) }),
		widget.NewButton("Edit", func() {
			if n, ok := c.selectedNode(); ok {
				c.openNodeForm(&n)
			}
		}),
		widget.NewButton("Delete", c.onDeleteNode),
	)

	c.nodeBox = container.NewVBox(c.nodeRadio)
	left := container.NewBorder(nodeBar, nil, nil, nil, c.nodeBox)
	right := container.NewVBox(
		widget.NewLabel("Local listeners"),
		portForm,
		c.sysProxy,
		container.NewHBox(c.connect, c.disconnect, c.testBtn),
		c.status,
	)

	c.win.SetContent(container.NewBorder(
		widget.NewLabel("Bad Proxy"),
		nil, nil, nil,
		container.NewHBox(left, layout.NewSpacer(), right),
	))
	c.win.Resize(fyne.NewSize(680, 420))
}

func (c *controller) setupTray() {
	da, ok := c.fyneApp.Driver().(desktop.App)
	if !ok {
		return
	}
	show := fyne.NewMenuItem("Show", func() { c.win.Show(); c.win.RequestFocus() })
	quit := fyne.NewMenuItem("Quit", func() { c.shutdown() })
	da.SetSystemTrayMenu(fyne.NewMenu("", show, fyne.NewMenuItemSeparator(), quit))
}

func (c *controller) selectedNode() (Node, bool) {
	if c.state.Active < 0 || c.state.Active >= len(c.state.Nodes) {
		dialog.ShowInformation("No node", "Select or add a node first.", c.win)
		return Node{}, false
	}
	return c.state.Nodes[c.state.Active], true
}

func (c *controller) onConnect() {
	if !c.applyPorts() {
		return
	}
	if _, ok := c.selectedNode(); !ok {
		return
	}
	if err := c.eng.start(c.state); err != nil {
		c.setStatus("error: " + err.Error(), color.NRGBA{0xb0, 0x30, 0x30, 0xff})
		dialog.ShowError(err, c.win)
		return
	}
	c.setStatus(fmt.Sprintf("connected (socks :%s, http :%s)", c.state.SocksPort, c.state.HttpPort),
		color.NRGBA{0x2e, 0x8b, 0x57, 0xff})
	c.connect.Disable()
	c.disconnect.Enable()
	c.persist()
}

func (c *controller) onDisconnect() {
	c.eng.stop()
	c.setStatus("status: stopped", color.NRGBA{0x80, 0x80, 0x80, 0xff})
	c.connect.Enable()
	c.disconnect.Disable()
}

func (c *controller) onTest() {
	if !c.eng.running() {
		dialog.ShowInformation("Not connected", "Connect first, then test.", c.win)
		return
	}
	c.setStatus("testing...", color.NRGBA{0x80, 0x80, 0x80, 0xff})
	go func() {
		latency, err := c.state.dialThroughProxy()
		if err != nil {
			c.setStatus("test failed: "+err.Error(), color.NRGBA{0xb0, 0x30, 0x30, 0xff})
			return
		}
		c.setStatus(fmt.Sprintf("test ok: %s round-trip", latency.Round(time.Millisecond)),
			color.NRGBA{0x2e, 0x8b, 0x57, 0xff})
	}()
}

func (c *controller) onDeleteNode() {
	node, ok := c.selectedNode()
	if !ok {
		return
	}
	// disconnect if we remove the node currently carrying traffic
	if c.eng.running() {
		c.eng.stop()
		c.setStatus("status: stopped", color.NRGBA{0x80, 0x80, 0x80, 0xff})
		c.connect.Enable()
		c.disconnect.Disable()
	}
	c.state.Nodes = removeNode(c.state.Nodes, node.Name)
	c.state.Active = -1
	c.refreshNodes()
	c.persist()
}

// openNodeForm shows the add/edit dialog. edit is nil for a new node.
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
	tls := widget.NewCheck("HTTPS (xhttp over TLS)", nil)

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

	items := []*widget.FormItem{
		widget.NewFormItem("Name", name),
		widget.NewFormItem("Protocol", proto),
		widget.NewFormItem("Server", server),
		widget.NewFormItem("Port", port),
		widget.NewFormItem("Secret/UUID", secret),
		widget.NewFormItem("Transport", transport),
		widget.NewFormItem("Path", path),
		widget.NewFormItem("Authority/SNI", authority),
		widget.NewFormItem("Padding", padding),
		widget.NewFormItem("TLS", tls),
	}

	d := dialog.NewForm("Node", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		n := Node{
			Name: name.Text, Protocol: proto.Selected, Server: server.Text, Port: port.Text,
			Secret: secret.Text, Transport: transport.Selected, Path: path.Text,
			Authority: authority.Text, TLS: tls.Checked, Padding: padding.Text,
		}
		if n.Name == "" || n.Server == "" || n.Port == "" {
			dialog.ShowError(fmt.Errorf("name, server and port are required"), c.win)
			return
		}
		if edit != nil {
			c.state.Nodes = replaceNode(c.state.Nodes, edit.Name, n)
		} else {
			c.state.Nodes = append(c.state.Nodes, n)
		}
		// keep active pointing at the same node across rename
		c.state.Active = indexOfNode(c.state.Nodes, n.Name)
		c.refreshNodes()
		c.persist()
	}, c.win)
	d.Resize(fyne.NewSize(420, 480))
	d.Show()
}

func (c *controller) applyPorts() bool {
	if !validPort(c.socksPort.Text) || !validPort(c.httpPort.Text) {
		dialog.ShowError(fmt.Errorf("ports must be 1-65535"), c.win)
		return false
	}
	c.state.SocksPort = c.socksPort.Text
	c.state.HttpPort = c.httpPort.Text
	return true
}

func (c *controller) setStatus(text string, col color.NRGBA) {
	c.status.SetText(text)
	c.status.Truncation = fyne.TextTruncateEllipsis
	_ = col // fyne label colour styling is theme-wide; kept for future theming
}

func (c *controller) persist() {
	if err := c.store.save(c.state); err != nil {
		log.Printf("persist state: %v", err)
	}
}

// shutdown tears down the engine and system proxy before the app exits.
func (c *controller) shutdown() {
	if c.eng.running() {
		c.eng.stop()
	}
	if c.state.SystemProxy {
		_ = applySystemProxy(false, c.state)
	}
	c.fyneApp.Quit()
}
