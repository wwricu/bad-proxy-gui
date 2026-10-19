package platform

import (
	"os"
	"path/filepath"
)

// WriteAtomic puts data at path so that a reader never sees half of a file: the
// bytes go into a temporary file in the same folder and are renamed over the
// target once all of them are written and closed.
//
// The same folder is what makes it work - a rename cannot cross a volume - and
// the rename is RenameOver because the file being replaced may be held open for
// a moment by something that is not this app.
//
// Both halves of the app that persist anything use this: the state file, which
// is read again on the next launch, and the system proxy backup, which is the
// only copy of settings that are about to be overwritten and cannot be
// recovered if the copy is torn.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // nothing to remove once the rename has moved it
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return RenameOver(name, path)
}
