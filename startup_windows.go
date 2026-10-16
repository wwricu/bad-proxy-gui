//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// startAtLoginSupported reports that this platform can register a login item.
const startAtLoginSupported = true

const (
	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Bad Proxy"
)

// setStartAtLogin adds or removes the current executable from HKCU Run, so the
// app launches when the user signs in. HKCU needs no admin. The path is quoted
// because install locations commonly contain spaces.
func setStartAtLogin(enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open run key: %w", err)
	}
	defer k.Close()

	if !enable {
		if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValueName, `"`+exe+`"`)
}
