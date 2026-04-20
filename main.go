package main

import (
	"embed"
	"fmt"
	"os"

	"deckanator/internal"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "dev"

func main() {
	filterStderr()
	fmt.Fprintf(os.Stderr, "Deckanator %s\n", version)

	a := internal.New()

	err := wails.Run(&options.App{
		Title:  "Deckanator",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 21, A: 1},
		OnStartup: a.Startup,
		Bind: []interface{}{
			a,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
