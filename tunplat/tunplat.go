// Package tunplat owns the operating-system half of a tunnel: the virtual
// network adapter, the route that points the fake-address block at it, and the
// system DNS settings that make name resolution hand out those fake addresses.
//
// Everything here is privileged. Creating the adapter and rewriting the
// machine's resolver need administrator rights on Windows and root on macOS,
// and Up refuses without them rather than half-configuring the host.
//
// Because Up does change something the user set, it hands back a Token: the
// small, serialisable description of what to undo. The caller writes it to
// disk before the tunnel is used and deletes it afterwards, so a process that
// died mid-session can be repaired on the next launch instead of leaving the
// system resolver pointing at a server that no longer exists.
package tunplat

import (
	"errors"
	"net"

	"golang.zx2c4.com/wireguard/tun"
)

// fakeAddress is the tunnel adapter's own address. It sits inside the
// 198.18.0.0/16 fake block so that configuring the adapter installs the one
// route the tunnel needs - the block itself, on-link, with no default route
// anywhere.
var fakeAddress = net.IPv4(198, 18, 0, 1)

const fakePrefixLen = 16

const fakeBlock = "198.18.0.0/16"

// Platform is a configured adapter plus the token that undoes it.
type Platform struct {
	Device    tun.Device
	AdapterIP net.IP
	MTU       int
	// DNSListen is where the tunnel's resolver must run for the system to
	// reach it. Loopback: the tunnel never captures it.
	DNSListen string
	// Forwarders are the DNS servers the system used before Up, so the
	// resolver can keep answering the names the tunnel does not own.
	Forwarders []string
	// AdapterName is the interface as it shows up in the OS list.
	AdapterName string
	// Token is what Recover needs to undo this Up.
	Token map[string]string
}

// Down restores the machine first and the adapter second: the resolver must
// never be left pointing at a tunnel that has already closed.
func (p *Platform) Down() {
	if len(p.Token) > 0 {
		_ = Recover(p.Token)
	}
	p.Device.Close()
}

// ErrUnsupported means this platform has no TUN backend compiled in.
var ErrUnsupported = errors.New("tun: TUN mode is not supported on this platform")
