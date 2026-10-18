#!/usr/bin/env bash
# Build the portable Windows bundle: one folder holding the exe, rules.dat and
# a .portable marker, zipped so unzipping anywhere gives a working install.
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

prepare_rules() {
    # go:embed rules.dat needs the file to exist at build time, so this must
    # run before the build. Prefer the fresh upstream dataset; an empty
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

prepare_syso() {
    # The icon, the version resource and - above all - the per-monitor DPI
    # manifest live in a .syso that the Go linker picks up from the package
    # directory. wails3 generates it from build/windows/ and wails.json; with
    # it missing the exe still runs, it is just blurry on a scaled display.
    local syso="${app}_windows_amd64.syso"
    rm -f "$syso"
    if ! command -v wails3 > /dev/null 2>&1; then
        echo "WARNING: wails3 CLI not found; building without the icon/DPI manifest" >&2
        return 0
    fi
    local rendered="build/windows/info.generated.json"
    if ! render_info "build/windows/info.json" "$rendered"; then
        return 0
    fi
    wails3 generate syso -arch amd64 \
        -icon build/windows/icon.ico \
        -info "$rendered" \
        -manifest build/windows/wails.exe.manifest \
        -out "$syso" || {
        echo "WARNING: could not generate $syso" >&2
        rm -f "$syso"
    }
}

render_info() {
    # build/windows/info.json still uses the v2 {{.Info.*}} placeholders, but
    # v3's CLI compiles that file verbatim: left alone, the exe's version
    # resource reads "{{.Info.ProductVersion}}" in Explorer's Details tab. So
    # fill the placeholders from wails.json first and hand the CLI the result.
    local tmpl="$1" out="$2"
    local product_version company_name product_name copyright
    product_version=$(json_info productVersion)
    company_name=$(json_info companyName)
    product_name=$(json_info productName)
    copyright=$(json_info copyright)
    if [ -z "$product_version" ]; then
        echo "WARNING: no info.productVersion in wails.json; building without a version resource" >&2
        return 1
    fi
    sed -e "s|{{\.Info\.ProductVersion}}|$product_version|g" \
        -e "s|{{\.Info\.CompanyName}}|$company_name|g" \
        -e "s|{{\.Info\.ProductName}}|$product_name|g" \
        -e "s|{{\.Info\.Copyright}}|$copyright|g" \
        -e "s|{{\.Info\.Comments}}||g" \
        "$tmpl" > "$out"
}

json_info() {
    sed -n 's/.*"'$1'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' wails.json | head -1
}

prepare_rules

rm -rf dist/stage
mkdir -p "$stage"

# v3's CLI builds through a Taskfile the app does not need: the frontend is
# plain files that //go:embed already carries, so the build is a go build with
# the two flags that matter - the production tag (without it the asset server is
# a stub) and the windowsgui subsystem (without it a console opens beside the
# window).
prepare_syso
mkdir -p build/bin
go build -tags production -trimpath -ldflags "-s -w -H windowsgui" -o "build/bin/$app.exe" .
cp "build/bin/$app.exe" "$stage/"
cp "build/bin/$app.exe" "$single"

# rules.dat was already ensured before the build; an empty placeholder is not
# worth shipping beside the exe, since the embedded copy is just as empty.
if [ -s "$rules" ]; then
    cp "$rules" "$stage/"
fi
: > "$stage/.portable"

rm -f "$out"
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \
    "Compress-Archive -Force -Path '$stage' -DestinationPath '$out'" | tail -2
ls -l "$out" "$single"
