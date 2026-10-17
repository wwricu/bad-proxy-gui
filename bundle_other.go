//go:build !darwin

package main

// Only a macOS .app bundle carries resources next to its executable; on
// Windows and Linux the dataset always lives in the data dir.
func bundleRulesPath() string { return "" }
