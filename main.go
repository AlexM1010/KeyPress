//main.go

package main

import (
	"embed"

	"Keypress/backend"
)

//go:embed all:frontend/build
var assets embed.FS

func main() {
	if err := backend.Run(assets); err != nil {
		println("Error:", err.Error())
	}
}
