package minecraft

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"deckanator/internal/config"
	"deckanator/internal/download"
	"deckanator/internal/maven"
)

const (
	loaderFabric = "fabric"
	ruleAllow    = "allow"
)

// IsInstalled reports whether the given (loader, mcVersion[, fabricVersion])
// combination is already present on disk.
func IsInstalled(loader, mcVersion, fabricVersion string) bool {
	dir := config.GameDir()
	if _, err := os.Stat(filepath.Join(dir, "versions", mcVersion, mcVersion+".json")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "versions", mcVersion, mcVersion+".jar")); err != nil {
		return false
	}
	if loader == loaderFabric && fabricVersion != "" {
		id := fabricProfileID(mcVersion, fabricVersion)
		if _, err := os.Stat(filepath.Join(dir, "versions", id, id+".json")); err != nil {
			return false
		}
	}
	return true
}

// Install downloads Minecraft (plus, optionally, Fabric) for the given
// target. It ensures the required Java runtime is cached.
func Install(
	ctx context.Context,
	loader, mcVersion, fabricVersion, javaComponent string,
	ensureJava func(component string, progress ProgressFunc) (string, error),
	javaCached func(component string) string,
	progress ProgressFunc,
) error {
	dir := config.GameDir()

	progress("Fetching versions", 2, 100)
	manifest, err := fetchManifest()
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}

	var versionURL string
	for _, v := range manifest.Versions {
		if v.ID == mcVersion {
			versionURL = v.URL
			break
		}
	}
	if versionURL == "" {
		return fmt.Errorf("version %s not found", mcVersion)
	}

	progress("Fetching metadata", 5, 100)
	details, err := fetchVersionDetails(versionURL)
	if err != nil {
		return fmt.Errorf("version details: %w", err)
	}

	versionDir := filepath.Join(dir, "versions", mcVersion)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return err
	}
	if data, err := json.MarshalIndent(details, "", "  "); err == nil {
		if err := os.WriteFile(filepath.Join(versionDir, mcVersion+".json"), data, 0o644); err != nil {
			return err
		}
	}

	progress("Downloading client", 8, 100)
	clientPath := filepath.Join(versionDir, mcVersion+".jar")
	if err := download.File(details.Downloads["client"].URL, clientPath); err != nil {
		return fmt.Errorf("client jar: %w", err)
	}

	libs := FilterLibraries(details.Libraries)
	libDir := filepath.Join(dir, "libraries")
	if err := downloadLibraries(libs, libDir, 10, 30, progress); err != nil {
		return err
	}

	progress("Extracting natives", 32, 100)
	nativesDir := filepath.Join(versionDir, "natives")
	if err := os.MkdirAll(nativesDir, 0o755); err != nil {
		return err
	}
	if err := extractAllNatives(libs, libDir, nativesDir); err != nil {
		return fmt.Errorf("natives: %w", err)
	}

	progress("Fetching asset index", 35, 100)
	indexDir := filepath.Join(dir, "assets", "indexes")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return err
	}
	indexPath := filepath.Join(indexDir, details.AssetIndex.ID+".json")
	if err := download.File(details.AssetIndex.URL, indexPath); err != nil {
		return fmt.Errorf("asset index: %w", err)
	}

	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	var index AssetIndex
	if err := json.Unmarshal(indexData, &index); err != nil {
		return err
	}

	if err := downloadAssets(index, filepath.Join(dir, "assets", "objects"), 37, 90, progress); err != nil {
		return fmt.Errorf("assets: %w", err)
	}

	if loader == loaderFabric && fabricVersion != "" {
		progress("Installing Fabric", 90, 100)
		if err := installFabric(ctx, dir, mcVersion, fabricVersion, libDir, progress); err != nil {
			return fmt.Errorf("fabric: %w", err)
		}
	}

	component := javaComponent
	if details.JavaVersion != nil && details.JavaVersion.Component != "" {
		component = details.JavaVersion.Component
	}
	if component == "" {
		component = "java-runtime-gamma"
	}
	if javaCached(component) == "" {
		progress("Downloading Java", 93, 100)
		if _, err := ensureJava(component, func(stage string, cur, tot int) {
			progress(stage, 93+cur*6/100, 100)
		}); err != nil {
			return fmt.Errorf("java: %w", err)
		}
	}

	progress("Done", 100, 100)
	return nil
}

