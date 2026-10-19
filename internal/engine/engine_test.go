package engine

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/wwricu/bad-proxy-gui/internal/config"
)

func nodeState(mode, routing string) config.State {
	s := config.DefaultState()
	s.ProxyMode = mode
	s.RoutingMode = routing
	s.Nodes = []config.Node{{Name: "n", Protocol: "vless", Secret: "u", Server: "h", Port: "443"}}
	s.Active = 0
	return s
}

func TestBuildConfigRouting(t *testing.T) {
	// direct mode ignores routing: single freedom default outbound.
	cfg, err := buildConfig(nodeState(config.ModeDirect, config.ModeCNWhitelist), rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Outbounds) != 1 || cfg.Outbounds[0].Tag != "" || len(cfg.Router) != 0 {
		t.Errorf("direct mode: got %+v / %+v", cfg.Outbounds, cfg.Router)
	}

	// cn-whitelist: node is default, cn list routes to a tagged direct.
	cfg, err = buildConfig(nodeState(config.ModeProxy, config.ModeCNWhitelist), rulesPath)
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
	cfg, err = buildConfig(nodeState(config.ModeProxy, config.ModeGFWBlacklist), rulesPath)
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
	cfg, err = buildConfig(nodeState(config.ModeProxy, config.ModeGlobal), rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Outbounds) != 1 || cfg.Outbounds[0].Protocol != "vless" || len(cfg.Router) != 0 {
		t.Errorf("global: got %+v / %+v", cfg.Outbounds, cfg.Router)
	}

	// The dataset path is handed in rather than looked up: this package has no
	// idea where the app keeps its files, and a dataset the app could not find
	// stays the caller's problem to report.
	cfg, err = buildConfig(nodeState(config.ModeProxy, config.ModeGlobal), "injected/rules.dat")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RouterPath != "injected/rules.dat" {
		t.Errorf("RouterPath = %q, want the path handed in", cfg.RouterPath)
	}
}

const rulesPath = "rules.dat"

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

// The always-TLS decision has to be visible in the outbound the engine dials,
// because that is the hop the node sees.
func TestOutboundAlwaysEncrypts(t *testing.T) {
	for _, transport := range []string{"tcp", "tls", "xhttp", ""} {
		n := config.Node{Name: "a", Protocol: "vless", Server: "example.com", Port: "443", Transport: transport}
		tr := outbound(n, "").Transport
		if !tr.Tls {
			t.Errorf("node with transport %q built a hop with Tls=false", transport)
		}
		if tr.Type == "tcp" {
			t.Errorf("node with transport %q still dials raw tcp", transport)
		}
	}
}

// What a "reachable" from the test button is allowed to mean. The two split
// routing modes put a freedom outbound beside the node, so a probe that inherited
// the user's routing could report a node as working after sending its request
// straight out - and the reply would be true of the network, not of the node.
func TestProbeStateCarriesOnlyTheNode(t *testing.T) {
	for _, routing := range []string{config.ModeCNWhitelist, config.ModeGFWBlacklist} {
		st := nodeState(config.ModeSystem, routing)
		p := probeState(st, 0, "29999", "29998")
		if p.ProxyMode != config.ModeProxy {
			t.Errorf("%s: the test engine runs in %s mode", routing, p.ProxyMode)
		}
		cfg, err := buildConfig(p, rulesPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Outbounds) != 1 || cfg.Outbounds[0].Tag != "" {
			t.Errorf("%s: the test engine has more than the node: %+v", routing, cfg.Outbounds)
		}
		if len(cfg.Router) != 0 {
			t.Errorf("%s: the test engine still splits traffic: %+v", routing, cfg.Router)
		}
	}
}

// A restart drops every live connection, so it has to be the change to the
// proxy's own shape that costs it - and nothing else. This is the rule that
// decides whether a click in the node editor or the settings window rebuilds
// the engine.
func TestAffectsEngine(t *testing.T) {
	proxy := config.State{
		SocksPort: "10808", HttpPort: "10809", ProxyMode: config.ModeProxy,
		RoutingMode: config.ModeGlobal, Active: 0,
		Nodes: []config.Node{{Name: "a", Protocol: "vless", Server: "x", Port: "443", Secret: "s", Transport: "tls"}},
	}
	direct := proxy
	direct.ProxyMode = config.ModeDirect
	hideTray := proxy
	hideTray.HideTray = true
	startLogin := proxy
	startLogin.StartLogin = true
	routeChanged := proxy
	routeChanged.RoutingMode = config.ModeCNWhitelist
	nodeEdited := proxy
	nodeEdited.Nodes = append([]config.Node(nil), proxy.Nodes...)
	nodeEdited.Nodes[0].Secret = "other"
	secondNode := proxy
	secondNode.Nodes = append(append([]config.Node(nil), proxy.Nodes...), config.Node{Name: "b"})
	activeMoved := proxy
	activeMoved.Active = 1
	ports := proxy
	ports.HttpPort = "10900"
	lan := proxy
	lan.AllowLAN = true

	for _, tc := range []struct {
		name string
		a, b config.State
		want bool
	}{
		{"nothing at all changed", proxy, proxy, false},
		// The two flags the settings window can move without touching the proxy.
		{"hide tray", proxy, hideTray, false},
		{"start at login", proxy, startLogin, false},
		{"routing", proxy, routeChanged, true},
		{"node edited", proxy, nodeEdited, true},
		{"node added", proxy, secondNode, true},
		{"active node", proxy, activeMoved, true},
		{"port", proxy, ports, true},
		{"bind lan", proxy, lan, true},
		// In direct mode buildConfig never reads the list, so editing it there
		// must not cost the connections that are running straight through.
		{"node edited while direct", direct, func() config.State {
			n := direct
			n.Nodes = append([]config.Node(nil), direct.Nodes...)
			n.Nodes[0].Secret = "other"
			return n
		}(), false},
		{"entering proxy mode", direct, proxy, true},
	} {
		if got := AffectsEngine(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: AffectsEngine = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// core reports a failed inbound bind only to its log: Start is void, so an
// engine handed a port somebody else holds returned no error and carried
// nothing. The pre-flight bind is what turns that into a message the user can
// act on, and it must not fire on a pair the OS just handed out.
func TestStartRefusesAPortItCannotBind(t *testing.T) {
	hold, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	taken := strconv.Itoa(hold.Addr().(*net.TCPAddr).Port)

	st := config.DefaultState() // direct mode: no node, no rules needed
	st.HttpPort = taken
	var e Engine
	err = e.Start(st, "")
	if err == nil {
		e.Stop()
		t.Fatalf("Start said nothing about an inbound on %s that another process holds", taken)
	}
	if !strings.Contains(err.Error(), "in use or reserved") {
		t.Errorf("the failure does not name the port problem: %v", err)
	}
	if e.Running() {
		t.Error("the refused engine claims to be running")
	}

	httpPort, socksPort, err := freePortPair()
	if err != nil {
		t.Fatal(err)
	}
	st.HttpPort, st.SocksPort = httpPort, socksPort
	if err := e.Start(st, ""); err != nil {
		t.Fatalf("Start refused free ports %s/%s: %v", httpPort, socksPort, err)
	}
	if !e.Running() {
		t.Error("a started engine does not report itself running")
	}
	e.Stop()
}
