//go:build darwin

package main

// Only Windows gets a native shortcut menu; macOS keeps the DOM-drawn fallback
// in the frontend.
const nativeContextMenuSupported = false

func showNativeContextMenu(labels []string) (int, error) { return -1, nil }
