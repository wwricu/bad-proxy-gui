package engine

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/wwricu/bad-proxy-core/core"
	"github.com/wwricu/bad-proxy-core/router"
	"github.com/wwricu/bad-proxy-gui/internal/config"
)

// An Engine owns one embedded core.Proxy lifecycle. The GUI builds a core.Config
// from the current state and the active node, starts it, and Stop()s it on
// disconnect. A Proxy is single-use, so every start builds a fresh one.
//
// Every method takes the lock, because a binding call runs on a goroutine of its
// own: two clicks that both move the engine have to queue rather than interleave
// a Stop and a Start, which would otherwise meet "already running" from the one
// proxy this app owns - and report it to the user as a failed restart.
type Engine struct {
	mu    sync.Mutex
	proxy *core.Proxy
}

func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.proxy != nil
}

func (e *Engine) Start(s config.State, rulesPath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.start(s, rulesPath)
}

// Restart replaces the running proxy, or starts one if there is none.
func (e *Engine) Restart(s config.State, rulesPath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.proxy != nil {
		e.proxy.Stop()
		e.proxy = nil
	}
	return e.start(s, rulesPath)
}

func (e *Engine) start(s config.State, rulesPath string) error {
	if e.proxy != nil {
		return errors.New("already running")
	}
	if err := checkPorts(s); err != nil {
		return err
	}
	cfg, err := buildConfig(s, rulesPath)
	if err != nil {
		return err
	}
	p, err := core.New(cfg)
	if err != nil {
		return err
	}
	p.Start()
	// Start is not over when it returns: core listens on a goroutine per inbound
	// and reports a failed bind only to its log ("one broken inbound must not
	// take the whole process down"), so Proxy.Start is void and hands back no
	// error at all. Waiting for both ports is what makes the one promise the rest
	// of this file depends on: an engine that started is an engine whose
	// listeners exist, so Stop always has something to close and a restart cannot
	// orphan a listener into a port it will never give back.
	if err := waitReady(s); err != nil {
		p.Stop()
		return err
	}
	e.proxy = p
	return nil
}

// checkPorts binds both inbounds and lets them go again before anything is
// started. It is the only place a taken port can be named: a port a browser
// extension or another proxy already holds is occupied by something that will
// happily accept a connection, so asking afterwards whether the port answers
// would not tell that from core's own listener - and Windows carves slices of the
// dynamic range away for Hyper-V and winnat, where a bind fails permanently.
func checkPorts(s config.State) error {
	for _, in := range s.InboundAddrs() {
		ln, err := net.Listen("tcp", in)
		if err != nil {
			return fmt.Errorf("port %s is in use or reserved - choose another in Settings: %w",
				in, err)
		}
		_ = ln.Close()
	}
	return nil
}

// waitReady blocks until both inbounds answer, or eight seconds have gone by.
// The budget is not for the common case, which is milliseconds, but for the
// machine that is busy: see waitListening.
//
// The dial goes to loopback even when the bind was on 0.0.0.0, because a
// wildcard address is not a destination - the listener behind it answers on
// 127.0.0.1 at the same port.
func waitReady(s config.State) error {
	for _, port := range []string{s.SocksPort, s.HttpPort} {
		if err := waitListening(net.JoinHostPort("127.0.0.1", port)); err != nil {
			return fmt.Errorf("the proxy never came up on port %s: %w", port, err)
		}
	}
	return nil
}

