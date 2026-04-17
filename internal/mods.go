package internal

import (
	"crypto/sha1"
	"encoding/hex"
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

type ModDependency struct {
	ProjectID      string `json:"project_id"`
	VersionID      string `json:"version_id"`
	DependencyType string `json:"dependency_type"`
}

type ModVersion struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	VersionNumber string          `json:"version_number"`
	GameVersions  []string        `json:"game_versions"`
	Loaders       []string        `json:"loaders"`
	ProjectID     string          `json:"project_id"`
	Dependencies  []ModDependency `json:"dependencies"`
	Files         []struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Primary  bool              `json:"primary"`
		Hashes   map[string]string `json:"hashes"`
	} `json:"files"`
}

type InstalledMod struct {
	ProjectID   string `json:"project_id"`
	Filename    string `json:"filename"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	VersionID   string `json:"version_id"`
	ProjectType string `json:"project_type,omitempty"`
	IconURL     string `json:"icon_url,omitempty"`
	Hash        string `json:"hash,omitempty"`
}

func modsDir(profileID string) string {
	return filepath.Join(GameDir(), "profiles", profileID, "mods")
}

func metaDir(profileID string) string {
	return filepath.Join(GameDir(), "profiles", profileID, ".meta")
}

func metaPath(profileID, projectID string) string {
	return filepath.Join(metaDir(profileID), projectID+".meta")
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

func fileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func downloadAndSave(dir, filename, downloadURL string) error {
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
	return os.Rename(tmp, dest)
}

func InstallMod(profileID, projectID, title, description, projectType, iconURL, versionID, downloadURL, filename string) error {
	dir := installDir(profileID, projectType, filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(metaDir(profileID), 0755); err != nil {
		return err
	}

	if err := downloadAndSave(dir, filename, downloadURL); err != nil {
		return err
	}

	meta := InstalledMod{
		ProjectID:   projectID,
		Filename:    filename,
		Title:       title,
		Description: description,
		VersionID:   versionID,
		ProjectType: projectType,
		IconURL:     iconURL,
		Hash:        fileHash(filepath.Join(dir, filename)),
	}
	data, _ := json.Marshal(meta)
	if err := os.WriteFile(metaPath(profileID, projectID), data, 0644); err != nil {
		return err
	}

	return installDependencies(profileID, versionID)
}

func installDependencies(profileID, versionID string) error {
	resp, err := http.Get("https://api.modrinth.com/v2/version/" + versionID)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var ver ModVersion
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return err
	}

	installed, _ := ListMods(profileID)
	installedSet := make(map[string]bool)
	for _, m := range installed {
		installedSet[m.ProjectID] = true
	}

	for _, dep := range ver.Dependencies {
		if dep.DependencyType != "required" || dep.ProjectID == "" {
			continue
		}
		if installedSet[dep.ProjectID] {
			continue
		}
		log.Printf("[mods] installing dependency %s for version %s", dep.ProjectID, versionID)

		depVerID := dep.VersionID
		var depVer ModVersion
		if depVerID != "" {
			r, err := http.Get("https://api.modrinth.com/v2/version/" + depVerID)
			if err != nil {
				continue
			}
			json.NewDecoder(r.Body).Decode(&depVer)
			r.Body.Close()
		} else {
			// get latest version of dep project
			params := url.Values{}
			for _, ldr := range ver.Loaders {
				params.Set("loaders", fmt.Sprintf(`["%s"]`, ldr))
				break
			}
			r, err := http.Get("https://api.modrinth.com/v2/project/" + dep.ProjectID + "/version?" + params.Encode())
			if err != nil {
				continue
			}
			var vers []ModVersion
			json.NewDecoder(r.Body).Decode(&vers)
			r.Body.Close()
			if len(vers) == 0 {
				continue
			}
			depVer = vers[0]
		}

		file := func() *struct {
			URL      string            `json:"url"`
			Filename string            `json:"filename"`
			Primary  bool              `json:"primary"`
			Hashes   map[string]string `json:"hashes"`
		} {
			for i := range depVer.Files {
				if depVer.Files[i].Primary {
					return &depVer.Files[i]
				}
			}
			if len(depVer.Files) > 0 {
				return &depVer.Files[0]
			}
			return nil
		}()
		if file == nil {
			continue
		}

		// fetch project info for title/icon/description
		var proj struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			IconURL     string `json:"icon_url"`
		}
		if r, err := http.Get("https://api.modrinth.com/v2/project/" + dep.ProjectID); err == nil {
			json.NewDecoder(r.Body).Decode(&proj)
			r.Body.Close()
		}

		dir := installDir(profileID, "mod", file.Filename)
		_ = os.MkdirAll(dir, 0755)
		_ = os.MkdirAll(metaDir(profileID), 0755)
		if err := downloadAndSave(dir, file.Filename, file.URL); err != nil {
			log.Printf("[mods] dep download error: %v", err)
			continue
		}
		m := InstalledMod{
			ProjectID:   dep.ProjectID,
			Filename:    file.Filename,
			Title:       proj.Title,
			Description: proj.Description,
			VersionID:   depVer.ID,
			ProjectType: "mod",
			IconURL:     proj.IconURL,
			Hash:        fileHash(filepath.Join(dir, file.Filename)),
		}
		data, _ := json.Marshal(m)
		_ = os.WriteFile(metaPath(profileID, dep.ProjectID), data, 0644)
		log.Printf("[mods] installed dependency %s (%s)", proj.Title, dep.ProjectID)
	}
	return nil
}

type ModInfo struct {
	IconURL     string `json:"icon_url"`
	Description string `json:"description"`
}

func FetchModInfo(profileID, projectID string) (ModInfo, error) {
	resp, err := http.Get("https://api.modrinth.com/v2/project/" + projectID)
	if err != nil {
		return ModInfo{}, err
	}
	defer resp.Body.Close()

	var proj struct {
		IconURL     string `json:"icon_url"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&proj); err != nil {
		return ModInfo{}, err
	}

	metaFile := metaPath(profileID, projectID)
	data, err := os.ReadFile(metaFile)
	if err == nil {
		var m InstalledMod
		if json.Unmarshal(data, &m) == nil {
			changed := false
			if proj.IconURL != "" && m.IconURL != proj.IconURL {
				m.IconURL = proj.IconURL
				changed = true
			}
			if proj.Description != "" && m.Description != proj.Description {
				m.Description = proj.Description
				changed = true
			}
			if changed {
				updated, _ := json.Marshal(m)
				_ = os.WriteFile(metaFile, updated, 0644)
			}
		}
	}

	return ModInfo{IconURL: proj.IconURL, Description: proj.Description}, nil
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

