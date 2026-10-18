package main

import (
	"reflect"
	"strings"
	"testing"
)

// The frontend calls bound methods by their full name, and reflect builds that
// name from the package path of the App type. A rename of the module or of a
// method fails every call at runtime and looks like a broken page, so both
// halves of the name are checked here against what the page actually ships.
func TestBindingNamesMatchTheFrontend(t *testing.T) {
	topic := reflect.TypeOf(App{})
	prefix := topic.PkgPath() + "." + topic.Name() + "."

	bridge, err := bundled.ReadFile("frontend/src/bridge.js")
	if err != nil {
		t.Fatalf("reading the frontend bridge: %v", err)
	}
	if !strings.Contains(string(bridge), `"`+prefix+`"`) {
		t.Errorf("bridge.js does not address bindings with %q", prefix)
	}

	methods := reflect.TypeOf(&App{})
	for _, name := range []string{
		"AddNode", "AddNodeUI", "ApplySettings", "ConfirmDialog", "DeleteNode",
		"EditNodeUI", "GetPlatformInfo", "GetState", "SelectNode",
		"SettingsDraft", "SettingsUI", "ShowContextMenu", "TestNode", "UpdateNode",
	} {
		if _, ok := methods.MethodByName(name); !ok {
			t.Errorf("*App has no method %s, which the frontend calls as %s%s", name, prefix, name)
		}
	}
}
