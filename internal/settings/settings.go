// Package settings persists launcher preferences to the config dir.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"

	"deckanator/internal/config"
)

// Settings holds user-tunable launcher behavior.
type Settings struct {
	// CloseAfterLaunch quits the launcher shortly after the game starts
	// successfully (the Steam Deck flow). When off, the launcher stays
	// open, waits for the game to exit and reports crashes whenever
	// they happen.
	CloseAfterLaunch bool `json:"closeAfterLaunch"`
	// MemoryMinMB sets the initial JVM heap (-Xms); zero keeps the JVM
	// default.
	MemoryMinMB int `json:"memoryMinMb"`
	// MemoryMaxMB caps the JVM heap (-Xmx); zero keeps the JVM default.
	MemoryMaxMB int `json:"memoryMaxMb"`
	// Fullscreen starts the game with --fullscreen.
	Fullscreen bool `json:"fullscreen"`
}

func defaults() Settings {
	return Settings{CloseAfterLaunch: true}
}

func path() string {
	return filepath.Join(config.ConfigDir(), "settings.json")
}

// Load reads settings from disk, falling back to defaults.
func Load() Settings {
	s := defaults()
	data, err := os.ReadFile(path())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	return s
}

// Save persists settings to disk.
func Save(s Settings) error {
	if err := os.MkdirAll(config.ConfigDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), data, 0o644)
}
