//go:build windows

package main

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// systemProxySupported reports that this platform can set and clear the OS proxy.
const systemProxySupported = true

var (
	wininet            = windows.NewLazySystemDLL("wininet.dll")
	internetSetOptionW = wininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

// setSystemProxy points the WinINet http proxy at 127.0.0.1:port and enables
// it. <local> plus the loopback literals go in ProxyOverride so the OS does not
// proxy the very ports we are dialing.
func setSystemProxy(port string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open internet settings: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue("ProxyServer", "127.0.0.1:"+port); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", "<local>;127.0.0.1;localhost"); err != nil {
		return err
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}

	_, _, _ = internetSetOptionW.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	_, _, _ = internetSetOptionW.Call(0, uintptr(internetOptionRefresh), 0, 0)
	return nil
}

// clearSystemProxy wipes the WinINet proxy configuration outright. Flipping
// ProxyEnable to 0 is not enough: Settings keeps rendering the stale
// ProxyServer/ProxyOverride values, and any app that reads ProxyServer without
// checking ProxyEnable still picks the proxy up. The values are deleted, so
// whatever another program left behind is gone too.
func clearSystemProxy() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open internet settings: %w", err)
	}
	defer k.Close()

	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		return err
	}
	for _, name := range []string{"ProxyServer", "ProxyOverride", "AutoConfigURL", "ProxyPacUrl"} {
		if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", name, err)
		}
	}

	// Tell WinINet to reload so already-running browsers drop the proxy.
	_, _, _ = internetSetOptionW.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	_, _, _ = internetSetOptionW.Call(0, uintptr(internetOptionRefresh), 0, 0)
	return nil
}
