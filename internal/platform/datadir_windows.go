//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"sync"
)

// portableMarker is the file that opts a Windows release folder into keeping
// its data beside the executable.
const portableMarker = ".portable"

// portableDir is the answer below, worked out once per run. Both conditions are
// properties of the installation rather than of the running app, and the store
// fixes its directory at startup anyway, so a probe that changed its mind
// mid-run could only put two parts of the app in different folders. The probe
// has a cost worth not paying twice: every engine restart asks where the data
// directory is, and each answer used to create and delete a file beside the
// executable - on a USB stick, for all this knows.
var portableDir = sync.OnceValues(probePortableDir)

// PortableConfigDir keeps state.json and rules.dat beside the executable, so
// unzipping one folder anywhere (another machine, a USB stick) carries the
// whole setup along.
//
// Two conditions, both required:
//   - a `.portable` marker next to the exe. Release zips ship it; a build/bin
//     development binary does not, so dev runs keep using %AppData% instead of
//     scattering a second state.json through the source tree.
//   - the directory is actually writable. Program Files and read-only media
//     must fall back to %AppData%, not fail on every save.
func PortableConfigDir() (string, bool) { return portableDir() }

func probePortableDir() (string, bool) {
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
