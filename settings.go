package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Settings is the flat draft the settings window edits. It is deliberately not
// GUIState: the window must not be able to reach the node list, and every field
// here is one the user may change in a single OK.
type Settings struct {
	HttpPort    string `json:"http_port"`
	SocksPort   string `json:"socks_port"`
	ProxyMode   string `json:"proxy_mode"`
	RoutingMode string `json:"routing_mode"`
	AllowLAN    bool   `json:"allow_lan"`
	StartLogin  bool   `json:"start_login"`
	HideTray    bool   `json:"hide_tray"`
}

// checkSettings is the validation both settings windows run. A port the engine
// cannot bind has to be reported while the dialog is still open, not after the
// state file already holds it.
func checkSettings(s Settings) error {
	hp, err := portNumber(s.HttpPort)
	if err != nil {
		return fmt.Errorf("HTTP port: %w", err)
	}
	sp, err := portNumber(s.SocksPort)
	if err != nil {
		return fmt.Errorf("SOCKS port: %w", err)
	}
	if hp == sp {
		return errors.New("the HTTP and SOCKS ports must differ")
	}
	return nil
}

func portNumber(p string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(p))
	if err != nil || n < 1 || n > 65535 {
		return 0, errors.New("must be a number from 1 to 65535")
	}
	return n, nil
}

// SettingsDraft is what the settings window opens with.
func (a *App) SettingsDraft() Settings {
	return Settings{
		HttpPort:    a.st.HttpPort,
		SocksPort:   a.st.SocksPort,
		ProxyMode:   a.st.ProxyMode,
		RoutingMode: a.st.RoutingMode,
		AllowLAN:    a.st.AllowLAN,
		StartLogin:  a.st.StartLogin,
		HideTray:    a.st.HideTray,
	}
}

// ApplySettings commits a draft. It is the only path by which a setting reaches
// the engine, so OK costs exactly one restart and Cancel costs nothing.
func (a *App) ApplySettings(s Settings) error {
	if err := checkSettings(s); err != nil {
		return err
	}
	s.HttpPort = strings.TrimSpace(s.HttpPort)
	s.SocksPort = strings.TrimSpace(s.SocksPort)
	prev := a.st
	next := a.st
	next.HttpPort, next.SocksPort = s.HttpPort, s.SocksPort
	next.ProxyMode, next.RoutingMode = s.ProxyMode, s.RoutingMode
	next.AllowLAN, next.StartLogin, next.HideTray = s.AllowLAN, s.StartLogin, s.HideTray
	next.normalize()

	// Registering with the OS first keeps the persisted flag truthful: a failure
	// here leaves the running proxy exactly as it was.
	if next.StartLogin != prev.StartLogin {
		if err := setStartAtLogin(next.StartLogin); err != nil {
			return fmt.Errorf("start at login failed: %w", err)
		}
	}

	a.st = next
	if err := a.store.save(next); err != nil {
		return err
	}
	if err := a.restartEngine(); err != nil {
		return err
	}
	// The OS proxy is only touched on the edges this app owns, so a direct<->
	// proxy change never disturbs whatever the user configured themselves.
	switch {
	case next.ProxyMode == modeSystem:
		return setSystemProxy(next.HttpPort)
	case prev.ProxyMode == modeSystem:
		return clearSystemProxy()
	}
	return nil
}
