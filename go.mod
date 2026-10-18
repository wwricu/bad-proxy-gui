module github.com/wwricu/bad-proxy-gui

go 1.26.3

require (
	github.com/wwricu/bad-proxy-core v0.0.0
	github.com/xjasonlyu/tun2socks/v2 v2.7.0
	golang.org/x/sys v0.48.0
	golang.zx2c4.com/wireguard v0.0.0-20261006164505-2631ce99a06f
	gvisor.dev/gvisor v0.0.0-20260701204157-69c2d17aea96
)

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
)

require (
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/wailsapp/wails/v3 v3.0.0-beta.31
	golang.org/x/exp v0.0.0-20260611194520-c48552f49976 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
)

replace github.com/wwricu/bad-proxy-core => ../bad-proxy-core
