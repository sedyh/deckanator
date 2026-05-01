// Package internal hosts the Wails-bound App type. It is intentionally
// thin: every method forwards to a subpackage under internal/ or pkg/.
// The package name "internal" is preserved so that Wails-generated
// frontend bindings keep their existing import path (go/internal/App).
package internal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"deckanator/internal/config"
	"deckanator/internal/icons"
	"deckanator/internal/java"
	"deckanator/internal/minecraft"
	"deckanator/internal/modrinth"
	"deckanator/internal/profile"
)

// App is the type bound to the Wails frontend. Methods exported on *App
// are callable from JavaScript.
type App struct {
	ctx context.Context
}

// New returns a zero-value App ready to be handed to Wails.
func New() *App { return &App{} }

// Startup captures the Wails context used for event emission.
func (a *App) Startup(ctx context.Context) { a.ctx = ctx }

// GetProfiles returns all stored profiles.
func (a *App) GetProfiles() []profile.Profile {
	p, _ := profile.Load()
	return p
}

// CreateProfile creates a new profile with defaults and returns it.
func (a *App) CreateProfile() profile.Profile { return profile.Create() }

// SaveProfile persists an existing or new profile.
func (a *App) SaveProfile(p profile.Profile) error { return profile.Save(p) }

// DeleteProfile removes the profile with the given ID and cleans up
// any now-unused game data.
func (a *App) DeleteProfile(id string) error { return profile.Delete(id) }

// CleanGameData wipes the entire game directory.
func (a *App) CleanGameData() error { return profile.CleanGameData() }

// GetIcons returns the curated icon set shown in the profile editor.
func (a *App) GetIcons() []icons.Icon { return icons.All }

// GetVanillaVersions returns all public release versions from Mojang.
func (a *App) GetVanillaVersions() ([]minecraft.VersionEntry, error) {
	return minecraft.FetchVanillaVersions()
}

// GetFabricLoaderVersions returns Fabric loader versions for mcVersion.
func (a *App) GetFabricLoaderVersions(mcVersion string) ([]minecraft.FabricLoaderVersion, error) {
	return minecraft.FetchFabricLoaderVersions(mcVersion)
}

// GetFabricGameVersions returns stable game versions supported by Fabric.
func (a *App) GetFabricGameVersions() ([]string, error) {
	return minecraft.FetchFabricGameVersions()
}

// IsInstalled reports whether the given install is already on disk.
func (a *App) IsInstalled(loader, mcVersion, fabricVersion string) bool {
	return minecraft.IsInstalled(loader, mcVersion, fabricVersion)
}

// Install downloads Minecraft (plus Fabric if requested) and emits
// progress events to the frontend.
func (a *App) Install(loader, mcVersion, fabricVersion, javaComponent string) error {
	return minecraft.Install(
		a.ctx,
		loader, mcVersion, fabricVersion, javaComponent,
		func(component string, p minecraft.ProgressFunc) (string, error) {
			return java.Ensure(component, java.ProgressFunc(p))
		},
		java.Cached,
		a.emitProgress,
	)
}

// OpenProfileDir opens the profile directory in the system file manager.
func (a *App) OpenProfileDir(profileID string) error {
	dir := filepath.Join(config.GameDir(), "profiles", profileID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}

// SearchMods proxies to the Modrinth search API.
func (a *App) SearchMods(query, mcVersion, loader, sortBy string, offset int, showMods, showDatapacks bool) (modrinth.SearchResponse, error) {
	return modrinth.Search(query, mcVersion, loader, sortBy, offset, showMods, showDatapacks)
}

// GetModVersions lists available Modrinth versions for a project.
func (a *App) GetModVersions(projectID, mcVersion, projectType, loader string) ([]modrinth.Version, error) {
	return modrinth.Versions(projectID, mcVersion, projectType, loader)
}

// InstallMod downloads and registers a mod (and its required deps).
func (a *App) InstallMod(profileID, projectID, title, description, projectType, iconURL, versionID, downloadURL, filename string) error {
	return modrinth.Install(profileID, projectID, title, description, projectType, iconURL, versionID, downloadURL, filename)
}

// DeleteMod removes a mod's file and metadata from a profile.
func (a *App) DeleteMod(profileID, projectID string) error {
	return modrinth.Delete(profileID, projectID)
}

// ListMods returns the installed mods for profileID.
func (a *App) ListMods(profileID string) ([]modrinth.Installed, error) {
	return modrinth.List(profileID)
}

// FetchModInfo returns fresh description/icon for a mod.
func (a *App) FetchModInfo(profileID, projectID string) (modrinth.Info, error) {
	return modrinth.FetchInfo(profileID, projectID)
}

// Launch starts Minecraft for the given profile and quits the launcher.
func (a *App) Launch(profileID string) error {
	profiles, err := profile.Load()
	if err != nil {
		return err
	}
	for _, p := range profiles {
		if p.ID != profileID {
			continue
		}
		if err := minecraft.Launch(p, minecraft.LaunchOptions{
			EnsureJava: func(component string, pf minecraft.ProgressFunc) (string, error) {
				return java.Ensure(component, java.ProgressFunc(pf))
			},
		}); err != nil {
			return err
		}
		wailsruntime.Quit(a.ctx)
		return nil
	}
	return fmt.Errorf("profile not found: %s", profileID)
}

func (a *App) emitProgress(stage string, current, total int) {
	wailsruntime.EventsEmit(a.ctx, "install:progress", map[string]any{
		"stage":   stage,
		"current": current,
		"total":   total,
	})
}
