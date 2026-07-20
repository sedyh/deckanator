package modrinth

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"deckanator/internal/config"
	"deckanator/internal/errs"
)

// The game-dir folder Global Packs force-loads resource packs from.
const managerResourcesDir = "global_packs/required_resources"

func resourcepacksDir(profileID string) string {
	return filepath.Join(config.GameDir(), "profiles", profileID, "resourcepacks")
}

func optionsPath(profileID string) string {
	return filepath.Join(config.GameDir(), "profiles", profileID, "options.txt")
}

// installBundledResourcePacks downloads the required-resource-pack
// files attached to a datapack version (custom textures/models) into
// the profile's resourcepacks folder. Best effort: the datapack itself
// works without them, just with fallback visuals.
func installBundledResourcePacks(profileID, versionID string) []string {
	ver, err := fetchVersionByID(versionID)
	if err != nil {
		return nil
	}
	var names []string
	for _, f := range ver.Files {
		if f.FileType != "required-resource-pack" {
			continue
		}
		if err := downloadToFile(resourcepacksDir(profileID), f.Filename, f.URL); err == nil {
			names = append(names, f.Filename)
		}
	}
	return names
}

func fetchVersionByID(versionID string) (_ Version, e error) {
	resp, err := http.Get(apiBase + "/version/" + versionID)
	if err != nil {
		return Version{}, err
	}
	defer errs.Close(&e, resp.Body)
	var ver Version
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return Version{}, err
	}
	return ver, nil
}

// trackedResourcePacks returns every resource pack file the launcher
// manages: standalone resourcepack projects and datapack bundles.
func trackedResourcePacks(profileID string) []string {
	mods, err := List(profileID)
	if err != nil {
		return nil
	}
	var names []string
	for _, m := range mods {
		if m.ProjectType == typeResourcepack {
			names = append(names, m.Filename)
		}
		names = append(names, m.ResourcePacks...)
	}
	return names
}

// removeResourcePack deletes a resource pack from everywhere the
// launcher may have put it.
func removeResourcePack(profileID, filename string) {
	_ = os.Remove(filepath.Join(resourcepacksDir(profileID), filename))
	_ = os.Remove(filepath.Join(config.GameDir(), "profiles", profileID, managerResourcesDir, filename))
	disableResourcePackInOptions(profileID, filename)
}

// editResourcePackOptions rewrites the resourcePacks list inside the
// profile's options.txt. Minecraft tolerates a partial options.txt, so
// the file is created if the game hasn't run yet.
func editResourcePackOptions(profileID string, mutate func([]string) []string) {
	path := optionsPath(profileID)
	data, _ := os.ReadFile(path)
	lines := strings.Split(string(data), "\n")
	found := false
	for i, l := range lines {
		if !strings.HasPrefix(l, "resourcePacks:") {
			continue
		}
		var packs []string
		_ = json.Unmarshal([]byte(strings.TrimPrefix(l, "resourcePacks:")), &packs)
		updated := mutate(packs)
		if slices.Equal(packs, updated) {
			return
		}
		b, _ := json.Marshal(updated)
		lines[i] = "resourcePacks:" + string(b)
		found = true
		break
	}
	if !found {
		packs := mutate([]string{loaderVanilla})
		b, _ := json.Marshal(packs)
		if len(data) == 0 {
			lines = []string{"resourcePacks:" + string(b), ""}
		} else {
			lines = append(lines, "resourcePacks:"+string(b))
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// enableResourcePacksInOptions makes vanilla load the given packs by
// listing them in options.txt (the manager mod does this by itself).
func enableResourcePacksInOptions(profileID string, filenames []string) {
	if len(filenames) == 0 {
		return
	}
	editResourcePackOptions(profileID, func(packs []string) []string {
		for _, name := range filenames {
			entry := "file/" + name
			if !slices.Contains(packs, entry) {
				packs = append(packs, entry)
			}
		}
		return packs
	})
}

func disableResourcePackInOptions(profileID, filename string) {
	editResourcePackOptions(profileID, func(packs []string) []string {
		return slices.DeleteFunc(packs, func(p string) bool { return p == "file/"+filename })
	})
}
