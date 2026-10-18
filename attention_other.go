//go:build !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// flashForAttention is the platform's own attention signal: on macOS Wails maps
// it to requestUserAttention, which bounces the Dock icon once - the AppKit
// answer to "the window you want is already open, here it is". Linux has no
// equivalent, so the raise in the caller is all the user gets.
//
// AppKit has no public window shake. The one macOS does ship belongs to the
// sheet machinery (presenting a second sheet on a window that already has one
// shakes it) and cannot be asked for on its own, so the Dock bounce stands in
// for it rather than an animation of ours.
func flashForAttention(win application.Window) {
	win.Flash(true)
}
