import { Call } from "/wails/runtime.js";

// v3 has no generated `window.go` namespace: a bound Go method is addressed by
// its full name at call time. The prefix is the package path of the App type,
// which for a main package in this module is the module path itself, not
// "main" — the wrong prefix fails every call and looks like a broken page.
const APP_BINDING = "github.com/wwricu/bad-proxy-gui.App.";

// Wrapping the call once lets the pages keep calling plain method names.
const goApp = new Proxy({}, {
    get: (_target, name) => (...args) => Call.ByName(APP_BINDING + name, ...args),
});

export const API = () => goApp;
