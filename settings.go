package main

import (
	"errors"
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
		st.HttpPort, st.SocksPort = s.HttpPort, s.SocksPort
		st.ProxyMode, st.RoutingMode = s.ProxyMode, s.RoutingMode
		st.AllowLAN, st.StartLogin, st.HideTray = s.AllowLAN, s.StartLogin, s.HideTray
		return nil
	})
	if err != nil {
		return err
	}

	// Everything below is a side effect of a draft that is already saved. None of
	// it belongs inside the edit: the edit is rolled back whole or not at all, and
	// an OS registration made before the save succeeds leaves the file and the
	// machine telling different stories - which is what the login item did here
	// until a failed save was the one that came back.
	//
	// They are collected rather than returned one at a time because they are
	// independent: a refused port is no reason to leave the OS proxy pointing at
	// the engine that just stopped.
	var problems []error

	restarted := true
	if err := a.restartIfChanged(prev, next); err != nil {
		problems = append(problems, err)
		restarted = false
	}

	// The OS proxy is only touched on the edges this app owns, so a direct<->
	// proxy change never disturbs whatever the user configured themselves.
	switch {
	case next.ProxyMode == config.ModeSystem && restarted:
		if err := platform.SetSystemProxy(a.store.Dir(), next.HttpPort); err != nil {
			problems = append(problems, err)
		}
	case next.ProxyMode == config.ModeSystem || prev.ProxyMode == config.ModeSystem:
		// Either the draft gave the system mode up, or it took it and the engine
		// it points at never came up. Both mean the same danger: every browser on
		// the machine is still sending its traffic to a port with nothing
		// listening, and nothing else undoes it - shutdown clears the proxy only
		// when the saved mode is still system, which a draft that moved away from
		// it just stopped being.
		if err := platform.ClearSystemProxy(a.store.Dir()); err != nil {
			problems = append(problems, err)
		}
	}

	if next.StartLogin != prev.StartLogin {
		if err := platform.SetStartAtLogin(next.StartLogin); err != nil {
			problems = append(problems, fmt.Errorf("start at login: %w", err))
		}
	}
	return errors.Join(problems...)
}

// SettingsUI opens the settings window. That window commits its own draft on
// OK, so a confirmed draft has already reached the engine and a dismissed one
// has not - either way there is nothing to collect here, and a refusal is read
// on the window that raised it.
func (a *App) SettingsUI() {
	a.openSettingsWindow()
}
