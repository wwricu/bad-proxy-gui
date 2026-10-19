package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Node is one upstream the user adds; it maps to a core outbound. Only one
// node is active at a time and becomes the default (empty-tag) outbound.
//
// There is deliberately no plaintext field: the client always encrypts the hop
// (tls transport, or xhttp over https) and verifies the certificate against the
// system trust store. A node saved by an older build with "tls": false simply
// loses that key, so it comes back encrypted.
type Node struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"` // btp, vless, trojan, socks
	Server    string `json:"server"`
	Port      string `json:"port"`
	Secret    string `json:"secret"` // uuid / password; socks ignores it
	Transport string `json:"transport"`
	Path      string `json:"path"`      // xhttp request path
	Authority string `json:"authority"` // SNI / Host, defaults to Server
	Mode      string `json:"mode"`      // xhttp request shape: auto, stream-one, stream-up, packet-up
}

// Routing modes are the presets the UI exposes. They only take effect in the
// proxy/system proxy modes and split traffic by domain lists in rules.dat:
//   - global:      every request leaves through the node.
//   - cn-whitelist: mainland (cn list) domains direct, the rest through the node.
//   - gfw-blacklist: non-mainland (geolocation-!cn list) domains through the
//     node, the rest direct.
const (
	modeGlobal       = "global"
	modeCNWhitelist  = "cn-whitelist"
	modeGFWBlacklist = "gfw-blacklist"
)

// ProxyMode is the master switch. The engine ALWAYS runs from launch; the mode
// only decides which default outbound it uses and whether the OS proxy is
// pointed at it. There is no "off" state — "direct" is the idle state.
//   - direct: engine runs on a built-in freedom outbound; all traffic direct.
//   - proxy:  engine runs on the active node; apps use the local ports only.
//   - system: like proxy, but the OS proxy is set to the local http inbound so
//     apps route through it without manual configuration. Leaving system mode
//     (and quitting while in it) clears that OS proxy again.
const (
	modeDirect = "direct"
	modeProxy  = "proxy"
	modeSystem = "system"
)

// GUIState is everything persisted between runs. The two local inbounds are
// fixed (socks + http); the user only picks their ports.
type GUIState struct {
	SocksPort   string `json:"socks_port"`
	HttpPort    string `json:"http_port"`
	Nodes       []Node `json:"nodes"`
	Active      int    `json:"active"` // index into Nodes; -1 = none
	ProxyMode   string `json:"proxy_mode"`
	RoutingMode string `json:"routing_mode"`
	HideTray    bool   `json:"hide_tray"`   // hide the tray / menu-bar icon (applies on restart)
	AllowLAN    bool   `json:"allow_lan"`   // bind the inbounds on 0.0.0.0 so other devices can use them
	StartLogin  bool   `json:"start_login"` // register the app to launch at login
}

func defaultState() GUIState {
	return GUIState{SocksPort: "10808", HttpPort: "10809", Active: -1, ProxyMode: modeDirect, RoutingMode: modeGlobal}
}

// activeNode returns the selected node, or an error if none is selected.
func (s GUIState) activeNode() (Node, error) {
	if s.Active < 0 || s.Active >= len(s.Nodes) {
		return Node{}, errors.New("no active node selected")
	}
	return s.Nodes[s.Active], nil
}

// nodeFields says which optional node form fields are meaningful for a given
// protocol+transport. It mirrors exactly what the core engine reads:
//   - secret   -> used by btp/vless/trojan; the socks handshake ignores it.
//   - tls      -> SNI (authority) only. TLS is implied by the transport itself.
//   - xhttp    -> path, authority (Host), request shape (mode).
//   - tcp      -> retired: it is cleartext, and the client offers no plaintext
//     hop any more (see encryptedTransport).
type nodeFields struct {
	Secret, Authority, Path, Mode bool
}

func visibleNodeFields(protocol, transport string) nodeFields {
	f := nodeFields{Secret: protocol != "socks"}
	switch transport {
	case "tls":
		f.Authority = true
	case "xhttp":
		f.Authority = true
		f.Path = true
		f.Mode = true
	}
	return f
}

