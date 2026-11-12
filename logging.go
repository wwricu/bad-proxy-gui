package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wwricu/bad-proxy-gui/internal/config"
)

// logf appends one line to app.log in the data directory.
//
// The shipped binary is built with -H windowsgui, which gives it no console:
// stdout and stderr are discarded, so println is a no-op and every error routed
// through it is simply lost. Startup is the part with nowhere else to complain
// - an engine that refused to bind, or a state file that could not be parsed,
// both happen before there is a window to show a message in - and the log is
// where a user being asked for a screenshot has to be pointed.
//
// Nothing here fails: a second error from the act of reporting the first has no
// reader, so a log that cannot be written is dropped rather than chased.
func logf(format string, args ...any) {
	dir, err := config.UserDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "app.log")
	// One previous file is kept, so the pair cannot grow without bound.
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	line := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
}
