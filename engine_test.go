package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
)

func nodeState(mode, routing string) GUIState {
	s := defaultState()
	s.ProxyMode = mode
	s.RoutingMode = routing
	s.Nodes = []Node{{Name: "n", Protocol: "vless", Secret: "u", Server: "h", Port: "443"}}
	s.Active = 0
	return s
}

func TestBuildConfigRouting(t *testing.T) {
	// direct mode ignores routing: single freedom default outbound.
	cfg, err := buildConfig(nodeState(modeDirect, modeCNWhitelist))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Outbounds) != 1 || cfg.Outbounds[0].Tag != "" || len(cfg.Router) != 0 {
		t.Errorf("direct mode: got %+v / %+v", cfg.Outbounds, cfg.Router)
	}

	// cn-whitelist: node is default, cn list routes to a tagged direct.
	cfg, err = buildConfig(nodeState(modeProxy, modeCNWhitelist))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Outbounds) != 2 || cfg.Outbounds[0].Tag != "" || cfg.Outbounds[0].Protocol == "freedom" {
		t.Errorf("cn-whitelist: node must stay default, got %+v", cfg.Outbounds)
	}
	if cfg.Outbounds[1].Tag != "direct" || cfg.Outbounds[1].Protocol != "freedom" {
		t.Errorf("cn-whitelist: missing direct freedom outbound, got %+v", cfg.Outbounds)
	}
	if len(cfg.Router) != 1 || cfg.Router[0].Tag != "direct" || len(cfg.Router[0].Rules) != 1 || cfg.Router[0].Rules[0] != "cn" {
		t.Errorf("cn-whitelist: wrong router config %+v", cfg.Router)
	}

	// gfw-blacklist: freedom is default, geolocation-!cn routes to tagged node.
	cfg, err = buildConfig(nodeState(modeProxy, modeGFWBlacklist))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Outbounds[0].Tag != "" || cfg.Outbounds[0].Protocol != "freedom" {
		t.Errorf("gfw-blacklist: default must be freedom, got %+v", cfg.Outbounds)
	}
	if cfg.Outbounds[1].Tag != "proxy" || cfg.Outbounds[1].Protocol != "vless" {
		t.Errorf("gfw-blacklist: node must be tagged proxy, got %+v", cfg.Outbounds)
	}
	if len(cfg.Router) != 1 || cfg.Router[0].Tag != "proxy" || cfg.Router[0].Rules[0] != "geolocation-!cn" {
		t.Errorf("gfw-blacklist: wrong router config %+v", cfg.Router)
	}

	// global: node-only, no routers.
	cfg, err = buildConfig(nodeState(modeProxy, modeGlobal))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Outbounds) != 1 || cfg.Outbounds[0].Protocol != "vless" || len(cfg.Router) != 0 {
		t.Errorf("global: got %+v / %+v", cfg.Outbounds, cfg.Router)
	}

	// routing rules must resolve through the shipped rules.dat path.
	if p := ensureRulesDat(); p == "" {
		t.Error("ensureRulesDat returned no usable path")
	}
}

// TestPortsFreeRefusesAHeldPort is the whole point of checking before core is
// handed the port: a Windows excluded range refuses the bind forever, and a
// probe that starts anyway spends eight seconds waiting for a listener that
// will never arrive, then reports the user's node as the failure.
func TestPortsFreeRefusesAHeldPort(t *testing.T) {
	// Let the OS choose so this never fights another test over a guessed port.
	lower, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := lower.Addr().(*net.TCPAddr).Port
	if portsFree(p) {
		t.Errorf("port %d is bound by this test and still reported free", p)
	}
	lower.Close()

	// The other half of the pair matters as much: socks is the port an earlier
	// probe is most likely to have left holding something.
	upper, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p+1))
	if err != nil {
		t.Skipf("the neighbour of %d was already taken, cannot test that half: %v", p, err)
	}
	if portsFree(p) {
		t.Errorf("port %d is bound by this test and still reported free", p+1)
	}
	upper.Close()

	if !portsFree(p) {
		t.Errorf("%d and %d are released and still reported busy", p, p+1)
	}
}

func TestFreePortPairHandsWithPortsThatCanBeBound(t *testing.T) {
	for i := 0; i < 10; i++ {
		h, s, err := freePortPair()
		if err != nil {
			t.Fatal(err)
		}
		p, err := strconv.Atoi(h)
		if err != nil {
			t.Fatalf("%q is not a port: %v", h, err)
		}
		if p < 20000 || p > 59999 {
			t.Errorf("pair %d is outside the range the probe is allowed to use", p)
		}
		if s != strconv.Itoa(p+1) {
			t.Errorf("socks %q is not the neighbour of http %q", s, h)
		}
		// freePortPair lets go on the way out, so binding here is the same
		// request core makes one step later.
		a, err := net.Listen("tcp", "127.0.0.1:"+h)
		if err != nil {
			t.Errorf("the http port handed out could not be bound: %v", err)
		} else {
			a.Close()
		}
		b, err := net.Listen("tcp", "127.0.0.1:"+s)
		if err != nil {
			t.Errorf("the socks port handed out could not be bound: %v", err)
		} else {
			b.Close()
		}
	}
}

// TestPickFreePortGivesUpOnAPortItCannotBind says what happens when the band is
// exhausted rather than pretending it never is. The span of one makes every
// draw the same port, so this is the hold-and-retry loop with nothing left to
// work around: the probe reports its own problem instead of handing core a port
// it cannot use and then blaming the node for the eight seconds nobody listened.
func TestPickFreePortGivesUpOnAPortItCannotBind(t *testing.T) {
	hold, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	p := hold.Addr().(*net.TCPAddr).Port
	got, err := pickFreePort(p, 1, 3)
	if err == nil {
		t.Fatalf("port %d is bound and pickFreePort handed it out as %d", p, got)
	}
	if !strings.Contains(err.Error(), "no free local port") {
		t.Errorf("gave up for the wrong reason: %v", err)
	}
}
