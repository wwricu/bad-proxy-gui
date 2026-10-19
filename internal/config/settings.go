package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Settings is the flat draft the settings window edits. It is deliberately not
// State: the window must not be able to reach the node list, and every field
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

// CheckSettings is the validation both settings windows run. A port the engine
// cannot bind has to be reported while the dialog is still open, not after the
// state file already holds it.
func CheckSettings(s Settings) error {
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
