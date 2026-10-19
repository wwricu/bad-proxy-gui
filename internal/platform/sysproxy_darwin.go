//go:build darwin

package platform

import (
	"fmt"
	"os/exec"
	"strings"
)

// SystemProxySupported reports that this platform can set and clear the OS proxy.
const SystemProxySupported = true

// SetSystemProxy points the active service's http and https proxies at
// 127.0.0.1:port and turns them on. networksetup stores the server/port, so
// ClearSystemProxy only flips the state off; setting is what puts our ports back.
func SetSystemProxy(port string) error {
	svc, err := activeNetworkService()
	if err != nil {
		return err
	}
	for _, args := range [][]string{
		{"-setwebproxy", svc, "127.0.0.1", port},
		{"-setwebproxystate", svc, "on"},
		{"-setsecurewebproxy", svc, "127.0.0.1", port},
		{"-setsecurewebproxystate", svc, "on"},
	} {
		if err := networksetup(args...); err != nil {
			return err
		}
	}
	return nil
}

// ClearSystemProxy switches every proxy kind networksetup knows about off for
// the active service. macOS exposes no API to erase the stored server/port, so
// "off" is as close to a wipe as the platform allows; it is what actually stops
// traffic from using a proxy left behind by another program.
func ClearSystemProxy() error {
	svc, err := activeNetworkService()
	if err != nil {
		return err
	}
	for _, args := range [][]string{
		{"-setwebproxystate", svc, "off"},
		{"-setsecurewebproxystate", svc, "off"},
		{"-setsocksfirewallproxystate", svc, "off"},
		{"-setautoproxystate", svc, "off"},
	} {
		if err := networksetup(args...); err != nil {
			return err
		}
	}
	return nil
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
