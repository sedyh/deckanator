package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type ModResult struct {
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	IconURL     string   `json:"icon_url"`
	Downloads   int      `json:"downloads"`
	ProjectType string   `json:"project_type"`
	Categories  []string `json:"categories"`
}

type SearchResponse struct {
	Hits      []ModResult `json:"hits"`
	TotalHits int         `json:"total_hits"`
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
	ProjectID   string `json:"project_id"`
	Filename    string `json:"filename"`
	Title       string `json:"title"`
	VersionID   string `json:"version_id"`
	ProjectType string `json:"project_type,omitempty"`
}

func modsDir(profileID string) string {
	return filepath.Join(GameDir(), "profiles", profileID, "mods")
}

func metaPath(profileID, projectID string) string {
	return filepath.Join(modsDir(profileID), projectID+".modmeta")
}

func SearchMods(query, mcVersion, loader, sortBy string, offset int, showMods, showDatapacks bool) (SearchResponse, error) {
	var facetGroups []string

	if showMods || showDatapacks {
		var typeItems []string
		if showMods {
			typeItems = append(typeItems, `"project_type:mod"`)
		}
		if showDatapacks {
			typeItems = append(typeItems, `"project_type:datapack"`)
		}
		facetGroups = append(facetGroups, "["+strings.Join(typeItems, ",")+"]")
	}

	if mcVersion != "" {
		facetGroups = append(facetGroups, fmt.Sprintf(`["versions:%s"]`, mcVersion))
	}

	if showMods && !showDatapacks && loader != "" {
		facetGroups = append(facetGroups, fmt.Sprintf(`["categories:%s"]`, loader))
	}

	if sortBy == "" {
		if query != "" {
			sortBy = "relevance"
		} else {
			sortBy = "downloads"
		}
	}

	params := url.Values{}
	if query != "" {
		params.Set("query", query)
	}
	if len(facetGroups) > 0 {
		params.Set("facets", "["+strings.Join(facetGroups, ",")+"]")
	}
	params.Set("limit", "20")
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("index", sortBy)

	reqURL := "https://api.modrinth.com/v2/search?" + params.Encode()
	log.Printf("[mods] SearchMods url: %s", reqURL)

	resp, err := http.Get(reqURL)
	if err != nil {
		log.Printf("[mods] SearchMods http error: %v", err)
		return SearchResponse{}, err
	}
	defer resp.Body.Close()

	var raw struct {
		Hits      []ModResult `json:"hits"`
		TotalHits int         `json:"total_hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Printf("[mods] SearchMods decode error: %v", err)
		return SearchResponse{}, err
	}
	log.Printf("[mods] SearchMods hits=%d total=%d", len(raw.Hits), raw.TotalHits)
	return SearchResponse{Hits: raw.Hits, TotalHits: raw.TotalHits}, nil
}

func fetchVersions(projectID string, params url.Values) ([]ModVersion, error) {
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

func GetModVersions(projectID, mcVersion, projectType, loader string) ([]ModVersion, error) {
	actualLoader := loader
	if projectType == "datapack" {
		actualLoader = "datapack"
	}

	params := url.Values{}
	if mcVersion != "" {
		params.Set("game_versions", fmt.Sprintf(`["%s"]`, mcVersion))
	}
	if actualLoader != "" {
		params.Set("loaders", fmt.Sprintf(`["%s"]`, actualLoader))
	}

	versions, err := fetchVersions(projectID, params)
	if err != nil {
		return nil, err
	}

	if len(versions) == 0 && mcVersion != "" {
		fallback := url.Values{}
		if actualLoader != "" {
			fallback.Set("loaders", fmt.Sprintf(`["%s"]`, actualLoader))
		}
		versions, err = fetchVersions(projectID, fallback)
		if err != nil {
			return nil, err
		}
	}

	return versions, nil
}

func installDir(profileID, projectType, filename string) string {
	if projectType == "datapack" && strings.HasSuffix(strings.ToLower(filename), ".zip") {
		return filepath.Join(GameDir(), "profiles", profileID, "datapacks")
	}
	return modsDir(profileID)
}

func InstallMod(profileID, projectID, title, projectType, versionID, downloadURL, filename string) error {
	dir := installDir(profileID, projectType, filename)
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
		ProjectID:   projectID,
		Filename:    filename,
		Title:       title,
		VersionID:   versionID,
		ProjectType: projectType,
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
			dir := installDir(profileID, m.ProjectType, m.Filename)
			os.Remove(filepath.Join(dir, m.Filename))
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
	mods := make([]InstalledMod, 0)
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
