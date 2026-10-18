//go:build windows

package main

import "github.com/wwricu/bad-proxy-gui/tunplat"

// settingsDialogSupported tells the frontend to open the real OS settings
// window instead of the in-page HTML card.
const settingsDialogSupported = true

// Control ids for the settings widgets. OK and Cancel are the shared
// IDOK/IDCANCEL.
const (
	sdIDHTTP = 201 + iota
	sdIDSOCKS
	sdIDMode
	sdIDRouting
	sdIDTun
	sdIDLAN
	sdIDLogin
	sdIDTray
)

// The dropdowns show the same words the HTML card shows, and the selection is
// read back by position: the label list and the value list stay index-aligned.
var (
	sdModeLabels  = []string{"Direct", "Proxy", "System proxy"}
	sdModes       = []string{modeDirect, modeProxy, modeSystem}
	sdRouteLabels = []string{"Global", "Bypass CN", "Bypass GFW"}
	sdRoutes      = []string{modeGlobal, modeCNWhitelist, modeGFWBlacklist}
)

// settingsForm is the settings window: the shared form machinery plus the one
// Settings draft it reads and writes.
type settingsForm struct {
	f   *ndForm
	in  Settings
	out Settings
}

// runSettingsDialog opens the native window and returns the draft and whether
// the user confirmed.
func runSettingsDialog(in Settings) (Settings, bool) {
	sf := &settingsForm{in: in, out: in}
	sf.f = &ndForm{
		title:   "Bad Proxy · Settings",
		first:   sdIDHTTP,
		inputs:  make(map[int]uintptr),
		vis:     make(map[int]bool),
		labelID: make(map[uintptr]int),
		build:   sf.build,
		onOK:    sf.confirm,
		onCmd:   sf.command,
	}
	if !runForm(sf.f) {
		return in, false
	}
	return sf.out, true
}

// build lays the draft out. Every capability these rows drive is present on
// Windows, so only the tunnel needs a check before it gets a box.
func (sf *settingsForm) build() {
	f := sf.f
	f.addRow("HTTP port", f.editRow(sdIDHTTP), sdIDHTTP)
	f.setTxt(sdIDHTTP, sf.in.HttpPort)
	f.addRow("SOCKS port", f.editRow(sdIDSOCKS), sdIDSOCKS)
	f.setTxt(sdIDSOCKS, sf.in.SocksPort)
	f.addComboRow("Mode", sdIDMode, sdModeLabels,
		sdModeLabels[comboIndexOf(sdModes, sf.in.ProxyMode)])
	f.addComboRow("Routing", sdIDRouting, sdRouteLabels,
		sdRouteLabels[comboIndexOf(sdRoutes, sf.in.RoutingMode)])
	if tunplat.Supported() {
		f.addCheckRow("TUN mode (takes over system traffic)", sdIDTun, sf.in.TunEnabled)
	}
	f.addCheckRow("Allow LAN access", sdIDLAN, sf.in.AllowLAN)
	f.addCheckRow("Start at login", sdIDLogin, sf.in.StartLogin)
	f.addCheckRow("Hide tray icon (after restart)", sdIDTray, sf.in.HideTray)

	f.addActionButtons("OK", "Cancel")
	f.layout()
	sf.syncTun()
	f.focusFirst()
}

// syncTun keeps the tunnel honest about the mode: direct has no node for it to
// carry traffic through, so the box comes unticked and unusable until a proxy
// mode is selected. The HTML card does the same.
func (sf *settingsForm) syncTun() {
	h, ok := sf.f.inputs[sdIDTun]
	if !ok {
		return
	}
	direct := sf.f.comboSel(sdIDMode, sdModes) == modeDirect
	if direct {
		ndSendMsg.Call(h, ndBMSet, 0, 0)
		ndEnableWnd.Call(h, 0)
		return
	}
	ndEnableWnd.Call(h, 1)
}

func (sf *settingsForm) command(notify, id int) {
	if id == sdIDMode && notify == ndCBNSel {
		sf.syncTun()
	}
}

// confirm reads the widgets back into a draft. Returning false keeps the window
// open so a validation message does not also throw the user's edits away.
func (sf *settingsForm) confirm() bool {
	f := sf.f
	s := Settings{
		HttpPort:    f.getTrim(sdIDHTTP),
		SocksPort:   f.getTrim(sdIDSOCKS),
		ProxyMode:   sdModes[f.comboIndex(sdIDMode, len(sdModes))],
		RoutingMode: sdRoutes[f.comboIndex(sdIDRouting, len(sdRoutes))],
		AllowLAN:    f.checked(sdIDLAN),
		StartLogin:  f.checked(sdIDLogin),
		HideTray:    f.checked(sdIDTray),
	}
	if _, ok := f.inputs[sdIDTun]; ok {
		s.TunEnabled = f.checked(sdIDTun)
	}
	if err := checkSettings(s); err != nil {
		showMessageBox("Bad Proxy", err.Error(), false)
		return false
	}
	sf.out = s
	return true
}
