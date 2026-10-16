package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Node is one upstream the user adds; it maps to a core outbound. Only one
// node is active at a time and becomes the default (empty-tag) outbound.
type Node struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"` // btp, vless, trojan, socks
	Server    string `json:"server"`
	Port      string `json:"port"`
	Secret    string `json:"secret"` // uuid / password; socks ignores it
	Transport string `json:"transport"`
	Path      string `json:"path"`      // xhttp request path
	Authority string `json:"authority"` // SNI / Host, defaults to Server
	TLS       bool   `json:"tls"`       // xhttp only: https vs http
	Padding   string `json:"padding"`   // xhttp X-Padding range
}

// GUIState is everything persisted between runs. The two local inbounds are
// fixed (socks + http); the user only picks their ports.
type GUIState struct {
	SocksPort   string `json:"socks_port"`
	HttpPort    string `json:"http_port"`
	Nodes       []Node `json:"nodes"`
	Active      int    `json:"active"` // index into Nodes; -1 = none
	SystemProxy bool   `json:"system_proxy"`
	HideTray    bool   `json:"hide_tray"`    // hide the tray / menu-bar icon (applies on restart)
	AllowLAN    bool   `json:"allow_lan"`    // bind the inbounds on 0.0.0.0 so other devices can use them
	AutoConnect bool   `json:"auto_connect"` // connect to the active node automatically on launch
}

func defaultState() GUIState {
	return GUIState{SocksPort: "10808", HttpPort: "10809", Active: -1}
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
//   - tls      -> SNI (authority) only; no path/padding/https (TLS is implied).
//   - xhttp    -> path, authority (Host), padding range, and the https toggle.
//   - tcp      -> raw stream; nothing beyond host/port.
type nodeFields struct {
	Secret, Authority, Path, Padding, HTTPS bool
}

func visibleNodeFields(protocol, transport string) nodeFields {
	f := nodeFields{Secret: protocol != "socks"}
	switch transport {
	case "tls":
		f.Authority = true
	case "xhttp":
		f.Authority = true
		f.Path = true
		f.Padding = true
		f.HTTPS = true
	}
	return f
}

// normalize enforces the "always a default node" invariant: whenever at least
// one node exists, Active points at a valid one (first node if unset/out of
// range). With zero nodes it resets Active to -1.
func (s *GUIState) normalize() {
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

// appConfigDir is where GUI state persists, independent of fyne's storage so a
// plain Go build (and CI smoke test) can locate it without a running app.
func appConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "bad-proxy-gui")
	return dir, os.MkdirAll(dir, 0o700)
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
