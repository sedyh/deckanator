package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type ModResult struct {
	ProjectID   string `json:"project_id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	Downloads   int    `json:"downloads"`
}

type ModVersion struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	Files         []struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
		Primary  bool   `json:"primary"`
	} `json:"files"`
}

type InstalledMod struct {
	ProjectID string `json:"project_id"`
	Filename  string `json:"filename"`
	Title     string `json:"title"`
	VersionID string `json:"version_id"`
}

func modsDir(profileID string) string {
	return filepath.Join(GameDir(), "profiles", profileID, "mods")
}

func metaPath(profileID, projectID string) string {
	return filepath.Join(modsDir(profileID), projectID+".modmeta")
}

func SearchMods(query, mcVersion, loader string) ([]ModResult, error) {
	facets := fmt.Sprintf(`[["project_type:mod"],["categories:%s"],["versions:%s"]]`, loader, mcVersion)
	params := url.Values{}
	params.Set("query", query)
	params.Set("facets", facets)
	params.Set("limit", "20")

	resp, err := http.Get("https://api.modrinth.com/v2/search?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Hits []ModResult `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Hits, nil
}

func GetModVersions(projectID, mcVersion, loader string) ([]ModVersion, error) {
	params := url.Values{}
	params.Set("game_versions", fmt.Sprintf(`["%s"]`, mcVersion))
	params.Set("loaders", fmt.Sprintf(`["%s"]`, loader))

	resp, err := http.Get(fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version?%s", projectID, params.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var versions []ModVersion
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, err
	}
	return versions, nil
}

func InstallMod(profileID, projectID, title, versionID, downloadURL, filename string) error {
	dir := modsDir(profileID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	resp, err := http.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dest := filepath.Join(dir, filename)
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}

	meta := InstalledMod{
		ProjectID: projectID,
		Filename:  filename,
		Title:     title,
		VersionID: versionID,
	}
	data, _ := json.Marshal(meta)
	return os.WriteFile(metaPath(profileID, projectID), data, 0644)
}

func DeleteMod(profileID, projectID string) error {
	mods, err := ListMods(profileID)
	if err != nil {
		return err
	}
	for _, m := range mods {
		if m.ProjectID == projectID {
			os.Remove(filepath.Join(modsDir(profileID), m.Filename))
			break
		}
	}
	return os.Remove(metaPath(profileID, projectID))
}

func ListMods(profileID string) ([]InstalledMod, error) {
	dir := modsDir(profileID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []InstalledMod{}, nil
	}
	if err != nil {
		return nil, err
	}
	var mods []InstalledMod
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".modmeta") {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var m InstalledMod
			if err := json.Unmarshal(data, &m); err != nil {
				continue
			}
			mods = append(mods, m)
		}
	}
	return mods, nil
}
