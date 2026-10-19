//go:build windows

package platform

import (
	"os"
	"time"
)

// RenameOver moves src onto dst, replacing whatever is there, and waits out the
// moment when something else has dst open.
//
// Windows refuses a rename onto a file another process holds with "Access is
// denied" - a refusal, not a busy-retry - and the holder is usually not this
// app. Defender reads a file the instant it appears, and the indexer and backup
// agents do the same, so the second save in a row (two dialog presses, a save
// racing the one before it) loses a race that is over in milliseconds. Waiting is
// the whole fix; on Unix the first rename already succeeds and the loop never
// runs a second time.
//
// It is a deadline rather than a number of attempts so that a lock which really
// will not release still ends in the error naming the file, inside the time a
// dialog can spend waiting on it.
func RenameOver(src, dst string) error {
	deadline := time.Now().Add(300 * time.Millisecond)
	for {
		err := os.Rename(src, dst)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}
