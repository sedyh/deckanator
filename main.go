package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"deckanator/internal"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "dev"

func logLibmanette() {
	for _, dir := range []string{"/app/lib", "/app/lib64", "/usr/lib/x86_64-linux-gnu", "/usr/lib"} {
		matches, err := filepath.Glob(filepath.Join(dir, "libmanette*"))
		if err != nil || len(matches) == 0 {
			continue
		}
		for _, m := range matches {
			info, err := os.Lstat(m)
			if err != nil {
				continue
			}
			target := ""
			if info.Mode()&os.ModeSymlink != 0 {
				if t, err := os.Readlink(m); err == nil {
					target = " -> " + t
				}
			}
			fmt.Fprintf(os.Stderr, "libmanette: %s (%d bytes)%s\n", m, info.Size(), target)
		}
	}
}

func main() {
	fmt.Fprintf(os.Stderr, "Deckanator %s\n", version)
	logLibmanette()

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
