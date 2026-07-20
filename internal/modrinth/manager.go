package modrinth

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"deckanator/internal/config"
	"deckanator/internal/request"
)

// Global Packs loads datapacks from a global folder for every world,
// including at world creation, which makes worldgen datapacks work
// without the restart-after-creating-a-world dance. When it is
// available for the profile's (loader, game version) it is installed
// alongside the first datapack and takes over pack distribution.
const (
	managerSlug = "globalpacks"
	// The game-dir folder Global Packs loads required datapacks from.
	managerDataDir = "global_packs/required_data"
)

type managerProject struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
}

func fetchManagerProject() (managerProject, error) {
	var p managerProject
	err := request.CachedJSON(apiBase+"/project/"+managerSlug, &p, time.Hour)
	return p, err
}

// ManagerStatus describes the datapack manager mod for a profile.
type ManagerStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Available bool   `json:"available"`
}

func versionForGame(vers []Version, mcVersion string) *Version {
	for i := range vers {
		if mcVersion == "" || slices.Contains(vers[i].GameVersions, mcVersion) {
			return &vers[i]
		}
	}
	return nil
}

// DatapackManagerStatus reports whether the manager mod is installed in
// the profile and whether a build exists for (loader, mcVersion).
func DatapackManagerStatus(profileID, loader, mcVersion string) ManagerStatus {
	st := ManagerStatus{Name: "Global Packs"}
	if loader == "" || loader == loaderVanilla {
		return st
	}
	proj, err := fetchManagerProject()
	if err != nil {
		return st
	}
	st.Name = proj.Title
	if isManagerInstalled(profileID, proj.ID) {
		st.Installed = true
		st.Available = true
		return st
	}
	vers, err := Versions(proj.ID, mcVersion, "mod", loader)
	st.Available = err == nil && versionForGame(vers, mcVersion) != nil
	return st
}

func isManagerInstalled(profileID, projectID string) bool {
	mods, err := List(profileID)
	if err != nil {
		return false
	}
	for _, m := range mods {
		if m.ProjectID == projectID {
			return true
		}
	}
	return false
}

// ensureDatapackManager installs the manager mod into a fabric-like
// profile if a matching build exists and it isn't installed yet.
// Failures are silent: the per-world sync fallback keeps working.
func ensureDatapackManager(profileID, loader, mcVersion string) {
	if loader == "" || loader == loaderVanilla {
		return
	}
	proj, err := fetchManagerProject()
	if err != nil {
		return
	}
	if isManagerInstalled(profileID, proj.ID) {
		return
	}
	vers, err := Versions(proj.ID, mcVersion, "mod", loader)
	if err != nil {
		return
	}
	chosen := versionForGame(vers, mcVersion)
	if chosen == nil {
		return
	}
	file := pickPrimary(chosen.Files)
	if file == nil {
		return
	}
	_ = Install(
		profileID, proj.ID, proj.Title, proj.Description, "mod",
		proj.IconURL, chosen.ID, file.URL, file.Filename,
		loader, mcVersion,
	)
}

// SyncDatapacks distributes the profile's data and resource packs to
// wherever the game will pick them up. With the manager mod installed
// everything goes to its global folders (stale per-world datapack
// copies and options.txt entries are cleaned up so packs don't double
// load); without it datapacks are mirrored into every world and
// resource packs are enabled through options.txt.
func SyncDatapacks(profileID string) {
	proj, err := fetchManagerProject()
	if err == nil && isManagerInstalled(profileID, proj.ID) {
		syncManagerPacks(profileID)
		return
	}
	syncWorldDatapacks(profileID)
	enableResourcePacksInOptions(profileID, trackedResourcePacks(profileID))
}

func syncManagerPacks(profileID string) {
	profileDir := filepath.Join(config.GameDir(), "profiles", profileID)
	mirrorZips(
		filepath.Join(profileDir, "datapacks"),
		filepath.Join(profileDir, managerDataDir),
		func(name string) {
			// Migration: copies previously synced into worlds would
			// load twice next to the manager's global copy.
			removeFromWorlds(profileID, name)
		},
	)
	mirrorZips(
		filepath.Join(profileDir, "resourcepacks"),
		filepath.Join(profileDir, managerResourcesDir),
		func(name string) {
			// Same for packs previously enabled through options.txt.
			disableResourcePackInOptions(profileID, name)
		},
	)
}

func mirrorZips(src, dst string, migrated func(name string)) {
	packs, err := os.ReadDir(src)
	if err != nil || len(packs) == 0 {
		return
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return
	}
	for _, p := range packs {
		if p.IsDir() || !strings.HasSuffix(strings.ToLower(p.Name()), extZip) {
			continue
		}
		if err := copyIfDiffers(filepath.Join(src, p.Name()), filepath.Join(dst, p.Name())); err != nil {
			fmt.Printf("[mods] pack sync %s: %v\n", p.Name(), err)
		}
		migrated(p.Name())
	}
}

func removeFromManagerDir(profileID, filename string) {
	_ = os.Remove(filepath.Join(config.GameDir(), "profiles", profileID, managerDataDir, filename))
}
