//go:build !windows

package main

// nodeDialogSupported and settingsDialogSupported keep the frontend on its HTML
// cards where no native window exists (macOS/Linux): Wails v2 owns exactly one
// window, so a second OS window has to be hand-built and there is no such
// implementation off Windows.
const (
	nodeDialogSupported     = false
	settingsDialogSupported = false
)

func runNodeDialog(in Node) (Node, bool) { return in, false }

// runSettingsDialog has no window here, and the draft is never applied because
// the frontend keeps its HTML card and calls ApplySettings itself.
func runSettingsDialog(in Settings, _ func(Settings) error) (Settings, bool) {
	return in, false
}
