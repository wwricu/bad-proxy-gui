package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend
var assets embed.FS

// windowTitle is the native window title; macOS reopen matches the main
// window by it, so it must stay in sync with options.Title.
const windowTitle = "Bad Proxy GUI"

func main() {
	app := &App{}

	err := wails.Run(&options.App{
		Title:  windowTitle,
		Width:  820,
		Height: 500,
		// Keeps the two-panel layout and status bar intact at any size.
		MinWidth:  640,
		MinHeight: 420,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.wwricu.bad-proxy-gui",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				app.showWindow()
			},
		},
		OnStartup: app.startup,
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			if app.trayOn && !app.quitting {
				becomeBackground()
				wailsRuntime.WindowHide(ctx)
				return true
			}
			app.shutdown()
			return false
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
