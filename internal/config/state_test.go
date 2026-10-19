package config

import "testing"

func TestNormalizeProxyMode(t *testing.T) {
	cases := []struct{ in, want string }{
		{ModeDirect, ModeDirect},
		{ModeProxy, ModeProxy},
		// system is a real, persistent mode now.
		{ModeSystem, ModeSystem},
		// the retired "clear" action and legacy junk fall back to direct.
		{"clear", ModeDirect},
		{"", ModeDirect},
		{"on", ModeDirect},
		{"nonsense", ModeDirect},
	}
	for _, tc := range cases {
		s := State{ProxyMode: tc.in}
		s.Normalize()
		if s.ProxyMode != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, s.ProxyMode, tc.want)
		}
	}
}

func TestNormalizeDefaultNode(t *testing.T) {
	cases := []struct {
		name string
		s    State
		want int
	}{
		{"empty stays -1", State{Active: -1}, -1},
		{"unset picks first", State{Nodes: []Node{{Name: "a"}, {Name: "b"}}, Active: -1}, 0},
		{"out of range picks first", State{Nodes: []Node{{Name: "a"}}, Active: 9}, 0},
		{"valid preserved", State{Nodes: []Node{{Name: "a"}, {Name: "b"}}, Active: 1}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.s
			s.Normalize()
			if s.Active != tc.want {
				t.Fatalf("Active = %d, want %d", s.Active, tc.want)
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
		s := State{Nodes: []Node{{Name: "a", Transport: tc.in}}, Active: 0}
		s.Normalize()
		if got := s.Nodes[0].Transport; got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// An xhttp node saved before the request-shape field existed has no mode, and
// one saved with a typo must not be dialled with whatever the engine cannot
// name: both fold onto the shape the node always used.
func TestNormalizeFoldsXHTTPMode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "stream-one"},
		{"auto", "auto"},
		{"stream-up", "stream-up"},
		{"packet-up", "packet-up"},
		{"STREAM-ONE", "stream-one"},
		{" long-poll ", "stream-one"},
		{"nonsense", "stream-one"},
	}
	for _, tc := range cases {
		s := State{Nodes: []Node{{Name: "a", Transport: "xhttp", Mode: tc.in}}, Active: 0}
		s.Normalize()
		if got := s.Nodes[0].Mode; got != tc.want {
			t.Errorf("xhttp mode %q folded to %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestActiveNode(t *testing.T) {
	if _, err := (State{}).ActiveNode(); err == nil {
		t.Error("a state with no nodes must not report an active node")
	}
	s := State{Nodes: []Node{{Name: "a"}, {Name: "b"}}, Active: 1}
	n, err := s.ActiveNode()
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "b" {
		t.Errorf("ActiveNode() = %q, want the node Active points at", n.Name)
	}
}

// A struct copy of a State shares the Nodes backing array, and DeleteNode edits
// that array in place. Clone is what stops a copy from being rewritten by the
// edit that follows it.
func TestCloneOwnsItsNodes(t *testing.T) {
	s := State{Nodes: []Node{{Name: "a"}, {Name: "b"}}}
	c := s.Clone()
	s.Nodes[0] = Node{Name: "changed"}
	s.Nodes = s.Nodes[:1]
	if c.Nodes[0].Name != "a" || len(c.Nodes) != 2 {
		t.Errorf("the clone moved with the original: %v", c.Nodes)
	}
	if nilState := (State{}).Clone(); nilState.Nodes != nil {
		t.Error("cloning a state with no nodes must not invent an empty slice")
	}
}