func nameFromFilename(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	// strip after '+' (e.g. +mc1.21.1)
	if i := strings.Index(name, "+"); i > 0 {
		name = name[:i]
	}
	// strip version suffix: last '-X...' segment where X is a digit
	for {
		i := strings.LastIndex(name, "-")
		if i < 0 {
			break
		}
		rest := name[i+1:]
		if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			name = name[:i]
		} else {
			break
		}
	}
	return name
}

func lookupVersionByHash(hash string) (*ModVersion, error) {
	resp, err := http.Get("https://api.modrinth.com/v2/version_file/" + hash + "?algorithm=sha1")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	var ver ModVersion
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return nil, err
	}
	return &ver, nil
}

func recoverOrphanedMods(profileID string) {
	metaDirPath := metaDir(profileID)

	trackedHashes := make(map[string]bool)
	trackedFiles := make(map[string]bool)
	knownIDs := make(map[string]bool)
	if entries, err := os.ReadDir(metaDirPath); err == nil {
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".meta") && !strings.HasSuffix(e.Name(), ".modmeta") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(metaDirPath, e.Name()))
			if err != nil {
				continue
			}
			var m InstalledMod
			if json.Unmarshal(data, &m) == nil {
				if m.Hash != "" {
					trackedHashes[m.Hash] = true
				}
				if m.Filename != "" {
					trackedFiles[m.Filename] = true
				}
				if m.ProjectID != "" {
					knownIDs[m.ProjectID] = true
				}
			}
		}
	}

	dirs := []struct {
		dir  string
		exts []string
	}{
		{modsDir(profileID), []string{".jar"}},
		{filepath.Join(GameDir(), "profiles", profileID, "datapacks"), []string{".zip"}},
	}

	for _, d := range dirs {
		entries, err := os.ReadDir(d.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			hasExt := false
			for _, x := range d.exts {
				if ext == x {
					hasExt = true
					break
				}
			}
			if !hasExt {
				continue
			}
			if trackedFiles[e.Name()] {
				continue
			}

			filePath := filepath.Join(d.dir, e.Name())
			hash := fileHash(filePath)

			if hash != "" && trackedHashes[hash] {
				continue
			}

			projectType := "mod"
			if ext == ".zip" {
				projectType = "datapack"
			}

			var meta *InstalledMod

			if hash != "" {
				ver, err := lookupVersionByHash(hash)
				if err == nil && ver != nil && ver.ProjectID != "" && !knownIDs[ver.ProjectID] {
					// fetch project info for title/icon/description
					var proj struct {
						Title       string `json:"title"`
						Description string `json:"description"`
						IconURL     string `json:"icon_url"`
						ProjectType string `json:"project_type"`
					}
					if r, err := http.Get("https://api.modrinth.com/v2/project/" + ver.ProjectID); err == nil {
						json.NewDecoder(r.Body).Decode(&proj)
						r.Body.Close()
					}
					if proj.ProjectType != "" {
						projectType = proj.ProjectType
					}
					meta = &InstalledMod{
						ProjectID:   ver.ProjectID,
						Filename:    e.Name(),
						Title:       proj.Title,
						Description: proj.Description,
						VersionID:   ver.ID,
						ProjectType: projectType,
						IconURL:     proj.IconURL,
						Hash:        hash,
					}
				}
			}

			if meta == nil {
				continue
			}
			if knownIDs[meta.ProjectID] {
				continue
			}
			if err := os.MkdirAll(metaDirPath, 0755); err == nil {
				data, _ := json.Marshal(meta)
				_ = os.WriteFile(filepath.Join(metaDirPath, meta.ProjectID+".meta"), data, 0644)
				if hash != "" {
					trackedHashes[hash] = true
				}
				trackedFiles[e.Name()] = true
				knownIDs[meta.ProjectID] = true
				log.Printf("[mods] recovered orphan: %s → %s (%s)", e.Name(), meta.Title, meta.ProjectID)
			}
		}
	}
}

