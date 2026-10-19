//go:build darwin

package platform

// Only Windows gets a native shortcut menu; macOS keeps the DOM-drawn fallback
// in the frontend.
const NativeContextMenuSupported = false

func ShowNativeContextMenu(labels []string) (int, error) { return -1, nil }
