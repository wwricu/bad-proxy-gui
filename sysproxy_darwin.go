//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// systemProxySupported reports that this platform can toggle the OS proxy.
const systemProxySupported = true

// applySystemProxy toggles the macOS system proxy for the active network
// service via networksetup, pointing web/HTTPS at the local HTTP listener and
// SOCKS at the local SOCKS listener.
func applySystemProxy(enable bool, s GUIState) error {
	svc, err := activeNetworkService()
	if err != nil {
		return err
	}
	if !enable {
		for _, args := range [][]string{
			{"-setwebproxystate", svc, "off"},
			{"-setsecurewebproxystate", svc, "off"},
			{"-setsocksfirewallproxystate", svc, "off"},
		} {
			if err := networksetup(args...); err != nil {
				return err
			}
		}
		return nil
	}

	for _, args := range [][]string{
		{"-setwebproxy", svc, "127.0.0.1", s.HttpPort},
		{"-setsecurewebproxy", svc, "127.0.0.1", s.HttpPort},
		{"-setsocksfirewallproxy", svc, "127.0.0.1", s.SocksPort},
	} {
		if err := networksetup(args...); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"-setwebproxystate", svc, "on"},
		{"-setsecurewebproxystate", svc, "on"},
		{"-setsocksfirewallproxystate", svc, "on"},
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
