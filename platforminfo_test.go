package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// platformRead is how the frontend asks for one of these keys: the object it
// destructured from GetPlatformInfo is `platform` in the settings window and
// `platformInfo` in the list, and both spellings have to be found.
var platformRead = regexp.MustCompile(`\bplatform(?:Info)?\.([A-Za-z][A-Za-z0-9_]*)`)

// The frontend reads these keys by name, and a name that stops matching is not
// an error anywhere: an absent key is undefined, undefined is false, and false
// hides a row of the settings window. That is how the tray checkbox and the
// System proxy mode both disappeared on every platform while every test stayed
// green and nobody could say when.
func TestPlatformInfoKeysMatchTheFrontend(t *testing.T) {
	wanted := frontendPlatformKeys(t)
	got := (&App{}).GetPlatformInfo()

	for key := range wanted {
		if _, ok := got[key]; !ok {
			t.Errorf("the frontend reads platform.%s and GetPlatformInfo does not answer it, so it reads as false", key)
		}
	}
	for key := range got {
		if !wanted[key] {
			t.Errorf("GetPlatformInfo answers platform.%s and no frontend file reads it", key)
		}
	}
}

func frontendPlatformKeys(t *testing.T) map[string]bool {
	t.Helper()
	names, err := filepath.Glob(filepath.Join("frontend", "src", "*.js"))
	if err != nil || len(names) == 0 {
		t.Fatalf("the frontend sources are not where this test looks: %v", err)
	}
	keys := map[string]bool{}
	for _, name := range names {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range platformRead.FindAllSubmatch(b, -1) {
			keys[string(m[1])] = true
		}
	}
	if len(keys) == 0 {
		var read []string
		for _, name := range names {
			read = append(read, filepath.Base(name))
		}
		sort.Strings(read)
		t.Fatalf("no frontend file reads a platform key at all (%v) - the test is broken, not the app", read)
	}
	return keys
}
