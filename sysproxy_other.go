//go:build !windows && !darwin

package main

import "errors"

// systemProxySupported is false on Linux/BSD: the toggle is hidden so the UI
// never offers a control that cannot take effect.
const systemProxySupported = false

// applySystemProxy is not implemented for this platform.
func applySystemProxy(bool, GUIState) error {
	return errors.New("system proxy toggle is not supported on this platform")
}
