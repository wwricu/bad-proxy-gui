package main

import (
	"reflect"
	"strings"
	"testing"
)

// The frontend calls bound methods by their full name, and reflect builds that
// name from the package path of the App type. A shipped binary is package main
// ("main.App.X"); a test build compiles the same package under its import path,
// so bridge.js carries both prefixes. This checks that the one reflect gives
// *this* build is among them - a module or type rename fails every call at
// runtime and looks like a broken page.
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
	if !strings.Contains(string(bridge), `"main.`+topic.Name()+`."`) {
		t.Errorf(`bridge.js does not carry the "main.%s." prefix a go build registers`, topic.Name())
	}

	methods := reflect.TypeOf(&App{})
	for _, name := range []string{
		"AddNode", "ApplySettings", "DeleteNode",
		"GetPlatformInfo", "GetState", "SelectNode",
		"SettingsDraft", "ShowContextMenu", "TestNode", "UpdateNode",
	} {
		if _, ok := methods.MethodByName(name); !ok {
			t.Errorf("*App has no method %s, which the frontend calls as %s%s", name, prefix, name)
		}
	}
}
