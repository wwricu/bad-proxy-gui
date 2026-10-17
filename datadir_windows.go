//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// portableConfigDir keeps state.json and rules.dat beside the executable, so
// unzipping one folder anywhere (another machine, a USB stick) carries the
// whole setup along.
//
// Two conditions, both required:
//   - a `.portable` marker next to the exe. Release zips ship it; a build/bin
//     development binary does not, so dev runs keep using %AppData% instead of
//     scattering a second state.json through the source tree.
//   - the directory is actually writable. Program Files and read-only media
//     must fall back to %AppData%, not fail on every save.
func portableConfigDir() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, portableMarker)); err != nil {
		return "", false
	}
	if !dirWritable(dir) {
		return "", false
	}
	return dir, true
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".bad-proxy-gui-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
