package main

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/wwricu/bad-proxy-gui/internal/config"
	"github.com/wwricu/bad-proxy-gui/internal/platform"
)

// rulesDat is the compiled domain-list-community rule file shipped inside the
// binary. The core router can only load named lists (geosite-style) from a
// disk path, so ensureRulesDat resolves one; users can replace that file to
// update rules without a rebuild.
//
//go:embed rules.dat
var rulesDat []byte

// ensureRulesDat returns the on-disk path holding routing rules, or "" when no
// dataset can be resolved. An empty path only disables named lists (the
// geosite/geoip style rules); inline full:/domain:/regexp: rules still route.
//
// Resolution order, most specific first:
//  1. rules.dat in the data dir. This is the file the Windows portable zip
//     ships beside the exe, so each release brings fresh rules and a user can
//     drop in their own dataset to override.
//  2. rules.dat sealed inside a macOS .app bundle (Contents/Resources), which
//     is read-only and therefore always matches the app version.
//  3. the embedded copy, materialised into the data dir for a lone executable.
func ensureRulesDat() string {
	dir, err := config.UserDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(dir, "rules.dat")
	if fileExists(path) {
		return path
	}
	if bundle := platform.BundleRulesPath(); bundle != "" {
		return bundle
	}
	// Through WriteAtomic like everything else the app persists: a dataset cut
	// short by a full disk or a crash is a routing table that loads and answers
	// for far fewer domains than it looks like it does.
	if err := platform.WriteAtomic(path, rulesDat); err != nil {
		logf("the bundled rules dataset could not be written: %v", err)
		return ""
	}
	return path
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