type libArtifact struct{ path, url string }

func collectArtifacts(libs []Library) []libArtifact {
	out := make([]libArtifact, 0, len(libs))
	for _, lib := range libs {
		switch {
		case lib.Downloads != nil && lib.Downloads.Artifact != nil:
			out = append(out, libArtifact{
				path: lib.Downloads.Artifact.Path,
				url:  lib.Downloads.Artifact.URL,
			})
		case lib.Name != "" && lib.URL != "":
			rel := maven.LocalPath(lib.Name)
			if rel != "" {
				out = append(out, libArtifact{
					path: rel,
					url:  maven.DownloadURL(lib.URL, lib.Name),
				})
			}
		}
	}
	return out
}

func downloadLibraries(libs []Library, libDir string, from, to int, progress ProgressFunc) error {
	artifacts := collectArtifacts(libs)
	total := len(artifacts)
	if total == 0 {
		return nil
	}

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var dlErr error
	done := 0

	for _, a := range artifacts {
		wg.Add(1)
		go func(a libArtifact) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := download.File(a.url, filepath.Join(libDir, a.path)); err != nil {
				mu.Lock()
				dlErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			done++
			progress("Downloading libraries", from+done*(to-from)/total, 100)
			mu.Unlock()
		}(a)
	}
	wg.Wait()
	return dlErr
}

func downloadAssets(index AssetIndex, objectsDir string, from, to int, progress ProgressFunc) error {
	hashes := make([]string, 0, len(index.Objects))
	for _, obj := range index.Objects {
		hashes = append(hashes, obj.Hash)
	}
	total := len(hashes)
	if total == 0 {
		return nil
	}

	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var dlErr error
	done := 0

	for _, hash := range hashes {
		wg.Add(1)
		go func(hash string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			sub := hash[:2]
			url := fmt.Sprintf("https://resources.download.minecraft.net/%s/%s", sub, hash)
			if err := download.File(url, filepath.Join(objectsDir, sub, hash)); err != nil {
				mu.Lock()
				dlErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			done++
			if done%200 == 0 || done == total {
				progress("Downloading assets", from+done*(to-from)/total, 100)
			}
			mu.Unlock()
		}(hash)
	}
	wg.Wait()
	return dlErr
}

func extractAllNatives(libs []Library, libDir, nativesDir string) error {
	key := "natives-" + config.OSName()
	for _, lib := range libs {
		if lib.Downloads == nil || lib.Downloads.Classifiers == nil {
			continue
		}
		native := lib.Downloads.Classifiers[key]
		if native == nil {
			continue
		}
		path := filepath.Join(libDir, native.Path)
		if err := download.File(native.URL, path); err != nil {
			return err
		}
		if err := download.ExtractNatives(path, nativesDir); err != nil {
			return err
		}
	}
	return nil
}

func installFabric(_ context.Context, dir, mcVersion, loaderVersion, libDir string, progress ProgressFunc) error {
	progress("Fetching Fabric profile", 93, 100)
	prof, err := fetchFabricProfile(mcVersion, loaderVersion)
	if err != nil {
		return err
	}

	id := fabricProfileID(mcVersion, loaderVersion)
	versionDir := filepath.Join(dir, "versions", id)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return err
	}
	if data, err := json.MarshalIndent(prof, "", "  "); err == nil {
		if err := os.WriteFile(filepath.Join(versionDir, id+".json"), data, 0o644); err != nil {
			return err
		}
	}

	progress("Downloading Fabric libraries", 95, 100)
	return downloadLibraries(prof.Libraries, libDir, 95, 99, progress)
}

// FilterLibraries returns the subset of libs whose rules allow them on
// the current OS.
func FilterLibraries(libs []Library) []Library {
	out := make([]Library, 0, len(libs))
	for _, lib := range libs {
		if shouldInclude(lib) {
			out = append(out, lib)
		}
	}
	return out
}

func shouldInclude(lib Library) bool {
	if len(lib.Rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range lib.Rules {
		switch {
		case rule.OS == nil:
			allowed = rule.Action == ruleAllow
		case rule.OS.Name == config.OSName():
			allowed = rule.Action == ruleAllow
		}
	}
	return allowed
}

func fabricProfileID(mcVersion, loaderVersion string) string {
	return fmt.Sprintf("fabric-loader-%s-%s", loaderVersion, mcVersion)
}
