package main

import "testing"

func TestNormalizeProxyMode(t *testing.T) {
	cases := []struct{ in, want string }{
		{modeDirect, modeDirect},
		{modeProxy, modeProxy},
		// system is a real, persistent mode now.
		{modeSystem, modeSystem},
		// the retired "clear" action and legacy junk fall back to direct.
		{"clear", modeDirect},
		{"", modeDirect},
		{"on", modeDirect},
		{"nonsense", modeDirect},
	}
	for _, tc := range cases {
		s := GUIState{ProxyMode: tc.in}
		s.normalize()
		if s.ProxyMode != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.in, s.ProxyMode, tc.want)
		}
	}
}

func TestNormalizeDefaultNode(t *testing.T) {
	cases := []struct {
		name string
		s    GUIState
		want int
	}{
		{"empty stays -1", GUIState{Active: -1}, -1},
		{"unset picks first", GUIState{Nodes: []Node{{Name: "a"}, {Name: "b"}}, Active: -1}, 0},
		{"out of range picks first", GUIState{Nodes: []Node{{Name: "a"}}, Active: 9}, 0},
		{"valid preserved", GUIState{Nodes: []Node{{Name: "a"}, {Name: "b"}}, Active: 1}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.s
			s.normalize()
			if s.Active != tc.want {
				t.Fatalf("Active = %d, want %d", s.Active, tc.want)
			}
		})
	}
}

func TestVisibleNodeFields(t *testing.T) {
	cases := []struct {
		proto, transport string
		want             nodeFields
	}{
		{"vless", "tcp", nodeFields{Secret: true}},
		{"vless", "tls", nodeFields{Secret: true, Authority: true}},
		{"vless", "xhttp", nodeFields{Secret: true, Authority: true, Path: true, Mode: true}},
		{"trojan", "xhttp", nodeFields{Secret: true, Authority: true, Path: true, Mode: true}},
		{"btp", "tls", nodeFields{Secret: true, Authority: true}},
		{"socks", "xhttp", nodeFields{Authority: true, Path: true, Mode: true}},
		{"socks", "tcp", nodeFields{}},
	}
	for _, tc := range cases {
		t.Run(tc.proto+"/"+tc.transport, func(t *testing.T) {
			if got := visibleNodeFields(tc.proto, tc.transport); got != tc.want {
				t.Fatalf("visibleNodeFields(%q,%q) = %+v, want %+v", tc.proto, tc.transport, got, tc.want)
			}
		})
	}
}

// The client makes no plaintext hop, so a state file written by an older build
// (transport "tcp", or the tls flag) must come back encrypted instead of dialing
// the server in the clear.
func TestNormalizeMigratesPlaintextTransport(t *testing.T) {
	cases := []struct{ in, want string }{
		{"tcp", "tls"},
		{"", "tls"},
		{"nonsense", "tls"},
		{"tls", "tls"},
		{"xhttp", "xhttp"},
	}
	for _, tc := range cases {
		s := GUIState{Nodes: []Node{{Name: "a", Transport: tc.in}}, Active: 0}
		s.normalize()
		if got := s.Nodes[0].Transport; got != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The engine reads the node straight out of the form/state, so the always-TLS
// decision has to be visible in the outbound it builds.
func TestNodeOutboundAlwaysEncrypts(t *testing.T) {
	for _, transport := range []string{"tcp", "tls", "xhttp", ""} {
		n := Node{Name: "a", Protocol: "vless", Server: "example.com", Port: "443", Transport: transport}
		tr := n.outbound("").Transport
		if !tr.Tls {
			t.Errorf("node with transport %q built a hop with Tls=false", transport)
		}
		if tr.Type == "tcp" {
			t.Errorf("node with transport %q still dials raw tcp", transport)
		}
	}
}
