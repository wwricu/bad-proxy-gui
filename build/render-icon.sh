#!/usr/bin/env bash
# Rebuild build/appicon.png from build/appicon.svg.
#
# The png is what everything else consumes: package-windows.sh feeds
# build/windows/icon.ico to `wails3 generate syso`, CI turns the same png into
# the macOS .icns, and the tray embeds one of the two. So after editing the svg
# run this script, then repackage. Requires the wails3 CLI for the .ico step.
#
# Edge in headless mode is used as the rasteriser because it is the one SVG
# renderer guaranteed on a Windows box; the page background is asked for as
# fully transparent so the rounded corners stay rounded on the taskbar.
set -euo pipefail
cd "$(dirname "$0")/.."

edge=""
for candidate in \
    "/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe" \
    "/c/Program Files/Microsoft/Edge/Application/msedge.exe"; do
    if [ -x "$candidate" ]; then edge="$candidate"; break; fi
done
if [ -z "$edge" ]; then
    echo "msedge.exe not found; build/appicon.png left untouched" >&2
    exit 1
fi

svg="file:///$(cygpath -m "$PWD/build/appicon.svg")"
png="$(cygpath -w "$PWD/build/appicon.png")"

"$edge" --headless=new --disable-gpu --hide-scrollbars \
    --default-background-color=00000000 --force-device-scale-factor=1 \
    --window-size=1024,1024 --screenshot="$png" "$svg"

echo "rendered $png"

# The .ico the Windows build embeds (taskbar, title bar, tray). The CLI always
# writes a macOS icon too and CI does not use it - the mac job builds its .icns
# from appicon.png with sips - so that half of the output goes to a scratch
# file that is deleted straight after.
mac_scratch="$(mktemp -d)/icon.icns"
wails3 generate icons -input build/appicon.png \
    -windowsfilename build/windows/icon.ico -macfilename "$mac_scratch"
rm -f "$mac_scratch"
echo "wrote build/windows/icon.ico"

