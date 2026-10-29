package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wwricu/bad-proxy-gui/internal/platform"
)

// Node is one upstream the user adds; it maps to a core outbound. Only one
// node is active at a time and becomes the default (empty-tag) outbound.
//
// There is deliberately no plaintext field: the client always encrypts the hop
// (tls, or xhttp/http2 over tls) and verifies the certificate against the
// system trust store. A node saved by an older build with "tls": false simply
// loses that key, so it comes back encrypted.
type Node struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"` // btp, vless, trojan, socks
	Server    string `json:"server"`
	Port      string `json:"port"`
	Secret    string `json:"secret"` // uuid / password; socks ignores it
	Transport string `json:"transport"`
	Path      string `json:"path"`      // xhttp/http2 request path
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
	ModeGlobal       = "global"
	ModeCNWhitelist  = "cn-whitelist"
	ModeGFWBlacklist = "gfw-blacklist"
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
	ModeDirect = "direct"
	ModeProxy  = "proxy"
	ModeSystem = "system"
)

// State is everything persisted between runs. The two local inbounds are
// fixed (socks + http); the user only picks their ports.
type State struct {
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

func DefaultState() State {
	return State{SocksPort: "10808", HttpPort: "10809", Active: -1, ProxyMode: ModeDirect, RoutingMode: ModeGlobal}
}

// InboundHost is the address the two local inbounds bind. Loopback keeps the
// proxy to this machine; AllowLAN hands it to the rest of the local network.
func (s State) InboundHost() string {
	if s.AllowLAN {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// InboundAddrs are the two sockets the engine has to take, SOCKS first then
// HTTP. This is the one place that knows both halves - the host above and the
// ports - because the engine binds them, checks they are free, and waits for
// them to answer, and all three have to mean the same pair.
func (s State) InboundAddrs() []string {
	host := s.InboundHost()
	return []string{net.JoinHostPort(host, s.SocksPort), net.JoinHostPort(host, s.HttpPort)}
}

// Clone returns a copy that owns its node list. A plain struct assignment shares
// the Nodes backing array, so a later DeleteNode - which shifts the tail down in
// place - would rewrite the list that copy is still holding.
func (s State) Clone() State {
	c := s
	if s.Nodes != nil {
		c.Nodes = append([]Node(nil), s.Nodes...)
	}
	return c
}

// ActiveNode returns the selected node, or an error if none is selected.
func (s State) ActiveNode() (Node, error) {
	if s.Active < 0 || s.Active >= len(s.Nodes) {
		return Node{}, errors.New("no active node selected")
	}
	return s.Nodes[s.Active], nil
}

// RemoveNode deletes one row and keeps the selection pointing at the same node
// it was pointing at, which is the only promise a user makes when they delete an
// entry they are not using. Active is an index, so the rows after the removed one
// all move up: without this, deleting the first of three nodes while using the
// third silently starts carrying traffic through the second, and the only sign is
// a different latency number.
//
// Deleting the node in use has no answer that keeps the promise, so it falls to
// the row that slid into its place - the next one, or the last when it was last.
func (s *State) RemoveNode(index int) error {
	if index < 0 || index >= len(s.Nodes) {
		return fmt.Errorf("invalid index: %d", index)
	}
	s.Nodes = append(s.Nodes[:index:index], s.Nodes[index+1:]...)
	switch {
	case index < s.Active:
		s.Active--
	case index == s.Active:
		if s.Active >= len(s.Nodes) {
			s.Active = len(s.Nodes) - 1
		}
	}
	s.Normalize()
	return nil
}

// XHTTPMode folds a configured request shape onto one of the four names the
// transport speaks. Unset is stream-one: that is the shape every node saved
// before this field existed already used, and the one long POST request that
// survives a buffering reverse proxy. Anything else is a typo in the state file
// and gets the same treatment.
func XHTTPMode(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	switch m {
	case "auto", "stream-up", "packet-up", "stream-one":
		return m
	}
	return "stream-one"
}

// EncryptedTransport folds every transport value a node can carry onto one of
// the hops the client is willing to make: "tls" (TCP wrapped in TLS), "xhttp"
// over https, or "http2" over https. Raw tcp is cleartext, so it is not offered
// and any legacy or blank value is migrated to tls, which reaches the same
// server the way Xray's "tcp + security: tls" does.
func EncryptedTransport(transport string) string {
	switch transport {
	case "xhttp", "http2":
		return transport
	}
	return "tls"
}

// Normalize enforces the "always a default node" invariant: whenever at least
// one node exists, Active points at a valid one (first node if unset/out of
// range). With zero nodes it resets Active to -1.
func (s *State) Normalize() {
	// Backfill and migrate legacy/unknown values ("", "off", "on", the retired
	// "clear" action) to direct, the always-on idle state.
	switch s.ProxyMode {
	case ModeDirect, ModeProxy, ModeSystem:
	default:
		s.ProxyMode = ModeDirect
	}
	switch s.RoutingMode {
	case ModeGlobal, ModeCNWhitelist, ModeGFWBlacklist:
	default:
		s.RoutingMode = ModeGlobal
	}
	// Fold legacy tcp (and blank) transports onto tls before anything reads a
	// node, so an old state file cannot dial a public server in the clear. The
	// xhttp shape is folded at the same time: those nodes carry no mode field
	// yet, and the engine should dial the request they always dialled.
	for i := range s.Nodes {
		s.Nodes[i].Transport = EncryptedTransport(s.Nodes[i].Transport)
		if s.Nodes[i].Transport == "xhttp" {
			s.Nodes[i].Mode = XHTTPMode(s.Nodes[i].Mode)
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

type Store struct{ path string }

func NewStore(dir string) *Store {
	return &Store{path: filepath.Join(dir, "state.json")}
}

// Dir is the folder the state file lives in. A platform that has no per-app
// store of its own keeps what it is about to overwrite of the system proxy in
// here.
func (s *Store) Dir() string { return filepath.Dir(s.path) }

// OpenStore creates a store in the per-user directory UserDir picks.
func OpenStore() *Store {
	dir, err := UserDir()
	if err != nil {
		dir = "."
	}
	adoptLegacyState(dir)
	return NewStore(dir)
}

// appDirName is the per-user config folder name.
const appDirName = "bad-proxy-gui"

// UserDir is where GUI state persists. A plain Go build (and CI smoke
// test) can locate it without a running app.
func UserDir() (string, error) {
	if dir, ok := platform.PortableConfigDir(); ok {
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

// Load reads the saved state. A file that cannot be parsed is an error rather
// than a first run: the defaults it returns are unusable until the user is told
// why their nodes are missing, and the unreadable file is kept so that telling
// them is possible after the next save has replaced it.
func (st *Store) Load() (State, error) {
	s := DefaultState()
	b, err := os.ReadFile(st.path)
	if err != nil {
		// Two different answers hide in this one error, and only one of them is
		// innocent. There is no file, so this is a first run. There is a file and
		// this process was not allowed to look at it - an antivirus or a sync
		// client holding it, a folder that went read-only - and answering "no
		// state" to that tells the app it is on its first run, which lets the next
		// click save an empty node list over the file it could not read.
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return DefaultState(), fmt.Errorf("the saved state at %s could not be read: %w", st.path, err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return DefaultState(), st.quarantine(err)
	}
	if s.SocksPort == "" {
		s.SocksPort = "10808"
	}
	if s.HttpPort == "" {
		s.HttpPort = "10809"
	}
	s.Normalize()
	return s, nil
}

// quarantine moves an unreadable state.json to a timestamped copy of itself.
// Without this the next save writes defaults over it and both the nodes and the
// evidence of what broke disappear together, which looks exactly like the app
// having lost them.
func (st *Store) quarantine(unmarshalErr error) error {
	kept := st.path + ".corrupt-" + time.Now().Format("20060102-150405")
	if err := os.WriteFile(kept, st.readOrEmpty(), 0o600); err != nil {
		return fmt.Errorf("%s: state file could not be read and could not be kept either: %w",
			st.path, err)
	}
	return fmt.Errorf("%s: %w (the unreadable file is kept as %s)", st.path, unmarshalErr, kept)
}

func (st *Store) readOrEmpty() []byte {
	b, err := os.ReadFile(st.path)
	if err != nil {
		return nil
	}
	return b
}

// Save writes the state through platform.WriteAtomic: a full file or the
// previous one, never the half that a crash or a full disk leaves behind in
// place of it.
func (st *Store) Save(s State) error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteAtomic(st.path, b)
}
