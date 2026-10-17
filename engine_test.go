package main

import "testing"

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
