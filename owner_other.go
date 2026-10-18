//go:build !windows && !darwin

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// keepAbove has nothing to be done here: the app is not packaged for Linux,
// and the toolkits it would use have no shared notion of a window that must
// stay above a sibling.
func keepAbove(win application.Window, owner application.Window) {}
