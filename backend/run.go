// run.go

package backend

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Run builds the App and hands it to Wails. The assets come from main.go
// because go:embed cannot reach outside the directory holding the directive,
// and the frontend build lives at the module root.
//
// Keypress stays resident. Closing the window hides it rather than quitting,
// and the system tray is what keeps the app reachable: a macro can be started
// from the tray menu, or from its global hotkey, with no window on screen at
// all. The only way out is Quit, on the tray menu.
func Run(assets embed.FS, appIcon []byte) error {
	// Create an instance of the app structure
	app := NewApp()

	wailsApp := application.New(application.Options{
		Name:        "Keypress",
		Description: "An auto clicker with a visual flowchart interface",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			// The window being closed is not the end of the app - the tray is
			// still there and macros still run - so terminating on the last
			// closed window would defeat the whole point.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	window := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Keypress",
		Width:            1024,
		Height:           768,
		MinWidth:         380,
		MinHeight:        400,
		BackgroundColour: application.NewRGB(0, 0, 32),
		URL:              "/",
	})

	// Hide rather than close. Cancelling the event is what keeps the process -
	// and with it the runner and every registered hotkey - alive.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	app.attach(wailsApp, window)

	// Hotkeys are claimed up front so a macro is reachable immediately, without
	// the user having to open the window first. Before the tray, not after: the
	// tray menu labels each macro with its hotkey, and it reads them as it
	// builds itself.
	app.syncHotkeys()

	// The tray is built before Run so its menu exists the moment the icon
	// appears; Wails defers the actual creation until the app is running.
	app.tray = newTray(app, appIcon)

	if err := wailsApp.Run(); err != nil {
		log.Printf("Keypress stopped: %v", err)
		return err
	}
	return nil
}
