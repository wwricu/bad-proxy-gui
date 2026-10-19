//go:build windows

package platform

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// proxyBackupKey holds what Internet Settings said before this app wrote over
// it. A released app takes the system proxy and gives it back on quit, and the
// machine it runs on may well have had a proxy of its own - a corporate manual
// proxy, or a PAC - so "give it back" has to mean the user's settings, not
// whatever the app happened to want.
const proxyBackupKey = `Software\wwricu\bad-proxy-gui\system-proxy`

// SystemProxySupported reports that this platform can set and clear the OS proxy.
const SystemProxySupported = true

var (
	wininet            = windows.NewLazySystemDLL("wininet.dll")
	internetSetOptionW = wininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

// proxyValue is one Internet Settings value this app takes over, and whether
// Windows keeps it as a number instead of a string.
type proxyValue struct {
	name  string
	dword bool
}

// proxyValues, in the order they are restored. ProxyEnable is last because a
// half-restored proxy is worse than none: with the server and the bypass list
// back in place, turning it on is the last thing that can go wrong.
var proxyValues = []proxyValue{
	{name: "ProxyServer"},
	{name: "ProxyOverride"},
	{name: "ProxyEnable", dword: true},
}

// SetSystemProxy points the WinINet http proxy at 127.0.0.1:port and enables
// it. <local> plus the loopback literals go in ProxyOverride so the OS does not
// proxy the very ports we are dialing.
//
// dir is ignored here: Windows gives an application a per-user store of its own
// for the settings it is about to overwrite, and proxyBackupKey is that store.
// The macOS build needs the caller's state folder for the same job.
func SetSystemProxy(dir, port string) error {
	return setSystemProxy(internetSettingsKey, proxyBackupKey, port)
}

func setSystemProxy(settingsPath, backupPath, port string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, settingsPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("open internet settings: %w", err)
	}
	defer k.Close()

	// The backup is taken the first time this app takes the settings over, and
	// never again until it hands them back: a second take-over would capture the
	// proxy this app itself wrote, and restoring that on quit leaves the OS
	// pointing at an engine that is no longer running.
	if err := savePreviousProxy(k, backupPath); err != nil {
		return err
	}

	if err := k.SetStringValue("ProxyServer", "127.0.0.1:"+port); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", "<local>;127.0.0.1;localhost"); err != nil {
		return err
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	refreshWinINet()
	return nil
}

// ClearSystemProxy puts back what SetSystemProxy found. With no backup to read -
// a state left by an older build, or a clear nobody asked for - it disables the
// proxy and deletes only the two values this app writes, leaving AutoConfigURL
// and anything else the machine's own administration configured alone.
func ClearSystemProxy(dir string) error {
	return clearSystemProxy(internetSettingsKey, proxyBackupKey)
}

func clearSystemProxy(settingsPath, backupPath string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, settingsPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open internet settings: %w", err)
	}
	defer k.Close()

	b, err := registry.OpenKey(registry.CURRENT_USER, backupPath, registry.READ)
	if err == nil {
		err = restoreProxyValues(k, b)
		b.Close()
		if err != nil {
			return err
		}
		if err := registry.DeleteKey(registry.CURRENT_USER, backupPath); err != nil &&
			!errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("release the saved proxy: %w", err)
		}
	} else if !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("read the saved proxy: %w", err)
	} else {
		// Nothing was saved: fall back to switching the proxy off and dropping the
		// values this app owns. ProxyEnable is set to 0 rather than deleted because
		// Settings renders a missing one as off anyway, and an app reading it as a
		// number would rather be given one.
		if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
			return err
		}
		for _, name := range []string{"ProxyServer", "ProxyOverride"} {
			if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
				return fmt.Errorf("delete %s: %w", name, err)
			}
		}
	}

	// Tell WinINet to reload so already-running browsers drop the proxy.
	refreshWinINet()
	return nil
}

// savePreviousProxy copies the values about to be overwritten into backupPath.
// A value that is not there is not written: "the user had no ProxyServer" has to
// come back as a deletion, not as an empty string Settings will display as a
// configured proxy of no address.
//
// The key itself is created even when nothing is copied, because its existence
// is the marker the restore reads: an Internet Settings with no proxy saves no
// values at all, and "nothing configured" has to be told apart from "never
// saved".
func savePreviousProxy(k registry.Key, backupPath string) error {
	if _, err := registry.OpenKey(registry.CURRENT_USER, backupPath, registry.READ); err == nil {
		return nil // already holding the state worth restoring
	} else if !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("look for a saved proxy: %w", err)
	}
	b, _, err := registry.CreateKey(registry.CURRENT_USER, backupPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("could not save the system proxy to put back: %w", err)
	}
	defer b.Close()
	for _, v := range proxyValues {
		if err := copyExisting(k, b, v); err != nil {
			return err
		}
	}
	return nil
}

// copyExisting saves one value into the backup key, and says nothing when the
// user never had it.
func copyExisting(k, b registry.Key, v proxyValue) error {
	if v.dword {
		n, _, err := k.GetIntegerValue(v.name)
		switch {
		case errors.Is(err, registry.ErrNotExist):
			return nil
		case err != nil:
			return fmt.Errorf("read %s: %w", v.name, err)
		}
		return b.SetDWordValue(v.name, uint32(n))
	}
	s, _, err := k.GetStringValue(v.name)
	switch {
	case errors.Is(err, registry.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("read %s: %w", v.name, err)
	}
	return b.SetStringValue(v.name, s)
}

// restoreProxyValues writes back every value that was saved and deletes every
// value this app owns that was not. The backup key is what decides: a value the
// user had is theirs again, one they did not have goes.
func restoreProxyValues(k, b registry.Key) error {
	for _, v := range proxyValues {
		if v.dword {
			n, _, err := b.GetIntegerValue(v.name)
			switch {
			case errors.Is(err, registry.ErrNotExist):
				if err := k.DeleteValue(v.name); err != nil && !errors.Is(err, registry.ErrNotExist) {
					return fmt.Errorf("delete %s: %w", v.name, err)
				}
				continue
			case err != nil:
				return fmt.Errorf("read the saved %s: %w", v.name, err)
			}
			if err := k.SetDWordValue(v.name, uint32(n)); err != nil {
				return fmt.Errorf("restore %s: %w", v.name, err)
			}
			continue
		}
		s, _, err := b.GetStringValue(v.name)
		switch {
		case errors.Is(err, registry.ErrNotExist):
			if err := k.DeleteValue(v.name); err != nil && !errors.Is(err, registry.ErrNotExist) {
				return fmt.Errorf("delete %s: %w", v.name, err)
			}
			continue
		case err != nil:
			return fmt.Errorf("read the saved %s: %w", v.name, err)
		}
		if err := k.SetStringValue(v.name, s); err != nil {
			return fmt.Errorf("restore %s: %w", v.name, err)
		}
	}
	return nil
}

func refreshWinINet() {
	_, _, _ = internetSetOptionW.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	_, _, _ = internetSetOptionW.Call(0, uintptr(internetOptionRefresh), 0, 0)
}
