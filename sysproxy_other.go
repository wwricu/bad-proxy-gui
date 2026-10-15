//go:build !windows

package main

import "errors"

// applySystemProxy is not implemented for macOS/Linux yet. On macOS this will
// shell out to `networksetup -setwebproxy/-setsocksfirewallproxy` once a Mac is
// available to verify the service name detection; until then it refuses so the
// checkbox does not silently no-op.
func applySystemProxy(bool, GUIState) error {
	return errors.New("system proxy toggle is Windows-only for now")
}
