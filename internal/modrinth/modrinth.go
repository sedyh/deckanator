// Package modrinth wraps the Modrinth v2 REST API and owns the
// per-profile installed-mod metadata layer.
package modrinth

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

	"deckanator/internal/config"
	"deckanator/internal/errs"
)

const (
	apiBase      = "https://api.modrinth.com/v2"
	typeDatapack = "datapack"
)

// Result is a search hit from /search.
type Result struct {
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	IconURL     string   `json:"icon_url"`
	Downloads   int      `json:"downloads"`
	ProjectType string   `json:"project_type"`
	Categories  []string `json:"categories"`
}

// SearchResponse is the decoded body of a /search request.
type SearchResponse struct {
	Hits      []Result `json:"hits"`
	TotalHits int      `json:"total_hits"`
}

// Dependency describes a relation between Modrinth versions.
type Dependency struct {
	ProjectID      string `json:"project_id"`
	VersionID      string `json:"version_id"`
	DependencyType string `json:"dependency_type"`
}

// File is one downloadable file attached to a Version.
type File struct {
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Hashes   map[string]string `json:"hashes"`
}

// Version is a single Modrinth project version.
type Version struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	VersionNumber string       `json:"version_number"`
	GameVersions  []string     `json:"game_versions"`
	Loaders       []string     `json:"loaders"`
	ProjectID     string       `json:"project_id"`
	Dependencies  []Dependency `json:"dependencies"`
	Files         []File       `json:"files"`
}

