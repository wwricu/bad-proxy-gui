//go:build !darwin

package main

// becomeForeground/becomeBackground are no-ops off macOS: only AppKit has an
// activation-policy / Dock-icon notion. watchReopen is dead code off macOS too
// (the tray is the reopen affordance there), but it must still link.
func becomeForeground(string) {}
func becomeBackground()       {}
func watchReopen(string)      {}
