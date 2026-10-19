//go:build windows

package main

// becomeForeground/becomeBackground are no-ops on Windows: only AppKit has an
// activation-policy / Dock-icon notion.
func becomeForeground(string) {}
func becomeBackground()       {}
