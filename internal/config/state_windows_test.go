//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Windows refuses to rename onto a file somebody has open - "Access is denied",
// not "busy, try again" - and the state file is opened by things this app does
// not control: Defender scanning the file it just wrote, the indexer, a backup
// agent. The first save lands, the second one right after it fails, and the
// dialog reports that the user's own settings could not be saved.
//
// A plain os.Open is enough to reproduce it, so the test holds one the way those
// scanners do: for long enough that a single rename attempt loses the race, and
// then let go.
func TestSaveWaitsOutALockOnTheTarget(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Save(DefaultState()); err != nil {
		t.Fatal(err)
	}

	h, err := os.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	failed := make(chan error, 1)
	go func() {
		next := DefaultState()
		next.HttpPort = "18080"
		failed <- st.Save(next)
	}()
	time.Sleep(50 * time.Millisecond)
	h.Close()

	if err := <-failed; err != nil {
		t.Fatalf("a save that outlived its lock was refused: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "18080") {
		t.Errorf("the retry wrote the wrong state: %s", b)
	}
}

// The wait is a deadline, not a hope: a port this app owns being held by
// something that will not let go has to come back as the error the user can act
// on, not as a save that never returns.
func TestSaveGivesUpOnAPermanentLock(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Save(DefaultState()); err != nil {
		t.Fatal(err)
	}
	h, err := os.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	start := time.Now()
	err = st.Save(DefaultState())
	if err == nil {
		t.Fatal("the save reported success over a handle that never closed")
	}
	if !strings.Contains(err.Error(), "state.json") {
		t.Errorf("the error does not name the file at fault: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the retry budget turned a refusal into a %v hang", d)
	}
}
