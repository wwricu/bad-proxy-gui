package engine

import (
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"time"

	"github.com/wwricu/bad-proxy-gui/internal/config"
)

// Probe is one way of asking whether traffic actually gets out through a
// node's local inbound: the 204 ping, or a real page.
type Probe func(config.State) (time.Duration, error)

// ProbeNode starts a throwaway engine with index as its only outbound on random
// high ports, runs probe through that engine's http inbound, and stops it. The
// running proxy is untouched: the throwaway engine cannot collide with it and a
// failing node cannot take the app down.
//
// The port is bound here first, before core is handed it. Windows carves large
// slices of the dynamic range away for Hyper-V and winnat, and a bind into one
// of those is a permanent "access forbidden" that core writes to its log and
// then starts the rest of the engine around: start() returns no error, nothing
// ever listens, and the user is told their node timed out. Asking the OS for
// the port turns that into a different port instead of a false verdict about
// somebody's network.
func ProbeNode(st config.State, index int, rulesPath string, probe Probe) (time.Duration, error) {
	if index < 0 || index >= len(st.Nodes) {
		return 0, fmt.Errorf("invalid index: %d", index)
	}
	httpPort, socksPort, err := freePortPair()
	if err != nil {
		return 0, err
	}
	st = probeState(st, index, httpPort, socksPort)
	var e Engine
	if err := e.Start(st, rulesPath); err != nil {
		return 0, fmt.Errorf("start test engine: %w", err)
	}
	defer e.Stop()
	return probe(st)
}

// probeState is the state a test engine runs: one node, on ports this machine
// just handed back, carrying everything.
//
// Global routing is the part that is not a convenience. The two split modes put a
// freedom outbound beside the node, so the test engine would send some
// destinations out directly - and report a node that never carried the probe as
// reachable. The probe asks one question about one node, and the user's routing
// is not part of it.
func probeState(st config.State, index int, httpPort, socksPort string) config.State {
	st.ProxyMode = config.ModeProxy
	st.RoutingMode = config.ModeGlobal
	st.Active = index
	st.HttpPort = httpPort
	st.SocksPort = socksPort
	return st
}

// freePortPair returns two adjacent loopback ports this machine will let us
// bind, released again so core can have them. A leftover listener from an
// earlier probe holding one is as good a reason to move on as a reserved range.
func freePortPair() (httpPort, socksPort string, err error) {
	p, err := pickFreePort(20000, 40000, 8)
	if err != nil {
		return "", "", err
	}
	return strconv.Itoa(p), strconv.Itoa(p + 1), nil
}

// pickFreePort draws up to attempts ports from [start, start+span) and returns
// the first that can be bound along with its neighbour.
func pickFreePort(start, span, attempts int) (int, error) {
	for i := 0; i < attempts; i++ {
		if p := start + rand.Intn(span); portsFree(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free local port for the test engine")
}

// portsFree reports whether p and p+1 can both be bound right now. This is the
// only question worth asking about a Windows excluded port range: a port inside
// one never binds, however long the caller is willing to wait for it.
func portsFree(p int) bool {
	a, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		return false
	}
	defer a.Close()
	b, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p+1))
	if err != nil {
		return false
	}
	b.Close()
	return true
}
