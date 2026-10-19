//go:build darwin

package main

// Only the Windows build goes portable: macOS ships a signed .app bundle, so
// writing beside the executable would break its seal.
func portableConfigDir() (string, bool) { return "", false }
