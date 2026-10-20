package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file that is there and cannot be read is not a first run. The distinction
// matters because the app starts either way and the first edit writes a state
// file, so answering "no state" to a lock or a permission refusal is how a node
// list disappears without anything ever saying it failed.
//
// A directory in the place of the state file is the portable way to be shown a
// file that exists and refuses to be read: the error is EISDIR, not ENOENT, on
// both platforms.
func TestLoadTellsAnUnreadableFileFromAnAbsentOne(t *testing.T) {
	dir := t.TempDir()

	if _, err := NewStore(dir).Load(); err != nil {
		t.Errorf("a data dir with no state file is a first run, not an error: %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "state.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := NewStore(dir).Load()
	if err == nil {
		t.Fatal("a state file that could not be read reported success, and the app would go on to overwrite it")
	}
	if !strings.Contains(err.Error(), "state.json") {
		t.Errorf("the error does not name the file at fault: %v", err)
	}
}
