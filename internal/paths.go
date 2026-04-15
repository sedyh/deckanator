package internal

import (
	"os"
	"path/filepath"
	"runtime"
)

func GameDir() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "linux" {
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg != "" {
			return filepath.Join(xdg, "deckanator")
		}
		return filepath.Join(home, ".local", "share", "deckanator")
	}
	return filepath.Join(home, "Library", "Application Support", "deckanator")
}

func ConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "deckanator")
	}
	return filepath.Join(dir, "deckanator")
}
