//go:build darwin

package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SystemProxySupported reports that this platform can set and clear the OS proxy.
const SystemProxySupported = true

// backupFileName is where the proxy settings this app is about to overwrite are
// kept, in the app's own state folder. macOS has no per-app registry an
// application may write under, so a file beside state.json is this platform's
// version of the Windows backup key.
const backupFileName = "system-proxy.backup.json"

// ownedProxy is one of the two proxy kinds this app writes: the active
// service's http and https manual proxies. Whatever else a service holds - an
// auto-config (PAC) URL, a SOCKS proxy somebody configured - is not this app's
// to touch. The clear used to switch those off along with its own two, and a
// machine that reaches the network through a PAC came out of the app with no
// working configuration at all.
type ownedProxy struct {
	kind string // name in the backup file
	get  string // verb printing the current settings
	set  string // verb writing server and port
	on   string // verb switching the state on or off
}

var ownedProxies = []ownedProxy{
	{kind: "web", get: "-getwebproxy", set: "-setwebproxy", on: "-setwebproxystate"},
	{kind: "secureweb", get: "-getsecurewebproxy", set: "-setsecurewebproxy", on: "-setsecurewebproxystate"},
}

// savedProxy is one of those kinds as it was found. An empty Server means the
// user had no proxy configured there at all.
type savedProxy struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Port    string `json:"port"`
}

// SetSystemProxy points the active service's http and https proxies at
// 127.0.0.1:port and turns them on, after saving what they were.
//
// dir is this app's own state folder, where the previous settings are kept
// until the clear reads them back.
func SetSystemProxy(dir, port string) error {
	svc, err := activeNetworkService()
	if err != nil {
		return err
	}
	if err := savePreviousProxies(dir, svc); err != nil {
		return err
	}
	for _, p := range ownedProxies {
		if err := networksetup(p.set, svc, "127.0.0.1", port); err != nil {
			return err
		}
		if err := networksetup(p.on, svc, "on"); err != nil {
			return err
		}
	}
	return nil
}

// ClearSystemProxy gives back the two proxy kinds SetSystemProxy wrote.
//
// With no backup to read - an older build took the settings over, or a quit
// that never happened - it switches them off. It cannot empty the server and
// port the way the Windows clear deletes its registry values: networksetup
// documents no way to blank those fields, and an address that is switched off
// carries no traffic.
func ClearSystemProxy(dir string) error {
	svc, err := activeNetworkService()
	if err != nil {
		return err
	}
	saved, err := loadSavedProxies(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range ownedProxies {
		s := saved[p.kind]
		// The address goes back before the switch does: should the second call
		// be the one that fails, an enabled proxy still holding this app's
		// address is the outcome that must not be the one left standing.
		if s.Server != "" {
			if err := networksetup(p.set, svc, s.Server, s.Port); err != nil {
				errs = append(errs, err)
			}
		}
		state := "off"
		if s.Enabled {
			state = "on"
		}
		if err := networksetup(p.on, svc, state); err != nil {
			errs = append(errs, err)
		}
	}
	if err := releaseSavedProxies(dir); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// savePreviousProxies records both kinds as they are now, once. A second
// take-over would otherwise record what this app itself wrote, and the quit
// after it would leave the service pointing at an engine that stopped
// listening.
//
// The file is written even when neither kind was configured, because its
// presence is what tells the restore "this app took these settings over" -
// the same marker the Windows backup key is.
func savePreviousProxies(dir, svc string) error {
	path := filepath.Join(dir, backupFileName)
	if _, err := os.Stat(path); err == nil {
		return nil // already holding the state worth restoring
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("look for a saved proxy: %w", err)
	}
	saved := map[string]savedProxy{}
	for _, p := range ownedProxies {
		out, err := exec.Command("networksetup", p.get, svc).CombinedOutput()
		if err != nil {
			return fmt.Errorf("could not read the proxy to save it: %w: %s",
				err, strings.TrimSpace(string(out)))
		}
		saved[p.kind] = parseProxySettings(string(out))
	}
	b, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not save the system proxy to put back: %w", err)
	}
	return WriteAtomic(path, b)
}

func loadSavedProxies(dir string) (map[string]savedProxy, error) {
	b, err := os.ReadFile(filepath.Join(dir, backupFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // nothing was ever saved: the fallback clear
	}
	if err != nil {
		return nil, fmt.Errorf("read the saved proxy: %w", err)
	}
	var saved map[string]savedProxy
	if err := json.Unmarshal(b, &saved); err != nil {
		// Refusing here is deliberate: without a readable backup the only thing
		// left to do is blank the user's proxy, and that is exactly the loss
		// this file was added to stop.
		return nil, fmt.Errorf("the saved proxy cannot be read: %w", err)
	}
	return saved, nil
}

func releaseSavedProxies(dir string) error {
	err := os.Remove(filepath.Join(dir, backupFileName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("release the saved proxy: %w", err)
	}
	return nil
}

// parseProxySettings reads what networksetup prints for one proxy kind:
//
//	Enabled: Yes
//	Server: 127.0.0.1
//	Port: 7890
//	Authenticated Proxy Enabled: 0
//
// A service with nothing configured prints Enabled: No and an empty Server, so
// a field that is not there reads as "not configured" rather than as a failure
// - which lines up with the fallback in ClearSystemProxy.
func parseProxySettings(out string) savedProxy {
	fields := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return savedProxy{
		Enabled: fields["Enabled"] == "Yes",
		Server:  fields["Server"],
		Port:    fields["Port"],
	}
}

func networksetup(args ...string) error {
	out, err := exec.Command("networksetup", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("networksetup %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// activeNetworkService maps the interface holding the default route to its
// networksetup service name (e.g. en0 -> "Wi-Fi").
func activeNetworkService() (string, error) {
	rt, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return "", fmt.Errorf("default route: %w", err)
	}
	iface := ""
	for _, line := range strings.Split(string(rt), "\n") {
		if idx := strings.Index(line, "interface:"); idx >= 0 {
			iface = strings.TrimSpace(line[idx+len("interface:"):])
			break
		}
	}
	if iface == "" {
		return "", fmt.Errorf("no default interface found")
	}

	ports, err := exec.Command("networksetup", "-listallhardwareports").Output()
	if err != nil {
		return "", fmt.Errorf("list hardware ports: %w", err)
	}
	service := ""
	lastPort := ""
	for _, line := range strings.Split(string(ports), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Hardware Port:"):
			lastPort = strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))
		case strings.HasPrefix(line, "Device:"):
			if strings.TrimSpace(strings.TrimPrefix(line, "Device:")) == iface && lastPort != "" {
				service = lastPort
			}
		}
	}
	if service == "" {
		return "", fmt.Errorf("no service for interface %s", iface)
	}
	return service, nil
}
