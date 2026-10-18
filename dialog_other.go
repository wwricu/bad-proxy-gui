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

func runSettingsDialog(in Settings) (Settings, bool) { return in, false }
