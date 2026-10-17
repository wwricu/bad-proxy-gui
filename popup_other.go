//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Only Windows gets a native shortcut menu; macOS/Linux keep the DOM-drawn
// fallback in the frontend.
const nativeContextMenuSupported = false

func showNativeContextMenu(labels []string) (int, error) { return -1, nil }

// showMessageBox pops a real macOS dialog. Elsewhere there is nothing to pop,
// and callers treat a false answer as "no".
func showMessageBox(title, message string, confirm bool) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	script := fmt.Sprintf(`display alert %q message %q`, title, message)
	if confirm {
		script = fmt.Sprintf(`display dialog %q buttons {"Cancel", "OK"} default button "Cancel"`, message)
	}
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return false
	}
	return !confirm || strings.Contains(string(out), "button returned:OK")
}