// Installed is the per-profile bookkeeping record written to .meta/.
type Installed struct {
	ProjectID   string `json:"project_id"`
	Filename    string `json:"filename"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	VersionID   string `json:"version_id"`
	ProjectType string `json:"project_type,omitempty"`
	IconURL     string `json:"icon_url,omitempty"`
	Hash        string `json:"hash,omitempty"`
}

// Info is the subset of a Modrinth project returned to the UI.
type Info struct {
	IconURL     string `json:"icon_url"`
	Description string `json:"description"`
}

func modsDir(profileID string) string {
	return filepath.Join(config.GameDir(), "profiles", profileID, "mods")
}

func metaDir(profileID string) string {
	return filepath.Join(config.GameDir(), "profiles", profileID, ".meta")
}

func metaPath(profileID, projectID string) string {
	return filepath.Join(metaDir(profileID), projectID+".meta")
}

// Search queries /search and returns hits plus a total count.
func Search(query, mcVersion, loader, sortBy string, offset int, showMods, showDatapacks bool) (_ SearchResponse, e error) {
	var facetGroups []string
	if showMods || showDatapacks {
		var items []string
		if showMods {
			items = append(items, `"project_type:mod"`)
		}
		if showDatapacks {
			items = append(items, `"project_type:datapack"`)
		}
		facetGroups = append(facetGroups, "["+strings.Join(items, ",")+"]")
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

	reqURL := apiBase + "/search?" + params.Encode()
	resp, err := http.Get(reqURL)
	if err != nil {
		return SearchResponse{}, err
	}
	defer errs.Close(&e, resp.Body)

	var raw SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return SearchResponse{}, err
	}
	return raw, nil
}

// Versions returns project versions matching the (mcVersion, loader)
// filter, falling back to loader-only if nothing matches.
func Versions(projectID, mcVersion, projectType, loader string) ([]Version, error) {
	actualLoader := loader
	if projectType == typeDatapack {
		actualLoader = typeDatapack
	}

	params := url.Values{}
	if mcVersion != "" {
		params.Set("game_versions", fmt.Sprintf(`["%s"]`, mcVersion))
	}
	if actualLoader != "" {
		params.Set("loaders", fmt.Sprintf(`["%s"]`, actualLoader))
	}

	vers, err := fetchVersions(projectID, params)
	if err != nil {
		return nil, err
	}
	if len(vers) == 0 && mcVersion != "" {
		fallback := url.Values{}
		if actualLoader != "" {
			fallback.Set("loaders", fmt.Sprintf(`["%s"]`, actualLoader))
		}
		return fetchVersions(projectID, fallback)
	}
	return vers, nil
}

func fetchVersions(projectID string, params url.Values) (_ []Version, e error) {
	resp, err := http.Get(fmt.Sprintf("%s/project/%s/version?%s", apiBase, projectID, params.Encode()))
	if err != nil {
		return nil, err
	}
	defer errs.Close(&e, resp.Body)
	var vers []Version
	if err := json.NewDecoder(resp.Body).Decode(&vers); err != nil {
		return nil, err
	}
	return vers, nil
}

// Install downloads the given file into the profile's mods/datapacks
// directory and records a .meta entry. Required dependencies are
// resolved recursively.
func Install(profileID, projectID, title, description, projectType, iconURL, versionID, downloadURL, filename string) error {
	dir := installDir(profileID, projectType, filename)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(metaDir(profileID), 0o755); err != nil {
		return err
	}
	if err := downloadToFile(dir, filename, downloadURL); err != nil {
		return err
	}
	meta := Installed{
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
	if err := os.WriteFile(metaPath(profileID, projectID), data, 0o644); err != nil {
		return err
	}
	return installDeps(profileID, versionID)
}

func installDeps(profileID, versionID string) (e error) {
	resp, err := http.Get(apiBase + "/version/" + versionID)
	if err != nil {
		return err
	}
	defer errs.Close(&e, resp.Body)
	var ver Version
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return err
	}

	installed, _ := List(profileID)
	known := make(map[string]bool, len(installed))
	for _, m := range installed {
		known[m.ProjectID] = true
	}

	for _, dep := range ver.Dependencies {
		if dep.DependencyType != "required" || dep.ProjectID == "" || known[dep.ProjectID] {
			continue
		}
		if err := installOneDep(profileID, dep, ver.Loaders); err != nil {
			log.Printf("[mods] dep %s: %v", dep.ProjectID, err)
		}
	}
	return nil
}

func installOneDep(profileID string, dep Dependency, parentLoaders []string) (e error) {
	var ver Version
	switch {
	case dep.VersionID != "":
		r, err := http.Get(apiBase + "/version/" + dep.VersionID)
		if err != nil {
			return err
		}
		defer errs.Close(&e, r.Body)
		if err := json.NewDecoder(r.Body).Decode(&ver); err != nil {
			return err
		}
	default:
		params := url.Values{}
		for _, ldr := range parentLoaders {
			params.Set("loaders", fmt.Sprintf(`["%s"]`, ldr))
			break
		}
		r, err := http.Get(apiBase + "/project/" + dep.ProjectID + "/version?" + params.Encode())
		if err != nil {
			return err
		}
		defer errs.Close(&e, r.Body)
		var vers []Version
		if err := json.NewDecoder(r.Body).Decode(&vers); err != nil {
			return err
		}
		if len(vers) == 0 {
			return fmt.Errorf("no versions")
		}
		ver = vers[0]
	}

	file := pickPrimary(ver.Files)
	if file == nil {
		return fmt.Errorf("no files")
	}

	// Best-effort: enrich with project metadata; ignore failures.
	var proj struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		IconURL     string `json:"icon_url"`
	}
	if r, err := http.Get(apiBase + "/project/" + dep.ProjectID); err == nil {
		_ = json.NewDecoder(r.Body).Decode(&proj)
		_ = r.Body.Close()
	}

	dir := installDir(profileID, "mod", file.Filename)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(metaDir(profileID), 0o755); err != nil {
		return err
	}
	if err := downloadToFile(dir, file.Filename, file.URL); err != nil {
		return err
	}
	m := Installed{
		ProjectID:   dep.ProjectID,
		Filename:    file.Filename,
		Title:       proj.Title,
		Description: proj.Description,
		VersionID:   ver.ID,
		ProjectType: "mod",
		IconURL:     proj.IconURL,
		Hash:        fileHash(filepath.Join(dir, file.Filename)),
	}
	data, _ := json.Marshal(m)
	return os.WriteFile(metaPath(profileID, dep.ProjectID), data, 0o644)
}

func pickPrimary(files []File) *File {
	for i := range files {
		if files[i].Primary {
			return &files[i]
		}
	}
	if len(files) > 0 {
		return &files[0]
	}
	return nil
}

// FetchInfo returns fresh icon/description for a project and refreshes
// the profile's cached .meta entry if the data changed.
func FetchInfo(profileID, projectID string) (_ Info, e error) {
	resp, err := http.Get(apiBase + "/project/" + projectID)
	if err != nil {
		return Info{}, err
	}
	defer errs.Close(&e, resp.Body)
	var proj Info
	if err := json.NewDecoder(resp.Body).Decode(&proj); err != nil {
		return Info{}, err
	}

	metaFile := metaPath(profileID, projectID)
	data, err := os.ReadFile(metaFile)
	if err != nil {
		return proj, nil
	}
	var m Installed
	if json.Unmarshal(data, &m) != nil {
		return proj, nil
	}
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
		_ = os.WriteFile(metaFile, updated, 0o644)
	}
	return proj, nil
}

// Delete removes a mod's file and its .meta entry.
func Delete(profileID, projectID string) error {
	mods, err := List(profileID)
	if err != nil {
		return err
	}
	for _, m := range mods {
		if m.ProjectID == projectID {
			dir := installDir(profileID, m.ProjectType, m.Filename)
			_ = os.Remove(filepath.Join(dir, m.Filename))
			break
		}
	}
	return os.Remove(metaPath(profileID, projectID))
}

// List returns all installed mods for profileID, reconciling .meta files
// with what is actually on disk along the way.
func List(profileID string) ([]Installed, error) {
	deduplicateMetas(profileID)
	go recoverOrphans(profileID)

	dir := metaDir(profileID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Installed{}, nil
	}
	if err != nil {
		return nil, err
	}
	mods := make([]Installed, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".meta") && !strings.HasSuffix(e.Name(), ".modmeta") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m Installed
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		mods = append(mods, m)
	}
	return mods, nil
}

func installDir(profileID, projectType, filename string) string {
	if projectType == typeDatapack && strings.HasSuffix(strings.ToLower(filename), ".zip") {
		return filepath.Join(config.GameDir(), "profiles", profileID, "datapacks")
	}
	return modsDir(profileID)
}

func downloadToFile(dir, filename, rawURL string) (e error) {
	resp, err := http.Get(rawURL)
	if err != nil {
		return err
	}
	defer errs.Close(&e, resp.Body)
	dest := filepath.Join(dir, filename)
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func fileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		_ = f.Close()
		return ""
	}
	_ = f.Close()
	return hex.EncodeToString(h.Sum(nil))
}

func recoverOrphans(profileID string) {
	dirPath := metaDir(profileID)
	trackedHashes, trackedFiles, known := buildTrackingMaps(dirPath)

	dirs := []struct {
		dir  string
		exts []string
	}{
		{modsDir(profileID), []string{".jar"}},
		{filepath.Join(config.GameDir(), "profiles", profileID, "datapacks"), []string{".zip"}},
	}
	for _, d := range dirs {
		recoverOrphansFromDir(d.dir, d.exts, dirPath, trackedHashes, trackedFiles, known)
	}
}

func buildTrackingMaps(dirPath string) (hashes, files, known map[string]bool) {
	hashes = make(map[string]bool)
	files = make(map[string]bool)
	known = make(map[string]bool)

	entries, _ := os.ReadDir(dirPath)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".meta") && !strings.HasSuffix(e.Name(), ".modmeta") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dirPath, e.Name()))
		if err != nil {
			continue
		}
		var m Installed
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.Hash != "" {
			hashes[m.Hash] = true
		}
		if m.Filename != "" {
			files[m.Filename] = true
		}
		if m.ProjectID != "" {
			known[m.ProjectID] = true
		}
	}
	return hashes, files, known
}

func recoverOrphansFromDir(dir string, exts []string, dirPath string, trackedHashes, trackedFiles, known map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !hasExt(ext, exts) || trackedFiles[e.Name()] {
			continue
		}

		path := filepath.Join(dir, e.Name())
		hash := fileHash(path)
		if hash != "" && trackedHashes[hash] {
			continue
		}

		projectType := "mod"
		if ext == ".zip" {
			projectType = typeDatapack
		}

		meta := recoverByHash(hash, e.Name(), projectType, known)
		if meta == nil {
			continue
		}
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			log.Printf("[mods] mkdir %s: %v", dirPath, err)
			continue
		}
		data, _ := json.Marshal(meta)
		_ = os.WriteFile(filepath.Join(dirPath, meta.ProjectID+".meta"), data, 0o644)
		if hash != "" {
			trackedHashes[hash] = true
		}
		trackedFiles[e.Name()] = true
		known[meta.ProjectID] = true
		log.Printf("[mods] recovered orphan: %s -> %s (%s)", e.Name(), meta.Title, meta.ProjectID)
	}
}

func hasExt(ext string, exts []string) bool {
	for _, x := range exts {
		if ext == x {
			return true
		}
	}
	return false
}

func recoverByHash(hash, filename, projectType string, known map[string]bool) *Installed {
	if hash == "" {
		return nil
	}
	resp, err := http.Get(apiBase + "/version_file/" + hash + "?algorithm=sha1")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	var ver Version
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil || ver.ProjectID == "" || known[ver.ProjectID] {
		return nil
	}
	var proj struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		IconURL     string `json:"icon_url"`
		ProjectType string `json:"project_type"`
	}
	// Best-effort: enrich with project metadata; ignore failures.
	if r, err := http.Get(apiBase + "/project/" + ver.ProjectID); err == nil {
		_ = json.NewDecoder(r.Body).Decode(&proj)
		_ = r.Body.Close()
	}
	if proj.ProjectType != "" {
		projectType = proj.ProjectType
	}
	return &Installed{
		ProjectID:   ver.ProjectID,
		Filename:    filename,
		Title:       proj.Title,
		Description: proj.Description,
		VersionID:   ver.ID,
		ProjectType: projectType,
		IconURL:     proj.IconURL,
		Hash:        hash,
	}
}

type metaEntry struct {
	name string
	mod  Installed
}

func deduplicateMetas(profileID string) {
	dir := metaDir(profileID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	all := make([]metaEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".meta") && !strings.HasSuffix(e.Name(), ".modmeta")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m Installed
		if json.Unmarshal(data, &m) != nil || m.Filename == "" {
			continue
		}
		all = append(all, metaEntry{e.Name(), m})
	}

	keep := make(map[string]bool)

	byHash := make(map[string][]metaEntry)
	noHash := make([]metaEntry, 0)
	for _, e := range all {
		if e.mod.Hash != "" {
			byHash[e.mod.Hash] = append(byHash[e.mod.Hash], e)
			continue
		}
		noHash = append(noHash, e)
	}
	pickBest(byHash, keep, dir, "hash")

	byFilename := make(map[string][]metaEntry)
	for _, e := range noHash {
		if keep[e.name] {
			continue
		}
		byFilename[e.mod.Filename] = append(byFilename[e.mod.Filename], e)
	}
	pickBest(byFilename, keep, dir, "file")
}

func metaScore(e metaEntry) int {
	s := 0
	if e.mod.VersionID != "" {
		s += 2
	}
	if e.mod.Hash != "" {
		s++
	}
	return s
}

func pickBest(groups map[string][]metaEntry, keep map[string]bool, dir, label string) {
	for key, group := range groups {
		if len(group) == 1 {
			keep[group[0].name] = true
			continue
		}
		best := group[0]
		for _, g := range group[1:] {
			if metaScore(g) > metaScore(best) {
				best = g
			}
		}
		keep[best.name] = true
		for _, g := range group {
			if g.name != best.name {
				_ = os.Remove(filepath.Join(dir, g.name))
				log.Printf("[mods] removed duplicate meta %s (%s %s, kept %s)", g.name, label, key, best.name)
			}
		}
	}
}
