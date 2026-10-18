import { Call } from "/wails/runtime.js";

// v3 has no generated `window.go` namespace: a bound Go method is addressed by
// its full name, which reflect builds from the package path of the App type.
// That path differs between the two builds that exist: a shipped binary is
// package main and registers "main.App.X", while `go test -c` compiles this
// same package as a library under its import path. Both are listed here, in
// shipping order, and the first one the runtime accepts is remembered.
const CANDIDATES = [
    "main.App.",
    "github.com/wwricu/bad-proxy-gui.App.",
];

const missing = Symbol("missing");

async function tryCall(prefix, name, args) {
    try {
        return await Call.ByName(prefix + name, ...args);
    } catch (err) {
        // A method's own error is not a naming problem; it must reach the
        // caller untouched.
        if (String(err && err.message).includes("unknown bound method name")) return missing;
        throw err;
    }
}

let resolved = CANDIDATES[0];

async function call(name, args) {
    let out = await tryCall(resolved, name, args);
    if (out !== missing) return out;
    for (const prefix of CANDIDATES) {
        out = await tryCall(prefix, name, args);
        if (out !== missing) {
            resolved = prefix;
            return out;
        }
    }
    throw new Error(`no binding named "${name}"`);
}

// Wrapping the call once lets the pages keep calling plain method names.
const goApp = new Proxy({}, {
    get: (_target, name) => (...args) => call(name, args),
});

export const API = () => goApp;
