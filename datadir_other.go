//go:build !windows

package main

// Only the Windows build goes portable: macOS ships a signed .app bundle, so
// writing beside the executable would break its seal, and Linux stays on the
// XDG config dir.
func portableConfigDir() (string, bool) { return "", false }
