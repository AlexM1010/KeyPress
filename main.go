// main.go

package main

import (
	"embed"
	"log"

	"Keypress/backend"
)

//go:embed all:frontend/build
var assets embed.FS

// appIcon is the icon the system tray shows. It is a PNG rather than the
// build/windows/icon.ico used for the executable itself: every platform's tray
// implementation decodes an image, and only Windows understands .ico.
//
//go:embed build/appicon.png
var appIcon []byte

func main() {
	if err := backend.Run(assets, appIcon); err != nil {
		log.Fatalf("Keypress exited with an error: %v", err)
	}
}
