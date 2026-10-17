//go:build darwin

package tunplat

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"golang.zx2c4.com/wireguard/tun"
)

type macState struct {
	// service is the networksetup name that owns the active interface's DNS.
	service string
	// iface is the utun device, block the route that was added.
	iface string
	// servers/automatic mirror what dnsServers found before the override.
	servers   []string
	automatic bool
}

const (
	tokService   = "service"
	tokIface     = "iface"
	tokServers   = "servers"
	tokAutomatic = "automatic"
)

// Up allocates a utun interface, routes the fake block into it and points the
// active network service at this process' resolver. macOS hands out utun only
// to root, so everything below runs with the privileges the process already
// has or refuses to start.
func Up(name string, mtu int) (*Platform, error) {
	if mtu < 576 || mtu > 1500 {
		mtu = 1500
	}
	if os.Geteuid() != 0 {
		return nil, errors.New("TUN 需要 root：请用 sudo 启动，或改用 System Proxy 模式")
	}
	dev, err := tun.CreateTUN("utun", mtu)
	if err != nil {
		return nil, fmt.Errorf("create utun: %w", err)
	}
	fail := func(step string, err error) error {
		dev.Close()
		return fmt.Errorf("%s: %w", step, err)
	}
	ifName, err := dev.Name()
	if err != nil {
		return nil, fail("name adapter", err)
	}
	// utun has no link layer, so the address is point-to-point with itself as
	// the peer and a /32; the block arrives through an interface route.
	if out, err := run("ifconfig", ifName, "inet", fakeAddress.String(), fakeAddress.String(),
		"netmask", "255.255.255.255", "mtu", fmt.Sprint(mtu), "up"); err != nil {
		return nil, fail("address "+ifName+": "+out, err)
	}
	if out, err := run("route", "-q", "add", "-net", fakeBlock, "-interface", ifName); err != nil {
		return nil, fail("route "+fakeBlock+": "+out, err)
	}
	service, err := activeNetworkService()
	if err != nil {
		return nil, fail("find the active network service", err)
	}
	st := macState{service: service, iface: ifName}
	st.servers, st.automatic, err = dnsServers(service)
	if err != nil {
		return nil, fail("read the current DNS servers", err)
	}
	if out, err := run("networksetup", "-setdnsservers", service, "127.0.0.1"); err != nil {
		return nil, fail("point the system resolver at the tunnel: "+out, err)
	}
	flushResolverCache()
	return &Platform{
		Device:      dev,
		AdapterIP:   fakeAddress,
		MTU:         mtu,
		DNSListen:   "127.0.0.1:53",
		Forwarders:  st.servers,
		AdapterName: ifName,
		Token:       st.token(),
	}, nil
}

// Recover undoes a previous Up from its token, including the case where the
// process that did it is gone and only the DNS override survives.
func Recover(tok map[string]string) error {
	service := tok[tokService]
	if service == "" {
		return errors.New("tunplat: token has no network service")
	}
	var servers []string
	for _, s := range strings.Split(tok[tokServers], ",") {
		if s = strings.TrimSpace(s); s != "" && net.ParseIP(s) != nil {
			servers = append(servers, s)
		}
	}
	if tok[tokAutomatic] == "1" || len(servers) == 0 {
		_, _ = run("networksetup", "-setdnsservers", service, "Empty")
	} else {
		args := append([]string{"-setdnsservers", service}, servers...)
		_, _ = run("networksetup", args...)
	}
	flushResolverCache()
	if iface := tok[tokIface]; iface != "" {
		_, _ = run("route", "-q", "delete", "-net", fakeBlock, "-interface", iface)
		_, _ = run("ifconfig", iface, "inet", fakeAddress.String(), "delete")
	}
	return nil
}

func (s macState) token() map[string]string {
	automatic := "0"
	if s.automatic {
		automatic = "1"
	}
	return map[string]string{
		tokService:   s.service,
		tokIface:     s.iface,
		tokServers:   strings.Join(s.servers, ","),
		tokAutomatic: automatic,
	}
}

// Supported reports that this build has a TUN backend.
func Supported() bool { return true }

func run(cmd string, args ...string) (string, error) {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// activeNetworkService maps the interface carrying the default route to the
// networksetup service name that owns its DNS configuration.
func activeNetworkService() (string, error) {
	out, err := run("route", "-n", "get", "default")
	if err != nil {
		return "", err
	}
	m := ifaceRe.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("no default interface in %q", out)
	}
	hw, err := run("networksetup", "-listallhardwareports")
	if err != nil {
		return "", err
	}
	var port string
	for _, line := range strings.Split(hw, "\n") {
		if p := portRe.FindStringSubmatch(line); p != nil {
			port = strings.TrimSpace(p[1])
			continue
		}
		if d := deviceRe.FindStringSubmatch(line); d != nil && d[1] == m[1] && port != "" {
			return port, nil
		}
	}
	return "", fmt.Errorf("no network service owns %s", m[1])
}

var (
	ifaceRe  = regexp.MustCompile(`interface:\s*(\S+)`)
	portRe   = regexp.MustCompile(`Hardware Port:\s*(.+)`)
	deviceRe = regexp.MustCompile(`Device:\s*(\S+)`)
)

// dnsServers returns the configured servers for a service and whether the
// service takes them from DHCP, which networksetup reports as an empty list.
func dnsServers(service string) ([]string, bool, error) {
	out, err := run("networksetup", "-getdnsservers", service)
	if err != nil {
		return nil, false, err
	}
	var list []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); isIPAddress(line) {
			list = append(list, line)
		}
	}
	return list, len(list) == 0, nil
}

func isIPAddress(s string) bool { return net.ParseIP(s) != nil }

func flushResolverCache() {
	_, _ = run("dscacheutil", "-flushcache")
	_, _ = run("killall", "-HUP", "mDNSResponder")
}
