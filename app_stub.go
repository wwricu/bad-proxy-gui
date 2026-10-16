//go:build !darwin

package main

// becomeForeground/becomeBackground are no-ops off macOS: only AppKit has an
// activation-policy / Dock-icon notion.
func becomeForeground() {}
func becomeBackground() {}
