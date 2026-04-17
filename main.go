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

func dumpMaps() {
	data, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return
	}
	out := "/tmp/deckanator-maps.txt"
	if err := os.WriteFile(out, data, 0644); err == nil {
		fmt.Fprintf(os.Stderr, "maps dumped to %s (%d bytes)\n", out, len(data))
	}
}

func logLibmanette() {
	roots := []string{"/app/lib", "/app/lib64", "/usr/lib/x86_64-linux-gnu", "/usr/lib"}
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if len(name) < 9 || name[:9] != "libmanett" {
				return nil
			}
			info, err := os.Lstat(p)
			if err != nil {
				return nil
			}
			target := ""
			if info.Mode()&os.ModeSymlink != 0 {
				if t, err := os.Readlink(p); err == nil {
					target = " -> " + t
				}
			}
			fmt.Fprintf(os.Stderr, "libmanette: %s (%d bytes)%s\n", p, info.Size(), target)
			return nil
		})
	}
	if v := os.Getenv("LD_LIBRARY_PATH"); v != "" {
		fmt.Fprintf(os.Stderr, "LD_LIBRARY_PATH=%s\n", v)
	}
}

func main() {
	fmt.Fprintf(os.Stderr, "Deckanator %s\n", version)
	logLibmanette()
	dumpMaps()

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
