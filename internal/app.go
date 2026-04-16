package internal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context
}

func New() *App {
	return &App{}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) GetProfiles() []Profile {
	profiles, _ := LoadProfiles()
	return profiles
}

func (a *App) CreateProfile() Profile {
	return CreateProfile()
}

func (a *App) SaveProfile(p Profile) error {
	return SaveProfile(p)
}

func (a *App) DeleteProfile(id string) error {
	return DeleteProfile(id)
}

func (a *App) CleanGameData() error {
	return CleanGameData()
}

func (a *App) GetIcons() []IconDef {
	return Icons
}

func (a *App) GetVanillaVersions() ([]VersionEntry, error) {
	return FetchVanillaVersions()
}

func (a *App) GetFabricLoaderVersions(mcVersion string) ([]FabricLoaderVersion, error) {
	return FetchFabricLoaderVersions(mcVersion)
}

func (a *App) GetFabricGameVersions() ([]string, error) {
	return FetchFabricGameVersions()
}

func (a *App) IsInstalled(loader, mcVersion, fabricVersion string) bool {
	return IsInstalled(loader, mcVersion, fabricVersion)
}

func (a *App) Install(loader, mcVersion, fabricVersion, javaComponent string) error {
	return Install(a.ctx, loader, mcVersion, fabricVersion, javaComponent, func(stage string, current, total int) {
		runtime.EventsEmit(a.ctx, "install:progress", map[string]interface{}{
			"stage":   stage,
			"current": current,
			"total":   total,
		})
	})
}

func (a *App) OpenProfileDir(profileID string) error {
	dir := filepath.Join(GameDir(), "profiles", profileID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}

func (a *App) SearchMods(query, mcVersion, loader string) ([]ModResult, error) {
	return SearchMods(query, mcVersion, loader)
}

func (a *App) GetModVersions(projectID, mcVersion, loader string) ([]ModVersion, error) {
	return GetModVersions(projectID, mcVersion, loader)
}

func (a *App) InstallMod(profileID, projectID, title, versionID, downloadURL, filename string) error {
	return InstallMod(profileID, projectID, title, versionID, downloadURL, filename)
}

func (a *App) DeleteMod(profileID, projectID string) error {
	return DeleteMod(profileID, projectID)
}

func (a *App) ListMods(profileID string) ([]InstalledMod, error) {
	return ListMods(profileID)
}

func (a *App) Launch(profileID string) error {
	profiles, err := LoadProfiles()
	if err != nil {
		return err
	}
	for _, p := range profiles {
		if p.ID == profileID {
			if err := Launch(p); err != nil {
				return err
			}
			runtime.Quit(a.ctx)
			return nil
		}
	}
	return fmt.Errorf("profile not found: %s", profileID)
}
