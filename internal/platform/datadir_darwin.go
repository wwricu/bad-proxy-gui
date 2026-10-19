//go:build darwin

package platform

// Only the Windows build goes portable: macOS ships a signed .app bundle, so
// writing beside the executable would break its seal.
func PortableConfigDir() (string, bool) { return "", false }
