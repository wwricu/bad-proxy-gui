# Bad Proxy GUI

Desktop client for bad-proxy-core: an HTTP and SOCKS5 proxy on your own machine,
with a domain routing list, driving one or more outbound nodes.

## Install

* **Windows** — `bad-proxy-gui-windows-amd64.exe` installs, the `.zip` is the
  same build unpacked and movable (it carries a `.portable` marker).
* **macOS 11 or newer** — `bad-proxy-gui-darwin-universal.zip` holds a universal
  (arm64 + x86_64) `.app`. It is ad-hoc signed and not notarised, so on first
  open either right-click → Open, or clear the flag up front:
  `xattr -dr com.apple.quarantine "Bad Proxy.app"`.

## This build

* Settings and the node editor are their own windows now, sized to the form, so
  neither one needs a scrollbar and neither covers the list it is talking about.
* Fix: the macOS build aborted the moment a form window opened. The window was
  being bound to the main window with a selector AppKit never published.
* A form window stays above the window that called it, and the list rereads the
  state when the form closes — by Cancel, by Save, or by the title-bar cross.
* Only one form window at a time: a second request raises the open one and the
  operating system asks for attention on its own instead of stacking another
  editor over a state it would each write back stale.
* Fix: the editor scrolled on a Windows display at 100% scaling and the
  settings window left a band of empty window under its buttons on macOS. A
  field and a popup are drawn by the operating system, so each platform's forms
  are now measured on the platform they run on.
* The main window opens 640 px wide rather than 800; the row menu offers Edit
  before Test; the macOS menu-bar item is a template image now, so it takes the
  bar's own colour instead of showing the app icon's white tile.

## Known limitations

* Not signed with a developer certificate, so a fresh download needs the
  right-click Open or the `xattr` step above.
* On macOS a form window cannot hide on its own while the list is on screen:
  AppKit keeps a child window with its parent, so hiding the app takes both
  down and restoring brings both back.
* Routing rules come from the domain-list dataset sealed into the build; there
  is no in-app rule editor.
