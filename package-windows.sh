#!/usr/bin/env bash
# Build the portable Windows bundle: one folder holding the exe, rules.dat,
# wintun.dll and a .portable marker, zipped so unzipping anywhere gives a
# working install.
# State persists beside the exe (datadir_windows.go), which is what lets the
# folder be copied to another machine.
# Also emits the lone exe: same binary without the .portable marker, so it
# keeps the embedded rules.dat and stores state in %AppData% instead.
#   ./package-windows.sh    ->  dist/bad-proxy-gui-windows-amd64.zip
#                               dist/bad-proxy-gui-windows-amd64.exe
set -euo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

app="bad-proxy-gui"
rules="rules.dat"
stage="dist/stage/$app"
out="dist/$app-windows-amd64.zip"
single="dist/$app-windows-amd64.exe"

# wintun.dll is the signed driver the TUN adapter is built on. It is not part
# of Windows, so the portable bundle has to carry it. Pinned by hash because
# the file runs inside a process that holds administrator rights.
wintun_url="https://www.wintun.net/builds/wintun-0.14.1.zip"
wintun_sha256="07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51"

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

prepare_wintun() {
    # Ship the signed amd64 driver beside the exe. A download failure only
    # costs TUN mode - the rest of the app still builds, so warn and carry on.
    local zip="dist/wintun-0.14.1.zip"
    if [ ! -s "$zip" ] || ! echo "$wintun_sha256  $zip" | sha256sum -c --status -; then
        curl -fsSL -o "$zip" "$wintun_url" || {
            echo "WARNING: wintun.dll unavailable; the bundle will not do TUN" >&2
            rm -f "$zip"
            return 0
        }
        if ! echo "$wintun_sha256  $zip" | sha256sum -c --status -; then
            echo "ERROR: wintun.zip checksum mismatch - refusing to ship it" >&2
            rm -f "$zip"
            return 1
        fi
    fi
    local x="dist/wintun-unpack"
    rm -rf "$x"
    powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \
        "Expand-Archive -Force -Path '$zip' -DestinationPath '$x'" > /dev/null
    cp "$x/wintun/bin/amd64/wintun.dll" "$stage/" || {
        echo "WARNING: could not extract wintun.dll" >&2
        return 0
    }
    rm -rf "$x"
}

prepare_rules

rm -rf dist/stage
mkdir -p "$stage"

wails build -platform windows/amd64 2>&1 | tail -3
cp "build/bin/$app.exe" "$stage/"
cp "build/bin/$app.exe" "$single"

prepare_rules
if [ -s "$rules" ]; then
    cp "$rules" "$stage/"
fi
prepare_wintun
: > "$stage/.portable"

rm -f "$out"
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \
    "Compress-Archive -Force -Path '$stage' -DestinationPath '$out'" | tail -2
ls -l "$out" "$single"
