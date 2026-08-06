// run.go

package backend

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// Run builds the App and hands it to Wails. The assets come from main.go
// because go:embed cannot reach outside the directory holding the directive,
// and the frontend build lives at the module root.
func Run(assets embed.FS) error {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	return wails.Run(&options.App{
		Title:     "Keypress",
		Width:     1024,
		Height:    768,
		MinWidth:  380,
		MinHeight: 400,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 32, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})
}
