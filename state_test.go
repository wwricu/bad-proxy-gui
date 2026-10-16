package main

import "testing"

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
		{"vless", "xhttp", nodeFields{Secret: true, Authority: true, Path: true, Padding: true, HTTPS: true}},
		{"trojan", "xhttp", nodeFields{Secret: true, Authority: true, Path: true, Padding: true, HTTPS: true}},
		{"btp", "tls", nodeFields{Secret: true, Authority: true}},
		{"socks", "xhttp", nodeFields{Authority: true, Path: true, Padding: true, HTTPS: true}},
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
