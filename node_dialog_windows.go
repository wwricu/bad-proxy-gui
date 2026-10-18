//go:build windows

package main

// nodeDialogSupported tells the frontend to open the real OS editor window
// instead of the in-page HTML modal.
const nodeDialogSupported = true

// Control ids for the addressable node widgets. Save and Cancel are the shared
// IDOK/IDCANCEL.
const (
	ndIDName = 101 + iota
	ndIDProto
	ndIDServer
	ndIDPort
	ndIDSecret
	ndIDTransport
	ndIDAuthority
	ndIDPath
	ndIDPadding
)

var (
	ndProtocols  = []string{"vless", "btp", "trojan", "socks"}
	ndTransports = []string{"tls", "xhttp"}
)

// nodeForm is the node editor: the shared form machinery plus the one Node it
// reads and writes.
type nodeForm struct {
	f   *ndForm
	in  Node
	out Node
}

// runNodeDialog opens the native editor and returns the edited node and
// whether the user confirmed.
func runNodeDialog(in Node) (Node, bool) {
	nf := &nodeForm{in: in, out: in}
	nf.f = &ndForm{
		title:   nodeDialogTitle(in),
		first:   ndIDName,
		inputs:  make(map[int]uintptr),
		vis:     make(map[int]bool),
		labelID: make(map[uintptr]int),
		build:   nf.build,
		onOK:    nf.confirm,
		onCmd:   nf.command,
	}
	nf.markVisibility(in.Protocol, in.Transport)
	if !runForm(nf.f) {
		return in, false
	}
	return nf.out, true
}

// nodeDialogTitle is the native caption. A native window has a caption, so the
// editor does not repeat the action as an in-card heading - the HTML modal
// dropped that too, and the two now show the form and nothing else.
func nodeDialogTitle(n Node) string {
	if nodeIsNew(n) {
		return "Bad Proxy · Add Node"
	}
	return "Bad Proxy · Edit Node"
}

func nodeIsNew(n Node) bool {
	return n.Name == "" && n.Server == "" && n.Port == ""
}

// build creates every control once; layout() then positions the visible ones.
func (nf *nodeForm) build() {
	f := nf.f
	// The card title is the window caption itself ("Bad Proxy · Edit Node"), so
	// there is no in-card "Edit Node" heading to duplicate it.

	f.addRow("Name", f.editRow(ndIDName), ndIDName)
	f.setTxt(ndIDName, nf.in.Name)

	f.addComboRow("Protocol", ndIDProto, ndProtocols, nf.in.Protocol)
	f.addRow("Address", f.editRow(ndIDServer), ndIDServer)
	f.setTxt(ndIDServer, nf.in.Server)
	f.addRow("Port", f.editRow(ndIDPort), ndIDPort)
	f.setTxt(ndIDPort, nf.in.Port)
	f.addRow("Secret", f.editRow(ndIDSecret), ndIDSecret)
	f.setTxt(ndIDSecret, nf.in.Secret)
	f.addComboRow("Transport", ndIDTransport, ndTransports, nf.in.Transport)
	f.addRow("Domain (SNI/Host)", f.editRow(ndIDAuthority), ndIDAuthority)
	f.setTxt(ndIDAuthority, nf.in.Authority)
	f.addRow("Path", f.editRow(ndIDPath), ndIDPath)
	f.setTxt(ndIDPath, nf.in.Path)
	f.addRow("Padding range", f.editRow(ndIDPadding), ndIDPadding)
	f.setTxt(ndIDPadding, nf.in.Padding)

	f.addActionButtons("Save", "Cancel")
	f.layout()
	f.focusFirst()
}

// markVisibility records which optional rows show, using the shared
// visibleNodeFields rule. It is the single source of truth both before and
// after the rows exist, so the native editor and the HTML modal hide the same
// fields for the same protocol/transport pair.
func (nf *nodeForm) markVisibility(proto, trans string) {
	vf := visibleNodeFields(proto, trans)
	nf.f.vis[ndIDSecret] = vf.Secret
	nf.f.vis[ndIDAuthority] = vf.Authority
	nf.f.vis[ndIDPath] = vf.Path
	nf.f.vis[ndIDPadding] = vf.Padding
	for _, r := range nf.f.rows {
		r.visible = nf.f.visFor(r.visID)
	}
}

func (nf *nodeForm) command(notify, id int) {
	if (id == ndIDProto || id == ndIDTransport) && notify == ndCBNSel {
		nf.markVisibility(nf.f.comboSel(ndIDProto, ndProtocols),
			nf.f.comboSel(ndIDTransport, ndTransports))
		nf.f.layout()
	}
}

// confirm reads the widgets back into a Node. Returning false keeps the window
// open, so a validation message does not also throw the user's edits away.
func (nf *nodeForm) confirm() bool {
	f := nf.f
	p, t := f.comboSel(ndIDProto, ndProtocols), f.comboSel(ndIDTransport, ndTransports)
	vf := visibleNodeFields(p, t)
	n := Node{
		Name:      f.getTrim(ndIDName),
		Protocol:  p,
		Server:    f.getTrim(ndIDServer),
		Port:      f.getTrim(ndIDPort),
		Transport: t,
	}
	if vf.Secret {
		n.Secret = f.getTrim(ndIDSecret)
	}
	if vf.Authority {
		n.Authority = f.getTrim(ndIDAuthority)
	}
	if vf.Path {
		n.Path = f.getTrim(ndIDPath)
	}
	if vf.Padding {
		n.Padding = f.getTrim(ndIDPadding)
	}
	if n.Name == "" || n.Server == "" || n.Port == "" {
		showMessageBox("Bad Proxy", "Name, address and port are required", false)
		return false
	}
	nf.out = n
	return true
}
