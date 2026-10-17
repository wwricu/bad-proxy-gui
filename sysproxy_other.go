//go:build !windows && !darwin

package main

import "errors"

// systemProxySupported is false on Linux/BSD: the UI hides the control so it
// never offers a mode that cannot take effect.
const systemProxySupported = false

// setSystemProxy is not implemented for this platform.
func setSystemProxy(port string) error {
	return errors.New("setting the system proxy is not supported on this platform")
}

// clearSystemProxy is not implemented for this platform.
func clearSystemProxy() error {
	return errors.New("clearing the system proxy is not supported on this platform")
}
