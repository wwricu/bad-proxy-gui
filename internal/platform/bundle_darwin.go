//go:build darwin

package platform

import (
	"os"
	"path/filepath"
)

// BundleRulesPath finds the dataset sealed into the running .app bundle:
//
//	Bad Proxy.app/Contents/MacOS/bad-proxy-gui  ->  Contents/Resources/rules.dat
//
// Loading it in place instead of copying it out keeps the routing rules in
// step with the app version, and never writes inside the bundle: that would
// break the code signature. A rules.dat in Application Support still wins, so
// an explicit user override works.
//
// package-darwin.sh (or the CI mac job) places the file there before signing.
func BundleRulesPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" {
		return ""
	}
	path := filepath.Join(filepath.Dir(contents), "Contents", "Resources", "rules.dat")
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		return path
	}
	return ""
}
