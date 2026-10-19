package main

import (
	"sync"
	"testing"

	"github.com/wwricu/bad-proxy-gui/internal/config"
)

// Wails answers a binding call from its own goroutine - the runtime transport is
// an ordinary net/http handler - so two clicks can be in flight at the same
// time: the list window reads the state while an editor saves one, and a probe
// holds on to the state it was handed for the seconds its test engine runs.
//
// Nothing in App protected either. A struct copy of State shares its Nodes
// backing array with the live state, so a DeleteNode that shifts elements down
// rewrites the list a reader already took, and the slice header is written here
// while it is read there. Run with -race.
//
// The state is left in direct mode on purpose: no node reaches the engine
// configuration there, so the serialised path never starts a proxy and this test
// binds no ports.
func TestBindingStateAccessIsSerialised(t *testing.T) {
	a := &App{store: config.NewStore(t.TempDir())}
	st := config.DefaultState()
	st.Nodes = []config.Node{{Name: "one"}, {Name: "two"}, {Name: "three"}}
	st.Active = 0
	a.st = st

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// What every page does on every render, and what a probe is
				// handed before it starts an engine of its own.
				s := a.GetState()
				for _, n := range s.Nodes {
					if n.Name == "" {
						t.Error("a node read as its zero value")
						return
					}
				}
			}
		}()
	}
	for i := 0; i < 200; i++ {
		if err := a.DeleteNode(0); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if err := a.AddNode(config.Node{Name: "fresh"}); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	close(stop)
	wg.Wait()

	// What the copies bought, beyond the race: a list read before an edit keeps
	// the nodes it was given.
	a.st = st
	before := a.GetState()
	if err := a.DeleteNode(0); err != nil {
		t.Fatal(err)
	}
	if before.Nodes[1].Name != "two" || len(before.Nodes) != 3 {
		t.Errorf("the delete rewrote the list someone else was holding: %v", before.Nodes)
	}

	// Direct mode builds its proxy without any node, so none of the 400 edits
	// above had a reason to restart an engine - and one that did would have been
	// binding the real 10808/10809 from a unit test.
	if a.eng.Running() {
		t.Error("editing a node in direct mode restarted the engine")
	}
}