func deduplicateMetas(profileID string) {
	dir := metaDir(profileID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type entry struct {
		name string
		mod  InstalledMod
	}

	readEntries := make([]entry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".meta") && !strings.HasSuffix(e.Name(), ".modmeta")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m InstalledMod
		if json.Unmarshal(data, &m) != nil || m.Filename == "" {
			continue
		}
		readEntries = append(readEntries, entry{e.Name(), m})
	}

	keepSet := make(map[string]bool) // meta filenames to keep

	// prefer entry with version_id; score: version_id > hash > nothing
	score := func(e entry) int {
		s := 0
		if e.mod.VersionID != "" {
			s += 2
		}
		if e.mod.Hash != "" {
			s += 1
		}
		return s
	}

	// deduplicate by hash first
	byHash := make(map[string][]entry)
	noHash := make([]entry, 0)
	for _, e := range readEntries {
		if e.mod.Hash != "" {
			byHash[e.mod.Hash] = append(byHash[e.mod.Hash], e)
		} else {
			noHash = append(noHash, e)
		}
	}
	for hash, group := range byHash {
		if len(group) == 1 {
			keepSet[group[0].name] = true
			continue
		}
		best := group[0]
		for _, g := range group[1:] {
			if score(g) > score(best) {
				best = g
			}
		}
		keepSet[best.name] = true
		for _, g := range group {
			if g.name != best.name {
				_ = os.Remove(filepath.Join(dir, g.name))
				log.Printf("[mods] removed duplicate meta %s (hash %s, kept %s)", g.name, hash, best.name)
			}
		}
	}

	// deduplicate no-hash entries by filename
	byFilename := make(map[string][]entry)
	for _, e := range noHash {
		if keepSet[e.name] {
			continue
		}
		byFilename[e.mod.Filename] = append(byFilename[e.mod.Filename], e)
	}
	for filename, group := range byFilename {
		if len(group) == 1 {
			keepSet[group[0].name] = true
			continue
		}
		best := group[0]
		for _, g := range group[1:] {
			if score(g) > score(best) {
				best = g
			}
		}
		keepSet[best.name] = true
		for _, g := range group {
			if g.name != best.name {
				_ = os.Remove(filepath.Join(dir, g.name))
				log.Printf("[mods] removed duplicate meta %s (file %s, kept %s)", g.name, filename, best.name)
			}
		}
	}
}

func ListMods(profileID string) ([]InstalledMod, error) {
	deduplicateMetas(profileID)
	go recoverOrphanedMods(profileID)
	dir := metaDir(profileID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []InstalledMod{}, nil
	}
	if err != nil {
		return nil, err
	}
	mods := make([]InstalledMod, 0)
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".meta") || strings.HasSuffix(e.Name(), ".modmeta")) {
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
