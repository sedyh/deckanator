// Package config exposes platform-specific paths and identifiers used
// by the launcher (game data dir, config dir, Mojang's OS keys).
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const (
	goosLinux   = "linux"
	goosWindows = "windows"
)

// GameDir returns the root directory used for Minecraft installations,
// assets, libraries, and per-profile state.
func GameDir() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == goosLinux {
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "deckanator")
		}
		return filepath.Join(home, ".local", "share", "deckanator")
	}
	return filepath.Join(home, "Library", "Application Support", "deckanator")
}

// ConfigDir returns the directory for persistent launcher configuration
// (profiles.json and similar).
func ConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "deckanator")
	}
	return filepath.Join(dir, "deckanator")
}

// OSName returns the OS identifier used inside Mojang version manifests
// ("linux", "osx", "windows").
func OSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "osx"
	case goosWindows:
		return goosWindows
	default:
		return goosLinux
	}
}

// MojangOSKey returns the key used by Mojang's Java runtime manifest to
// identify the current platform and CPU architecture.
func MojangOSKey() string {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "mac-os-arm64"
		}
		return "mac-os"
	case goosWindows:
		switch runtime.GOARCH {
		case "arm64":
			return "windows-arm64"
		case "386":
			return "windows-x86"
		}
		return "windows-x64"
	default:
		return goosLinux
	}
}
