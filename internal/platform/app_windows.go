//go:build windows

package platform

// BecomeForeground/BecomeBackground are no-ops on Windows: only AppKit has an
// activation-policy / Dock-icon notion.
func BecomeForeground(string) {}
func BecomeBackground()       {}
