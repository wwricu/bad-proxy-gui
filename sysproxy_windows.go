//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// systemProxySupported reports that this platform can toggle the OS proxy.
const systemProxySupported = true

var (
	wininet            = windows.NewLazySystemDLL("wininet.dll")
	internetSetOptionW = wininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

// applySystemProxy enables/disables the Windows system proxy (HKCU, no admin
// needed) pointing at the local HTTP+SOCKS listeners, then tells WinINet to
// reload so browsers pick it up without a restart.
func applySystemProxy(enable bool, s GUIState) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open internet settings: %w", err)
	}
	defer k.Close()

	if enable {
		// per-protocol form so http/https and socks both route locally
		server := fmt.Sprintf("http=127.0.0.1:%s;https=127.0.0.1:%s;socks=127.0.0.1:%s",
			s.HttpPort, s.HttpPort, s.SocksPort)
		if err := k.SetStringValue("ProxyServer", server); err != nil {
			return err
		}
	}
	if err := k.SetDWordValue("ProxyEnable", dword(enable)); err != nil {
		return err
	}

	_, _, _ = internetSetOptionW.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	_, _, _ = internetSetOptionW.Call(0, uintptr(internetOptionRefresh), 0, 0)
	return nil
}

func dword(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}
