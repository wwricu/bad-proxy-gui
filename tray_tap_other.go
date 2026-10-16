//go:build !darwin

package main

// customizeTrayTap is a no-op off macOS: Windows reopens via Fyne's default tray
// tap handler, and its close-to-tray intercept is equivalent to a plain Hide
// (becomeBackground does nothing there).
func customizeTrayTap(c *controller) {}
