package config

import "testing"

// three is the list every case below starts from, with the node in use named so
// the assertion can say which server traffic is going through rather than which
// row number survived.
func three() State {
	return State{Nodes: []Node{{Name: "a"}, {Name: "b"}, {Name: "c"}}, Active: 2}
}

// The promise a delete has to keep: removing a row the user is not using leaves
// them on the node they were on. Active is an index into the list, so every row
// after the deleted one moves up, and a delete that only deletes starts carrying
// the user's traffic through a different server.
func TestRemoveNodeKeepsTheSameNodeSelected(t *testing.T) {
	cases := []struct {
		name    string
		st      State
		drop    int
		wantAct int
		want    string
	}{
		{"before the one in use", three(), 0, 1, "c"},
		{"just before the one in use", three(), 1, 1, "c"},
		{"after the one in use", State{Nodes: []Node{{Name: "a"}, {Name: "b"}, {Name: "c"}}, Active: 0}, 2, 0, "a"},
		{"the one in use takes its place", State{Nodes: []Node{{Name: "a"}, {Name: "b"}, {Name: "c"}}, Active: 1}, 1, 1, "c"},
		{"deleting the last row in use falls back to the new last", three(), 2, 1, "b"},
		{"first of two, in use", State{Nodes: []Node{{Name: "a"}, {Name: "b"}}, Active: 0}, 0, 0, "b"},
		{"the only node", State{Nodes: []Node{{Name: "a"}}, Active: 0}, 0, -1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.st
			if err := st.RemoveNode(tc.drop); err != nil {
				t.Fatalf("a row the list has must delete: %v", err)
			}
			if tc.want == "" {
				if st.Active != -1 || len(st.Nodes) != 0 {
					t.Errorf("deleting the last row left %d nodes and Active=%d", len(st.Nodes), st.Active)
				}
				return
			}
			if st.Active != tc.wantAct {
				t.Errorf("Active=%d after the delete, want %d", st.Active, tc.wantAct)
			}
			got, err := st.ActiveNode()
			if err != nil {
				t.Fatalf("no node selected after the delete: Active=%d of %d rows", st.Active, len(st.Nodes))
			}
			if got.Name != tc.want {
				t.Errorf("Active=%d selects %q, want %q", st.Active, got.Name, tc.want)
			}
		})
	}
}

// The index is the frontend's, and a row can go away between the click that drew
// it and the click on its delete button. Refusing is the only safe answer, and it
// has to leave the list exactly as it was.
func TestRemoveNodeRefusesAnIndexTheListDoesNotHave(t *testing.T) {
	for _, i := range []int{-1, 3, 99} {
		st := three()
		if err := st.RemoveNode(i); err == nil {
			t.Errorf("RemoveNode(%d) on a three-row list succeeded", i)
		}
		if len(st.Nodes) != 3 || st.Active != 2 {
			t.Errorf("the refused delete changed the list: %d rows, Active=%d", len(st.Nodes), st.Active)
		}
	}
}
