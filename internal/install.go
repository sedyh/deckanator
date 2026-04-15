package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

func IsInstalled(loader, mcVersion, fabricVersion string) bool {
	dir := GameDir()
	vanillaJSON := filepath.Join(dir, "versions", mcVersion, mcVersion+".json")
	vanillaJAR := filepath.Join(dir, "versions", mcVersion, mcVersion+".jar")
	if _, err := os.Stat(vanillaJSON); err != nil {
		return false
	}
	if _, err := os.Stat(vanillaJAR); err != nil {
		return false
	}
	if loader == "fabric" && fabricVersion != "" {
		id := fabricProfileID(mcVersion, fabricVersion)
		fabricJSON := filepath.Join(dir, "versions", id, id+".json")
		if _, err := os.Stat(fabricJSON); err != nil {
			return false
		}
	}
	return true
}

func Install(ctx context.Context, loader, mcVersion, fabricVersion, javaComponent string, progress ProgressFunc) error {
	dir := GameDir()

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
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return err
	}
	jsonData, _ := json.MarshalIndent(details, "", "  ")
	if err := os.WriteFile(filepath.Join(versionDir, mcVersion+".json"), jsonData, 0644); err != nil {
		return err
	}

	progress("Downloading client", 8, 100)
	clientURL := details.Downloads["client"].URL
	clientPath := filepath.Join(versionDir, mcVersion+".jar")
	if err := downloadFile(clientURL, clientPath); err != nil {
		return fmt.Errorf("client jar: %w", err)
	}

	libs := filterLibraries(details.Libraries)
	libDir := filepath.Join(dir, "libraries")
	if err := downloadLibraries(libs, libDir, 10, 30, progress); err != nil {
		return err
	}

	progress("Extracting natives", 32, 100)
	nativesDir := filepath.Join(versionDir, "natives")
	if err := os.MkdirAll(nativesDir, 0755); err != nil {
		return err
	}
	if err := extractAllNatives(libs, libDir, nativesDir); err != nil {
		return fmt.Errorf("natives: %w", err)
	}

	progress("Fetching asset index", 35, 100)
	indexDir := filepath.Join(dir, "assets", "indexes")
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		return err
	}
	indexPath := filepath.Join(indexDir, details.AssetIndex.ID+".json")
	if err := downloadFile(details.AssetIndex.URL, indexPath); err != nil {
		return fmt.Errorf("asset index: %w", err)
	}

	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	var assetIndex AssetIndex
	if err := json.Unmarshal(indexData, &assetIndex); err != nil {
		return err
	}

	if err := downloadAssets(assetIndex, filepath.Join(dir, "assets", "objects"), 37, 90, progress); err != nil {
		return fmt.Errorf("assets: %w", err)
	}

	if loader == "fabric" && fabricVersion != "" {
		progress("Installing Fabric", 90, 100)
		if err := installFabric(ctx, dir, mcVersion, fabricVersion, libDir, progress); err != nil {
			return fmt.Errorf("fabric: %w", err)
		}
	}

	component := ""
	if details.JavaVersion != nil && details.JavaVersion.Component != "" {
		component = details.JavaVersion.Component
	} else if javaComponent != "" {
		component = javaComponent
	} else {
		component = "java-runtime-gamma"
	}
	if cachedJavaPath(component) == "" {
		progress("Downloading Java", 93, 100)
		if _, err := downloadMojangJava(component, func(stage string, cur, tot int) {
			progress(stage, 93+cur*6/100, 100)
		}); err != nil {
			return fmt.Errorf("java: %w", err)
		}
	}

	progress("Done", 100, 100)
	return nil
}

type libArtifact struct {
	path string
	url  string
}

func collectArtifacts(libs []Library) []libArtifact {
	var out []libArtifact
	for _, lib := range libs {
		if lib.Downloads != nil && lib.Downloads.Artifact != nil {
			out = append(out, libArtifact{
				path: lib.Downloads.Artifact.Path,
				url:  lib.Downloads.Artifact.URL,
			})
		} else if lib.Name != "" && lib.URL != "" {
			rel := mavenLocalPath(lib.Name)
			if rel != "" {
				out = append(out, libArtifact{
					path: rel,
					url:  mavenDownloadURL(lib.URL, lib.Name),
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

	for _, art := range artifacts {
		wg.Add(1)
		go func(a libArtifact) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			path := filepath.Join(libDir, a.path)
			if err := downloadFile(a.url, path); err != nil {
				mu.Lock()
				dlErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			done++
			pct := from + done*(to-from)/total
			progress("Downloading libraries", pct, 100)
			mu.Unlock()
		}(art)
	}
	wg.Wait()
	return dlErr
}

func downloadAssets(index AssetIndex, objectsDir string, from, to int, progress ProgressFunc) error {
	type asset struct{ hash string }
	var assets []asset
	for _, obj := range index.Objects {
		assets = append(assets, asset{obj.Hash})
	}
	total := len(assets)
	if total == 0 {
		return nil
	}

	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var dlErr error
	done := 0

	for _, a := range assets {
		wg.Add(1)
		go func(hash string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			sub := hash[:2]
			path := filepath.Join(objectsDir, sub, hash)
			url := fmt.Sprintf("https://resources.download.minecraft.net/%s/%s", sub, hash)
			if err := downloadFile(url, path); err != nil {
				mu.Lock()
				dlErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			done++
			if done%200 == 0 || done == total {
				pct := from + done*(to-from)/total
				progress("Downloading assets", pct, 100)
			}
			mu.Unlock()
		}(a.hash)
	}
	wg.Wait()
	return dlErr
}

func extractAllNatives(libs []Library, libDir, nativesDir string) error {
	key := "natives-" + osName()
	for _, lib := range libs {
		if lib.Downloads == nil || lib.Downloads.Classifiers == nil {
			continue
		}
		native := lib.Downloads.Classifiers[key]
		if native == nil {
			continue
		}
		path := filepath.Join(libDir, native.Path)
		if err := downloadFile(native.URL, path); err != nil {
			return err
		}
		if err := extractNatives(path, nativesDir); err != nil {
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
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return err
	}
	jsonData, _ := json.MarshalIndent(prof, "", "  ")
	if err := os.WriteFile(filepath.Join(versionDir, id+".json"), jsonData, 0644); err != nil {
		return err
	}

	progress("Downloading Fabric libraries", 95, 100)
	return downloadLibraries(prof.Libraries, libDir, 95, 99, progress)
}

func filterLibraries(libs []Library) []Library {
	var result []Library
	for _, lib := range libs {
		if shouldIncludeLibrary(lib) {
			result = append(result, lib)
		}
	}
	return result
}

func shouldIncludeLibrary(lib Library) bool {
	if len(lib.Rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range lib.Rules {
		if rule.OS == nil {
			if rule.Action == "allow" {
				allowed = true
			} else {
				allowed = false
			}
		} else if rule.OS.Name == osName() {
			if rule.Action == "allow" {
				allowed = true
			} else {
				allowed = false
			}
		}
	}
	return allowed
}

func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "osx"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

func fabricProfileID(mcVersion, loaderVersion string) string {
	return fmt.Sprintf("fabric-loader-%s-%s", loaderVersion, mcVersion)
}
