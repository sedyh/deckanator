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
	"sort"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"deckanator/internal/config"
	"deckanator/internal/icons"
	"deckanator/internal/java"
	"deckanator/internal/mclogs"
	"deckanator/internal/minecraft"
	"deckanator/internal/modrinth"
	"deckanator/internal/profile"
)

// App is the type bound to the Wails frontend. Methods exported on *App
// are callable from JavaScript.
type App struct {
	ctx     context.Context
	version string

	procMu   sync.Mutex
	gameProc *os.Process
}

func (a *App) setGameProc(p *os.Process) {
	a.procMu.Lock()
	a.gameProc = p
	a.procMu.Unlock()
}

// StopGame force-kills the running game process, if any. Used when the
// game hangs and stops responding.
func (a *App) StopGame() {
	a.procMu.Lock()
	p := a.gameProc
	a.procMu.Unlock()
	if p != nil {
		_ = p.Kill()
	}
}

// New returns an App ready to be handed to Wails. version is the build
// identifier injected via -ldflags (tag on CI releases, short commit
// hash on local builds, "dev" otherwise).
func New(version string) *App { return &App{version: version} }

// Startup captures the Wails context used for event emission.
func (a *App) Startup(ctx context.Context) { a.ctx = ctx }

// GetVersion returns the build identifier shown in the UI.
func (a *App) GetVersion() string { return a.version }

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

// GetLoaderVersions returns loader versions of the given fabric-like
// loader (fabric, quilt) available for mcVersion.
func (a *App) GetLoaderVersions(loader, mcVersion string) ([]minecraft.FabricLoaderVersion, error) {
	return minecraft.FetchLoaderVersions(loader, mcVersion)
}

// GetLoaderGameVersions returns stable game versions supported by the
// given fabric-like loader.
func (a *App) GetLoaderGameVersions(loader string) ([]string, error) {
	return minecraft.FetchLoaderGameVersions(loader)
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

// GetLauncherLog returns the full launcher log, for copying complete
// crash output to the clipboard.
func (a *App) GetLauncherLog() string {
	data, err := os.ReadFile(filepath.Join(config.GameDir(), "launcher.log"))
	if err != nil {
		return ""
	}
	return string(data)
}

// InstalledLoaderVersion detects the loader version installed for
// (loader, mcVersion) from the versions directory, or "" if none.
// Recovers profiles whose saved loader version was lost.
func (a *App) InstalledLoaderVersion(loader, mcVersion string) string {
	return minecraft.InstalledLoaderVersion(loader, mcVersion)
}

// AnalyzeCrash runs the most relevant logs through mclo.gs' stateless
// analysis endpoint and returns detected problems with solutions.
// Sources, by recency: the profile's newest crash report, the game's
// latest.log, and the launcher log; the two freshest are analyzed and
// merged, since crash reports and game logs carry the structured
// formats the analyzer knows best.
func (a *App) AnalyzeCrash(profileID string) (mclogs.Analysis, error) {
	sources := crashLogSources(profileID)
	if len(sources) == 0 {
		return mclogs.Analysis{}, fmt.Errorf("no logs found")
	}
	return mclogs.AnalyzeFiles(sources)
}

// crashLogSources returns up to the two freshest crash-relevant logs.
func crashLogSources(profileID string) []string {
	type source struct {
		path string
		mod  int64
	}
	var sources []source
	add := func(path string) {
		if fi, err := os.Stat(path); err == nil {
			sources = append(sources, source{path, fi.ModTime().UnixNano()})
		}
	}
	if profileID != "" {
		add(newestFile(filepath.Join(config.GameDir(), "profiles", profileID, "crash-reports")))
		add(filepath.Join(config.GameDir(), "profiles", profileID, "logs", "latest.log"))
	}
	add(filepath.Join(config.GameDir(), "launcher.log"))
	sort.Slice(sources, func(i, j int) bool { return sources[i].mod > sources[j].mod })
	if len(sources) > 2 {
		sources = sources[:2]
	}
	paths := make([]string, len(sources))
	for i, s := range sources {
		paths[i] = s.path
	}
	return paths
}

// newestFile returns the most recently modified regular file in dir.
func newestFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	newest, newestMod := "", int64(0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if m := info.ModTime().UnixNano(); m > newestMod {
			newest, newestMod = filepath.Join(dir, e.Name()), m
		}
	}
	return newest
}

// CountWorlds returns the number of created worlds in the profile.
func (a *App) CountWorlds(profileID string) int {
	entries, err := os.ReadDir(filepath.Join(config.GameDir(), "profiles", profileID, "saves"))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
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
		// Worlds created since the last sync need the profile's datapacks
		// copied in before the game starts.
		modrinth.SyncWorldDatapacks(profileID)
		if err := minecraft.Launch(p, minecraft.LaunchOptions{
			EnsureJava: func(component string, pf minecraft.ProgressFunc) (string, error) {
				return java.Ensure(component, java.ProgressFunc(pf))
			},
			OnStarted: a.setGameProc,
		}); err != nil {
			a.setGameProc(nil)
			return err
		}
		a.setGameProc(nil)
		// Auto-quit after a clean game exit only on the Deck (Linux),
		// where the launcher hands the Steam session over to the game.
		// On macOS Quit from a binding deadlocks the main thread (the
		// termination sequence waits for the in-flight binding, which is
		// blocked inside Quit), freezing all input — and keeping the
		// launcher open after the game closes is desirable there anyway.
		if runtime.GOOS == "linux" {
			go wailsruntime.Quit(a.ctx)
		}
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
