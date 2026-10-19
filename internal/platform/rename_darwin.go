//go:build darwin

package platform

import "os"

// RenameOver moves src onto dst, replacing whatever is there.
//
// No waiting is needed here: POSIX rename is atomic whether or not another
// process has dst open, and the holder simply keeps reading the inode it was
// given. Windows makes dst unmovable while it is open, which is why its variant
// retries - see rename_windows.go.
func RenameOver(src, dst string) error {
	return os.Rename(src, dst)
}