// waitListening returns once something accepts connections on the address, or
// eight seconds after it started.
//
// The budget is not for the common case - a refused connection comes back in
// milliseconds - but for the machine that is busy: core binds its inbounds on
// its own clock, and on a loaded box that is later than anything this loop could
// predict. Two seconds of waiting produced "never came up" on a machine that was
// merely slow, which reads to the user as a broken node.
//
// It is a deadline rather than a count of attempts because the two are not the
// same number: a dial that blocks for its full 250 ms timeout costs five times
// what a refused one costs, and a loop of 160 of those says forty-eight seconds
// while its comment promises eight.
func waitListening(addr string) error {
	deadline := time.Now().Add(8 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			return conn.Close()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("nothing is listening on %s", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.proxy != nil {
		e.proxy.Stop()
		e.proxy = nil
	}
}

// AffectsEngine reports whether the proxy built for next differs from the one
// built for prev, which is the only kind of change a restart can act on. It is
// stated next to buildConfig because it is that function's own shape read
// backwards: in direct mode no node and no routing mode reaches the config, so
// editing the list there must not cost a restart - a restart drops every live
// connection, and a click in the node editor is not a reason to lose them.
func AffectsEngine(prev, next config.State) bool {
	if prev.SocksPort != next.SocksPort || prev.HttpPort != next.HttpPort ||
		prev.AllowLAN != next.AllowLAN || prev.ProxyMode != next.ProxyMode {
		return true
	}
	if next.ProxyMode == config.ModeDirect {
		return false
	}
	return prev.RoutingMode != next.RoutingMode || prev.Active != next.Active ||
		len(prev.Nodes) != len(next.Nodes) || !sameNodes(prev.Nodes, next.Nodes)
}

func sameNodes(a, b []config.Node) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// buildConfig turns GUI state into the engine's config. Inbounds are fixed
// socks+http, bound to loopback unless AllowLAN is on (then 0.0.0.0 so devices
// on the local network can use them). The proxy mode picks the base setup:
//   - direct: a built-in freedom outbound; traffic leaves straight out. This is
//     always valid, even with zero nodes, so the engine can run from launch.
//   - proxy: the active node; requires a selected node. The routing mode then
//     splits traffic by domain lists from rules.dat:
//   - global:        everything through the node.
//   - cn-whitelist:  mainland (geosite cn) domains direct, the rest via node.
//   - gfw-blacklist: non-mainland (geolocation-!cn) domains via the node, the
//     rest direct — the classic "bypass GFW" split.
//
// Routing is domain-based only: the core router matches the destination host,
// so raw-IP connections always take the default outbound. Console logging
// stays at warn so the engine does not spam stdout.
func buildConfig(s config.State, rulesPath string) (core.Config, error) {
	host := s.InboundHost()
	outbounds := []core.OutboundConfig{{Tag: "", Protocol: core.FREEDOM}}
	var routers []router.Config
	if s.ProxyMode != config.ModeDirect {
		node, err := s.ActiveNode()
		if err != nil {
			return core.Config{}, err
		}
		switch s.RoutingMode {
		case config.ModeCNWhitelist:
			outbounds = []core.OutboundConfig{
				outbound(node, ""),
				{Tag: "direct", Protocol: core.FREEDOM},
			}
			routers = []router.Config{{Tag: "direct", Rules: []string{"cn"}}}
		case config.ModeGFWBlacklist:
			outbounds = []core.OutboundConfig{
				{Tag: "", Protocol: core.FREEDOM},
				outbound(node, "proxy"),
			}
			routers = []router.Config{{Tag: "proxy", Rules: []string{"geolocation-!cn"}}}
		default: // global
			outbounds = []core.OutboundConfig{outbound(node, "")}
		}
	}
	return core.Config{
		General: core.GeneralConfig{ConsoleLevel: "warn"},
		Inbounds: []core.InboundConfig{
			{Protocol: "socks", Host: host, Port: s.SocksPort},
			{Protocol: "http", Host: host, Port: s.HttpPort},
		},
		Outbounds:  outbounds,
		Router:     routers,
		RouterPath: rulesPath,
	}, nil
}

func outbound(n config.Node, tag string) core.OutboundConfig {
	return core.OutboundConfig{
		Tag:      tag,
		Protocol: n.Protocol,
		Secret:   n.Secret,
		Host:     n.Server,
		Port:     n.Port,
		Transport: core.TransportConfig{
			Type:      config.EncryptedTransport(n.Transport),
			Path:      n.Path,
			Authority: n.Authority,
			// Always on: the client makes no plaintext hop, so xhttp and http2
			// run over tls and there is no user-facing toggle to turn it off.
			Tls:  true,
			Mode: n.Mode,
		},
	}
}

// testURL is a well-known connectivity probe returning 204 with no body.
const testURL = "http://www.gstatic.com/generate_204"

// httpTestURL is a plain-HTTP (no TLS) page that answers 200 with a body. It is
// the captive-portal probe Mozilla ships in Firefox: fetching it through the
// proxy proves not just that the tunnel is up but that ordinary web traffic
// reaches the open internet and comes back. It is used over the famous
// neverssl.com because that one took 4.2 s from the server against this page's
// 0.3 s, and a probe that answers slowly reads as a broken node.
const httpTestURL = "http://detectportal.firefox.com/canonical.html"

// proxyClient routes a request through the local http inbound on the given port,
// independent of the OS system proxy. It is a one-shot client: the caller is
// done with it after the answer and closes its idle connections.
func proxyClient(httpPort string) *http.Client {
	proxyURL, err := url.Parse("http://127.0.0.1:" + httpPort)
	if err != nil {
		return nil
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
			return proxyURL, nil
		}},
	}
}

// dialThroughProxy makes a real request out through the local http inbound and
// returns the round-trip latency. 2xx/204 counts as success; anything else (or
// a transport error) means the proxy path is not actually carrying traffic.
func DialThroughProxy(s config.State) (time.Duration, error) {
	client := proxyClient(s.HttpPort)
	if client == nil {
		return 0, errors.New("bad http port")
	}
	// The transport holds the connection it used, waiting for a request that a
	// one-shot client will never make.
	defer client.CloseIdleConnections()

	start := time.Now()
	resp, err := client.Get(testURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return elapsed, errors.New("proxy returned " + resp.Status)
	}
	return elapsed, nil
}

// dialHTTPThroughProxy fetches a real web page through the local http inbound and
// requires a 2xx carrying a non-empty body, so a tunnel that comes up but cannot
// actually carry web traffic fails here. It returns the round-trip latency.
func DialHTTPThroughProxy(s config.State) (time.Duration, error) {
	client := proxyClient(s.HttpPort)
	if client == nil {
		return 0, errors.New("bad http port")
	}
	defer client.CloseIdleConnections()

	start := time.Now()
	resp, err := client.Get(httpTestURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return elapsed, errors.New("proxy returned " + resp.Status)
	}
	// A 200 with an empty body is a broken answer, not a loaded page.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return elapsed, err
	}
	if len(body) == 0 {
		return elapsed, errors.New("page came back empty")
	}
	return elapsed, nil
}
