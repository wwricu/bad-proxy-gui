package main

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/wwricu/bad-proxy-core/core"
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
// on the local network can use them). The routing mode picks the outbounds and
// router:
//   - proxy:  the active node is the default (empty-tag) outbound; all traffic.
//   - direct: freedom is the default; the node is ignored, all traffic direct.
//   - bypass: freedom is the default, the node is tagged "proxy", and a router
//     sends only blocked domains through it; the rest go direct.
//
// Console logging stays at warn so the engine does not spam stdout from inside
// the GUI.
func buildConfig(s GUIState) (core.Config, error) {
	host := "127.0.0.1"
	if s.AllowLAN {
		host = "0.0.0.0"
	}
	cfg := core.Config{
		General: core.GeneralConfig{ConsoleLevel: "warn"},
		Inbounds: []core.InboundConfig{
			{Protocol: "socks", Host: host, Port: s.SocksPort},
			{Protocol: "http", Host: host, Port: s.HttpPort},
		},
	}
	switch s.RoutingMode {
	case modeDirect:
		cfg.Outbounds = []core.OutboundConfig{freedomOutbound("")}
	case modeBypass:
		node, err := s.activeNode()
		if err != nil {
			return core.Config{}, err
		}
		cfg.Outbounds = []core.OutboundConfig{
			freedomOutbound(""),           // default: direct
			node.outbound(proxyRouterTag), // blocked hosts: the node
		}
		cfg.Router = blockedRouter()
	default: // modeProxy
		node, err := s.activeNode()
		if err != nil {
			return core.Config{}, err
		}
		cfg.Outbounds = []core.OutboundConfig{node.outbound("")}
	}
	return cfg, nil
}

func freedomOutbound(tag string) core.OutboundConfig {
	return core.OutboundConfig{Tag: tag, Protocol: core.FREEDOM}
}

func (n Node) outbound(tag string) core.OutboundConfig {
	return core.OutboundConfig{
		Tag:      tag,
		Protocol: n.Protocol,
		Secret:   n.Secret,
		Host:     n.Server,
		Port:     n.Port,
		Transport: core.TransportConfig{
			Type:      n.Transport,
			Path:      n.Path,
			Authority: n.Authority,
			Tls:       n.TLS,
			Padding:   n.Padding,
		},
	}
}

// testURL is a well-known connectivity probe returning 204 with no body.
const testURL = "http://www.gstatic.com/generate_204"

// dialThroughProxy makes a real request out through the local http inbound and
// returns the round-trip latency. 2xx/204 counts as success; anything else (or
// a transport error) means the tunnel is not actually carrying traffic.
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
