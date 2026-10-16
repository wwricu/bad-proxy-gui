package main

import (
	"fmt"
	"image/color"
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type controller struct {
	fyneApp fyne.App
	win     fyne.Window
	store   *store
	eng     engine
	state   GUIState

	statusDot  *canvas.Circle
	statusText *widget.Label
	nodeList   *widget.List
	primary    *widget.Button
	testBtn    *widget.Button
	socksPort  *widget.Entry
	httpPort   *widget.Entry
	sysProxy   *widget.Check
}

func main() {
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
	c.setStatus("Not connected", colMuted)
	c.win.ShowAndRun()
}

func (c *controller) buildUI() {
	// header: brand dot + title + live status
	c.statusDot = canvas.NewCircle(colMuted)
	c.statusDot.StrokeColor = colBorder
	c.statusDot.StrokeWidth = 1
	title := widget.NewLabel("Bad Proxy")
	title.Importance = widget.HighImportance
	title.TextStyle = fyne.TextStyle{Bold: true}
	c.statusText = widget.NewLabel("")
	c.statusText.Importance = widget.LowImportance
	header := container.NewHBox(
		container.NewGridWrap(fyne.NewSize(12, 12), c.statusDot),
		title,
		layout.NewSpacer(),
		c.statusText,
	)

	// node list
	c.nodeList = widget.NewList(
		func() int { return len(c.state.Nodes) },
		func() fyne.CanvasObject {
			name := widget.NewLabel("name")
			name.Truncation = fyne.TextTruncateEllipsis
			name.TextStyle = fyne.TextStyle{Bold: true}
			sub := widget.NewLabel("detail")
			sub.Truncation = fyne.TextTruncateEllipsis
			sub.Importance = widget.MediumImportance
			return container.NewVBox(name, sub)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(c.state.Nodes) {
				return
			}
			n := c.state.Nodes[id]
			box := obj.(*fyne.Container)
			box.Objects[0].(*widget.Label).SetText(n.Name)
			box.Objects[1].(*widget.Label).SetText(nodeDetail(n))
		},
	)
	c.nodeList.OnSelected = func(id widget.ListItemID) {
		c.state.Active = id
		c.persist()
	}
	if c.state.Active >= 0 && c.state.Active < len(c.state.Nodes) {
		c.nodeList.Select(c.state.Active)
	}

	addBtn := widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), func() { c.openNodeForm(nil) })
	editBtn := widget.NewButtonWithIcon("Edit", theme.DocumentSaveIcon(), func() {
		if n, ok := c.selectedNode(); ok {
			c.openNodeForm(&n)
		}
	})
	delBtn := widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), c.onDeleteNode)
	nodeActions := container.NewHBox(addBtn, editBtn, layout.NewSpacer(), delBtn)
	nodesCard := widget.NewCard("", "", container.NewBorder(nil, nodeActions, nil, nil, c.nodeList))

	// control panel
	c.primary = widget.NewButton("Connect", c.onPrimary)
	c.primary.Importance = widget.HighImportance
	c.testBtn = widget.NewButton("Test", c.onTest)
	c.socksPort = widget.NewEntry()
	c.socksPort.SetText(c.state.SocksPort)
	c.httpPort = widget.NewEntry()
	c.httpPort.SetText(c.state.HttpPort)
	c.sysProxy = widget.NewCheck("Route system traffic through proxy", func(on bool) {
		c.state.SystemProxy = on
		if err := applySystemProxy(on, c.state); err != nil {
			dialog.ShowError(err, c.win)
			c.sysProxy.SetChecked(false)
			c.state.SystemProxy = false
		}
		c.persist()
	})
	c.sysProxy.SetChecked(c.state.SystemProxy)

	portForm := widget.NewForm(
		widget.NewFormItem("SOCKS", c.socksPort),
		widget.NewFormItem("HTTP", c.httpPort),
	)
	controlChildren := []fyne.CanvasObject{
		container.NewGridWithColumns(2, c.primary, c.testBtn),
		widget.NewSeparator(),
		portForm,
	}
	if systemProxySupported {
		controlChildren = append(controlChildren, widget.NewSeparator(), c.sysProxy)
	}
	control := container.NewVBox(controlChildren...)
	controlCard := widget.NewCard("", "", control)

	body := container.NewHSplit(nodesCard, controlCard)
	body.Offset = 0.6

	c.win.SetContent(container.NewPadded(
		container.NewBorder(header, nil, nil, nil, body)))
	c.win.Resize(fyne.NewSize(720, 440))
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
		c.setStatus("Error: " + err.Error(), colErr)
		dialog.ShowError(err, c.win)
		return
	}
	c.setStatus(fmt.Sprintf("Connected · socks :%s · http :%s", c.state.SocksPort, c.state.HttpPort), colOK)
	c.primary.SetText("Disconnect")
	c.primary.Importance = widget.DangerImportance
	c.primary.Refresh()
	c.persist()
}

func (c *controller) disconnect() {
	c.eng.stop()
	c.setStatus("Not connected", colMuted)
	c.primary.SetText("Connect")
	c.primary.Importance = widget.HighImportance
	c.primary.Refresh()
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

func (c *controller) onDeleteNode() {
	node, ok := c.selectedNode()
	if !ok {
		return
	}
	if c.eng.running() {
		c.disconnect()
	}
	c.state.Nodes = removeNode(c.state.Nodes, node.Name)
	c.state.Active = -1
	c.nodeList.UnselectAll()
	c.nodeList.Refresh()
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
		widget.NewFormItem("HTTPS", tls),
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
		c.state.Active = indexOfNode(c.state.Nodes, n.Name)
		c.nodeList.Refresh()
		if c.state.Active >= 0 {
			c.nodeList.Select(c.state.Active)
		}
		c.persist()
	}, c.win)
	d.Resize(fyne.NewSize(440, 520))
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

func (c *controller) setStatus(text string, dotColor color.NRGBA) {
	c.statusDot.FillColor = dotColor
	c.statusDot.Refresh()
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
