//go:build !windows && !darwin

package main

import "errors"

// startAtLoginSupported is false on Linux/BSD: the toggle is hidden so the UI
// never offers a control that cannot take effect.
const startAtLoginSupported = false

// setStartAtLogin is not implemented for this platform.
func setStartAtLogin(bool) error {
	return errors.New("start at login is not supported on this platform")
}
