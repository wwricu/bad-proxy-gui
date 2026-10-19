package main

import (
	"fmt"
	"strings"

	"github.com/wwricu/bad-proxy-gui/internal/config"
	"github.com/wwricu/bad-proxy-gui/internal/platform"
)

// SettingsDraft is what the settings window opens with.
func (a *App) SettingsDraft() config.Settings {
	st := a.snapshot()
	return config.Settings{
		HttpPort:    st.HttpPort,
		SocksPort:   st.SocksPort,
		ProxyMode:   st.ProxyMode,
		RoutingMode: st.RoutingMode,
		AllowLAN:    st.AllowLAN,
		StartLogin:  st.StartLogin,
		HideTray:    st.HideTray,
	}
}

// ApplySettings commits a draft. It is the only path by which a setting reaches
// the engine, so OK costs at most one restart and Cancel costs nothing.
func (a *App) ApplySettings(s config.Settings) error {
	if err := config.CheckSettings(s); err != nil {
		return err
	}
	s.HttpPort = strings.TrimSpace(s.HttpPort)
	s.SocksPort = strings.TrimSpace(s.SocksPort)

	prev, next, err := a.edit(func(st *config.State) error {
		// Asking the OS before the draft is saved keeps the persisted flag
		// truthful: a refusal aborts the whole edit, so neither the file nor the
		// running proxy moves.
		if st.StartLogin != s.StartLogin {
			if err := platform.SetStartAtLogin(s.StartLogin); err != nil {
				return fmt.Errorf("start at login failed: %w", err)
			}
		}
		st.HttpPort, st.SocksPort = s.HttpPort, s.SocksPort
		st.ProxyMode, st.RoutingMode = s.ProxyMode, s.RoutingMode
		st.AllowLAN, st.StartLogin, st.HideTray = s.AllowLAN, s.StartLogin, s.HideTray
		return nil
	})
	if err != nil {
		return err
	}
	if err := a.restartIfChanged(prev, next); err != nil {
		return err
	}
	// The OS proxy is only touched on the edges this app owns, so a direct<->
	// proxy change never disturbs whatever the user configured themselves.
	switch {
	case next.ProxyMode == config.ModeSystem:
		return platform.SetSystemProxy(next.HttpPort)
	case prev.ProxyMode == config.ModeSystem:
		return platform.ClearSystemProxy()
	}
	return nil
}

// SettingsUI opens the settings window. That window commits its own draft on
// OK, so a confirmed draft has already reached the engine and a dismissed one
// has not - either way there is nothing to collect here, and a refusal is read
// on the window that raised it.
func (a *App) SettingsUI() {
	a.openSettingsWindow()
}
