package main

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/wwricu/bad-proxy-core/core"
	"github.com/wwricu/bad-proxy-core/router"
)

// engine owns one embedded core.Proxy lifecycle. The GUI builds a core.Config
// from the current state and the active node, starts it, and Stop()s it on
// disconnect. A Proxy is single-use, so every start builds a fresh one.
type engine struct {
	proxy *core.Proxy
}

func (e *engine) running() bool { return e.proxy != nil }

func (e *engine) start(s GUIState) error {
	if e.proxy != nil {
		return errors.New("already running")
	}
	cfg, err := buildConfig(s)
	if err != nil {
		return err
	}
	p, err := core.New(cfg)
	if err != nil {
		return err
	}
	p.Start()
	e.proxy = p
	return nil
}

func (e *engine) stop() {
	if e.proxy != nil {
		e.proxy.Stop()
		e.proxy = nil
	}
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
func buildConfig(s GUIState) (core.Config, error) {
	host := "127.0.0.1"
	if s.AllowLAN {
		host = "0.0.0.0"
	}
	outbounds := []core.OutboundConfig{{Tag: "", Protocol: core.FREEDOM}}
	var routers []router.Config
	if s.ProxyMode != modeDirect {
		node, err := s.activeNode()
		if err != nil {
			return core.Config{}, err
		}
		switch s.RoutingMode {
		case modeCNWhitelist:
			outbounds = []core.OutboundConfig{
				node.outbound(""),
				{Tag: "direct", Protocol: core.FREEDOM},
			}
			routers = []router.Config{{Tag: "direct", Rules: []string{"cn"}}}
		case modeGFWBlacklist:
			outbounds = []core.OutboundConfig{
				{Tag: "", Protocol: core.FREEDOM},
				node.outbound("proxy"),
			}
			routers = []router.Config{{Tag: "proxy", Rules: []string{"geolocation-!cn"}}}
		default: // global
			outbounds = []core.OutboundConfig{node.outbound("")}
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
		RouterPath: ensureRulesDat(),
	}, nil
}

func (n Node) outbound(tag string) core.OutboundConfig {
	return core.OutboundConfig{
		Tag:      tag,
		Protocol: n.Protocol,
		Secret:   n.Secret,
		Host:     n.Server,
		Port:     n.Port,
		Transport: core.TransportConfig{
			Type:      encryptedTransport(n.Transport),
			Path:      n.Path,
			Authority: n.Authority,
			// Always on: the client makes no plaintext hop, so xhttp runs over
			// https and there is no user-facing toggle to turn it off.
			Tls:  true,
			Mode: n.Mode,
		},
	}
}

// testURL is a well-known connectivity probe returning 204 with no body.
const testURL = "http://www.gstatic.com/generate_204"

// dialThroughProxy makes a real request out through the local http inbound and
// returns the round-trip latency. 2xx/204 counts as success; anything else (or
// a transport error) means the proxy path is not actually carrying traffic.
func (s GUIState) dialThroughProxy() (time.Duration, error) {
	proxyURL, err := url.Parse("http://127.0.0.1:" + s.HttpPort)
	if err != nil {
		return 0, err
	}
	// route the probe through the local http inbound explicitly, so the test
	// does not depend on the OS-level system proxy being enabled.
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
			return proxyURL, nil
		}},
	}

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