// xhttpMode folds a configured request shape onto one of the four names the
// transport speaks. Unset is stream-one: that is the shape every node saved
// before this field existed already used, and the one long POST request that
// survives a buffering reverse proxy. Anything else is a typo in the state file
// and gets the same treatment.
func xhttpMode(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	switch m {
	case "auto", "stream-up", "packet-up", "stream-one":
		return m
	}
	return "stream-one"
}

// encryptedTransport folds every transport value a node can carry onto one of
// the two hops the client is willing to make: "tls" (TCP wrapped in TLS) or
// "xhttp" over https. Raw tcp is cleartext, so it is not offered and any legacy
// or blank value is migrated to tls, which reaches the same server the way
// Xray's "tcp + security: tls" does.
func encryptedTransport(transport string) string {
	if transport == "xhttp" {
		return "xhttp"
	}
	return "tls"
}

// normalize enforces the "always a default node" invariant: whenever at least
// one node exists, Active points at a valid one (first node if unset/out of
// range). With zero nodes it resets Active to -1.
func (s *GUIState) normalize() {
	// Backfill and migrate legacy/unknown values ("", "off", "on", the retired
	// "clear" action) to direct, the always-on idle state.
	switch s.ProxyMode {
	case modeDirect, modeProxy, modeSystem:
	default:
		s.ProxyMode = modeDirect
	}
	switch s.RoutingMode {
	case modeGlobal, modeCNWhitelist, modeGFWBlacklist:
	default:
		s.RoutingMode = modeGlobal
	}
	// Fold legacy tcp (and blank) transports onto tls before anything reads a
	// node, so an old state file cannot dial a public server in the clear. The
	// xhttp shape is folded at the same time: those nodes carry no mode field
	// yet, and the engine should dial the request they always dialled.
	for i := range s.Nodes {
		s.Nodes[i].Transport = encryptedTransport(s.Nodes[i].Transport)
		if s.Nodes[i].Transport == "xhttp" {
			s.Nodes[i].Mode = xhttpMode(s.Nodes[i].Mode)
		}
	}
	if len(s.Nodes) == 0 {
		s.Active = -1
		return
	}
	if s.Active < 0 || s.Active >= len(s.Nodes) {
		s.Active = 0
	}
}

type store struct{ path string }

func newStore(dir string) *store {
	return &store{path: filepath.Join(dir, "state.json")}
}

// newStoreFromConfigDir creates a store in the directory appConfigDir picks.
func newStoreFromConfigDir() *store {
	dir, err := appConfigDir()
	if err != nil {
		dir = "."
	}
	adoptLegacyState(dir)
	return newStore(dir)
}

// appDirName is the per-user config folder name, and portableMarker is the
// file that opts a Windows release folder into keeping its data beside the
// executable.
const (
	appDirName     = "bad-proxy-gui"
	portableMarker = ".portable"
)

// appConfigDir is where GUI state persists. A plain Go build (and CI smoke
// test) can locate it without a running app.
func appConfigDir() (string, error) {
	if dir, ok := portableConfigDir(); ok {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appDirName)
	return dir, os.MkdirAll(dir, 0o700)
}

// legacyConfigDir is the pre-portable home, %AppData%\bad-proxy-gui on Windows.
func legacyConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDirName), nil
}

// adoptLegacyState copies an existing install's state.json into a portable
// folder that has none yet, so switching layouts does not look like losing
// every node. Only the copy is made: %AppData% keeps its own state, and
// rules.dat is deliberately not carried over — a stale dataset sitting next to
// the exe would outrank the fresh one the release zip ships forever after.
func adoptLegacyState(dir string) {
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err == nil {
		return
	}
	from, err := legacyConfigDir()
	if err != nil {
		return
	}
	src := filepath.Join(from, "state.json")
	b, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "state.json"), b, 0o600)
}

func (st *store) load() GUIState {
	s := defaultState()
	b, err := os.ReadFile(st.path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	if s.SocksPort == "" {
		s.SocksPort = "10808"
	}
	if s.HttpPort == "" {
		s.HttpPort = "10809"
	}
	s.normalize()
	return s
}

func (st *store) save(s GUIState) error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(st.path, b, 0o600)
}
