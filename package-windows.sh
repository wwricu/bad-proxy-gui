#!/usr/bin/env bash
# Build the portable Windows bundle: one folder holding the exe, rules.dat and
# a .portable marker, zipped so unzipping anywhere gives a working install.
# State persists beside the exe (datadir_windows.go), which is what lets the
# folder be copied to another machine.
#   ./package-windows.sh    ->  dist/bad-proxy-gui-windows-amd64.zip
set -euo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

app="bad-proxy-gui"
rules="rules.dat"
stage="dist/stage/$app"
out="dist/$app-windows-amd64.zip"

prepare_rules() {
    # go:embed rules.dat needs the file to exist at build time, so this must
    # run before wails build. Prefer the fresh upstream dataset; an empty
    # placeholder still compiles and only costs the sealed copy - the portable
    # zip ships rules.dat beside the exe anyway.
    if [ -s "$rules" ]; then
        return 0
    fi
    local url="https://github.com/wwricu/domain-list-community/releases/latest/download/$rules"
    curl -fsSL -o "$rules" "$url" || rm -f "$rules"
    if [ ! -s "$rules" ]; then
        echo "WARNING: $rules unavailable; embedding an empty placeholder" >&2
        : > "$rules"
    fi
}

prepare_rules

rm -rf dist/stage
mkdir -p "$stage"

wails build -platform windows/amd64 2>&1 | tail -3
cp "build/bin/$app.exe" "$stage/"

prepare_rules
if [ -s "$rules" ]; then
    cp "$rules" "$stage/"
fi
: > "$stage/.portable"

rm -f "$out"
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \
    "Compress-Archive -Force -Path '$stage' -DestinationPath '$out'" | tail -2
ls -l "$out"
