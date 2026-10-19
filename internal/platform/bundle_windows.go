//go:build windows

package platform

// Only a macOS .app bundle carries resources next to its executable; on Windows
// the dataset always lives in the data dir.
func BundleRulesPath() string { return "" }
