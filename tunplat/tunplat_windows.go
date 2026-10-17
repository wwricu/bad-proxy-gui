//go:build windows

package tunplat

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/tun"
)

// dnsRegistryKey holds the manually configured DNS servers per adapter. A
// non-empty NameServer means the user set them by hand, so restoring has to
// write them back rather than fall through to DHCP.
const dnsRegistryKey = `HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\`

type winState struct {
	tunIndex  int
	physIndex int
	// servers are the addresses the adapter had before the tunnel took over.
	servers []string
	// fromDHCP records whether those came from the lease, which decides
	// between "clear the override" and "write the list back".
	fromDHCP bool
}

const (
	tokTunIndex  = "tun_index"
	tokPhysIndex = "phys_index"
	tokServers   = "servers"
	tokFromDHCP  = "from_dhcp"
)

// Up creates the wintun adapter, gives it the fake block, and points the
// system resolver at this process.
func Up(name string, mtu int) (*Platform, error) {
	if mtu < 576 || mtu > 1500 {
		mtu = 1500
	}
	if !isElevated() {
		return nil, errors.New("TUN 需要管理员权限：请以管理员身份运行，或改用 System Proxy 模式")
	}
	preloadWintun()
	dev, err := tun.CreateTUN(name, mtu)
	if err != nil {
		return nil, explainCreateErr(err)
	}
	fail := func(step string, err error) error {
		dev.Close()
		return fmt.Errorf("%s: %w", step, err)
	}
	idx, err := waitForAdapter(name)
	if err != nil {
		return nil, fail("adapter did not appear", err)
	}
	if _, err := runPS(fmt.Sprintf(
		"New-NetIPAddress -InterfaceIndex %d -IPAddress %s -PrefixLength %d -AddressFamily IPv4 -ErrorAction Stop | Out-Null",
		idx, fakeAddress.String(), fakePrefixLen)); err != nil {
		return nil, fail("set adapter address", err)
	}
	phys, err := defaultRouteInterface(idx)
	if err != nil {
		return nil, fail("find the active interface", err)
	}
	st := winState{tunIndex: idx, physIndex: phys}
	st.servers, st.fromDHCP, err = currentDNS(phys)
	if err != nil {
		return nil, fail("read the current DNS servers", err)
	}
	if _, err := runPS(fmt.Sprintf(
		"Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses @('127.0.0.1') -ErrorAction Stop", phys)); err != nil {
		return nil, fail("point the system resolver at the tunnel", err)
	}
	_, _ = runPS("Clear-DnsClientCache")
	return &Platform{
		Device:      dev,
		AdapterIP:   fakeAddress,
		MTU:         mtu,
		DNSListen:   "127.0.0.1:53",
		Forwarders:  st.servers,
		AdapterName: name,
		Token:       st.token(),
	}, nil
}

// Recover undoes a previous Up, from its token alone. It also runs at startup
// after a crash, when the adapter of the dead process is long gone: the DNS
// override is the part that outlives it, and the part that must be reverted.
func Recover(tok map[string]string) error {
	st, err := stateFromToken(tok)
	if err != nil {
		return err
	}
	restoreDNS(st.physIndex, st.servers, st.fromDHCP)
	_, _ = runPS("Clear-DnsClientCache")
	_, _ = runPS(fmt.Sprintf(
		"Remove-NetIPAddress -InterfaceIndex %d -IPAddress %s -AddressFamily IPv4 -Confirm:$false -ErrorAction SilentlyContinue",
		st.tunIndex, fakeAddress.String()))
	return nil
}

func (s winState) token() map[string]string {
	dhcp := "0"
	if s.fromDHCP {
		dhcp = "1"
	}
	return map[string]string{
		tokTunIndex:  strconv.Itoa(s.tunIndex),
		tokPhysIndex: strconv.Itoa(s.physIndex),
		tokServers:   strings.Join(s.servers, ","),
		tokFromDHCP:  dhcp,
	}
}

func stateFromToken(tok map[string]string) (winState, error) {
	tunIndex, err := strconv.Atoi(tok[tokTunIndex])
	if err != nil {
		return winState{}, fmt.Errorf("bad tun index in token: %w", err)
	}
	physIndex, err := strconv.Atoi(tok[tokPhysIndex])
	if err != nil {
		return winState{}, fmt.Errorf("bad interface index in token: %w", err)
	}
	st := winState{tunIndex: tunIndex, physIndex: physIndex, fromDHCP: tok[tokFromDHCP] == "1"}
	for _, s := range strings.Split(tok[tokServers], ",") {
		if s = strings.TrimSpace(s); s != "" && net.ParseIP(s) != nil {
			st.servers = append(st.servers, s)
		}
	}
	return st, nil
}

// Supported reports that this build has a TUN backend.
func Supported() bool { return true }

// runPS executes one PowerShell statement and returns its stdout, trimmed.
// -NonInteractive keeps a stray prompt from hanging the app.
func runPS(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	out, err := cmd.Output()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return text, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return text, err
	}
	return text, nil
}

// waitForAdapter polls the interface list until Windows publishes the new
// adapter, which lags CreateTUN by a few hundred milliseconds.
func waitForAdapter(name string) (int, error) {
	for attempt := 0; attempt < 40; attempt++ {
		if ifaces, err := net.Interfaces(); err == nil {
			for _, i := range ifaces {
				if strings.EqualFold(i.Name, name) {
					return i.Index, nil
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return 0, fmt.Errorf("no interface named %s", name)
}

// defaultRouteInterface returns the IPv4 interface that actually carries
// traffic: the lowest-metric default route that is not the tunnel itself.
func defaultRouteInterface(tunIdx int) (int, error) {
	script := fmt.Sprintf(
		"(Get-NetRoute -DestinationPrefix '0.0.0.0/0' -AddressFamily IPv4 -ErrorAction SilentlyContinue"+
			" | Where-Object { $_.InterfaceIndex -ne %d } | Sort-Object RouteMetric | Select-Object -First 1).InterfaceIndex", tunIdx)
	out, err := runPS(script)
	if err != nil {
		return 0, err
	}
	idx, err := strconv.Atoi(out)
	if err != nil || idx <= 0 || idx == tunIdx {
		return 0, fmt.Errorf("no default route found (%q)", out)
	}
	return idx, nil
}

// currentDNS returns the DNS servers in effect on ifIndex plus whether they
// came from DHCP. The flag decides how restoring them has to work.
func currentDNS(ifIndex int) ([]string, bool, error) {
	out, err := runPS(fmt.Sprintf(
		"(Get-DnsClientServerAddress -InterfaceIndex %d -AddressFamily IPv4).ServerAddresses -join ','", ifIndex))
	if err != nil {
		return nil, false, err
	}
	effective := splitAddresses(out)
	guid, err := runPS(fmt.Sprintf(
		"(Get-NetIPInterface -InterfaceIndex %d -AddressFamily IPv4).InterfaceGuid", ifIndex))
	if err != nil {
		return effective, true, err
	}
	guid = strings.Trim(guid, "{}")
	if !guidRe.MatchString(guid) {
		return effective, true, nil
	}
	manual, err := runPS(fmt.Sprintf(
		"(Get-ItemProperty -LiteralPath '%s{%s}' -Name NameServer -ErrorAction SilentlyContinue).NameServer", dnsRegistryKey, guid))
	list := splitAddresses(manual)
	if err != nil || len(list) == 0 {
		// No NameServer value: the addresses above are lease data, so clearing
		// the override is the correct restore.
		return effective, true, nil
	}
	return list, false, nil
}

// splitAddresses turns a comma separated OS list into validated addresses.
// Anything the parser does not recognise is dropped rather than passed on to a
// shell command.
func splitAddresses(out string) []string {
	var list []string
	for _, s := range strings.Split(out, ",") {
		if s = strings.TrimSpace(s); s != "" && net.ParseIP(s) != nil {
			list = append(list, s)
		}
	}
	return list
}

var guidRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// restoreDNS puts back what currentDNS found: the manual list if the user had
// one, otherwise the DHCP-supplied answer.
func restoreDNS(ifIndex int, servers []string, fromDHCP bool) {
	if fromDHCP || len(servers) == 0 {
		_, _ = runPS(fmt.Sprintf(
			"Set-DnsClientServerAddress -InterfaceIndex %d -ResetServerAddresses -ErrorAction SilentlyContinue", ifIndex))
		return
	}
	quoted := make([]string, 0, len(servers))
	for _, s := range servers {
		quoted = append(quoted, "'"+s+"'")
	}
	_, _ = runPS(fmt.Sprintf(
		"Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses @(%s) -ErrorAction SilentlyContinue",
		ifIndex, strings.Join(quoted, ",")))
}

// preloadWintun tries to load wintun.dll from where a release can put it,
// before the loader looks only beside the executable and in System32. Loading
// by absolute path registers the module under its base name, so the driver
// package's own LoadLibrary("wintun.dll") then finds it.
func preloadWintun() {
	for _, dir := range wintunDirs() {
		path := filepath.Clean(filepath.Join(dir, "wintun.dll"))
		if _, err := os.Stat(path); err != nil {
			continue
		}
		const loadLibrarySearchDllLoadDir = 0x00000001
		if _, err := windows.LoadLibraryEx(path, 0, loadLibrarySearchDllLoadDir); err == nil {
			return
		}
	}
}

// wintunDirs lists the places worth looking in: first the folder the portable
// bundle is unzipped into, then the app's own writable directory, which an
// install inside Program Files cannot hold.
func wintunDirs() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if dir, err := appTunDir(); err == nil {
		dirs = append(dirs, dir)
	}
	return dirs
}

// appTunDir is %AppData%\bad-proxy-gui: the writable folder a non-
// administrator install can drop the driver into, and the same one the state
// file lives in.
func appTunDir() (string, error) {
	base := os.Getenv("APPDATA")
	if base == "" {
		return "", errors.New("APPDATA unset")
	}
	return filepath.Join(base, "bad-proxy-gui"), nil
}

// explainCreateErr picks out the two failures a user can actually fix.
func explainCreateErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "The specified module could not be found"),
		strings.Contains(msg, "wintun.dll"):
		return fmt.Errorf("TUN 需要 wintun.dll：请把它放进程序目录或 %%AppData%%\\bad-proxy-gui（便携压缩包已带）- %w", err)
	case strings.Contains(msg, "Access is denied"), strings.Contains(msg, "denied"):
		return fmt.Errorf("TUN 需要管理员权限 - %w", err)
	}
	return fmt.Errorf("create adapter: %w", err)
}

// isElevated reports whether this process already holds its full admin token.
func isElevated() bool {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &tok); err != nil {
		return false
	}
	defer tok.Close()
	return tok.IsElevated()
}
