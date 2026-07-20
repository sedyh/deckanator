package main

import (
	"embed"
	"fmt"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"deckanator/internal"
	"deckanator/internal/outfilter"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "dev"

func main() {
	outfilter.Install()
	fmt.Fprintf(os.Stderr, "Deckanator %s\n", version)

	a := internal.New(version)

	// The Deck's screen is exactly 1280x800: in KDE desktop mode a window
	// that size ends up behind the taskbar. Maximising instead lets the
	// WM fit us to the work area; the UI scales itself to the viewport.
	startState := options.Normal
	if runtime.GOOS == "linux" {
		startState = options.Maximised
	}

	err := wails.Run(&options.App{
		Title:            "Deckanator",
		Width:            1280,
		Height:           800,
		WindowStartState: startState,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 21, A: 1},
		OnStartup:        a.Startup,
		Bind: []any{
			a,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err.Error())
	}
}
