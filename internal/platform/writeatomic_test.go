package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The one property both of the app's persisted files depend on: the folder
// holds the file and nothing else. A temporary left behind is a second copy of
// yesterday's state that the next reader may pick up by a glob, and a file
// written in place is the half a crash leaves.
func TestWriteAtomicLeavesOnlyTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")

	if err := WriteAtomic(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("second")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "second" {
		t.Errorf("content = %q, want the whole of the second write", b)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the folder holds %s, want only data.txt", strings.Join(names, ", "))
	}
}

// A write whose folder is not there must say so. Both callers create the folder
// first and would otherwise report success while nothing was saved.
func TestWriteAtomicRefusesAMissingFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created", "data.txt")
	if err := WriteAtomic(path, []byte("x")); err == nil {
		t.Fatalf("wrote %s into a folder that does not exist", path)
	}
}
