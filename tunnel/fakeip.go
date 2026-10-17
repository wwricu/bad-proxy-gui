package tunnel

import (
	"net"
	"strings"
	"sync"
)

// fakePrefixLen is the 198.18.0.0/16 block (RFC 2544 benchmark space, never
// routable) used as a placeholder for a domain. Only this range is routed into
// the tunnel, so "is it a fake address" is the whole capture rule: anything
// the stack sees is a name we already decided to proxy.
var fakeNet = &net.IPNet{IP: net.IPv4(198, 18, 0, 0), Mask: net.CIDRMask(16, 32)}

func isFakeIP(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && fakeNet.Contains(v4)
}

// fakePool hands out fake addresses for queried domains. Apps then connect to
// one of those addresses and the TCP forwarder maps it back to the domain, so
// the core router still gets a name to match its rules against. Without this
// the OS resolver would return real IPs, every domain rule would see a bare
// address, and split routing would collapse to the default outbound.
type fakePool struct {
	mu       sync.Mutex
	byName   map[string]net.IP
	byIP     map[string]string
	host     uint32 // next candidate, 16-bit host part of 198.18.x.x
	reserved map[string]bool
}

func newFakePool() *fakePool {
	return &fakePool{byName: map[string]net.IP{}, byIP: map[string]string{}, host: 1}
}

// Reserve keeps an address out of the pool, so an interface address can share
// the block without being handed to a name.
func (p *fakePool) Reserve(ip net.IP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v4 := ip.To4(); v4 != nil {
		if p.reserved == nil {
			p.reserved = map[string]bool{}
		}
		p.reserved[v4.String()] = true
	}
}

// Assign returns the fake address standing in for name, creating it on first
// use. Callers must have already decided the name is meant to be proxied. Both
// maps are filled under one lock hold, so an address is never handed to a
// second name before the first mapping is visible to Lookup.
func (p *fakePool) Assign(name string) net.IP {
	p.mu.Lock()
	defer p.mu.Unlock()
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if ip, ok := p.byName[name]; ok {
		return ip
	}
	ip := p.nextLocked()
	p.byName[name] = ip
	p.byIP[ip.String()] = name
	return ip
}

// nextLocked walks the 198.18.0.0/16 host space, skipping .0/.255 (broadcast
// and network habits) and any reserved address. The block holds 65k names,
// which is far more than a session resolves, so the scan restarts harmlessly.
func (p *fakePool) nextLocked() net.IP {
	for attempts := 0; attempts < 1<<16; attempts++ {
		p.host++
		if p.host > 0xfffe {
			p.host = 1
		}
		a, b := byte(p.host>>8), byte(p.host&0xff)
		if b == 0 || b == 255 {
			continue
		}
		cand := net.IPv4(198, 18, a, b).String()
		if p.reserved[cand] {
			continue
		}
		return net.IPv4(198, 18, a, b)
	}
	return net.IPv4(198, 18, 0, 1) // unreachable in practice; keeps the API total
}

// Lookup maps a fake address back to the domain it stands for.
func (p *fakePool) Lookup(ip net.IP) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v4 := ip.To4()
	if v4 == nil {
		return "", false
	}
	name, ok := p.byIP[v4.String()]
	return name, ok
}
