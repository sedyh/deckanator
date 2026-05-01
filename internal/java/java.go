// Package java locates a working Java executable: cached Mojang runtime,
// any cached Mojang runtime, system Java, or a fresh Mojang download.
package java

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"deckanator/internal/config"
	"deckanator/internal/download"
	"deckanator/internal/request"
)

// ProgressFunc is how long-running operations report progress.
type ProgressFunc func(stage string, current, total int)

const allRuntimesURL = "https://piston-meta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

type allRuntimes map[string]map[string][]runtimeEntry

type runtimeEntry struct {
	Manifest struct {
		URL string `json:"url"`
	} `json:"manifest"`
}

type runtimeManifest struct {
	Files map[string]runtimeFile `json:"files"`
}

type runtimeFile struct {
	Type       string `json:"type"`
	Executable bool   `json:"executable"`
	Downloads  *struct {
		Raw struct {
			URL string `json:"url"`
		} `json:"raw"`
	} `json:"downloads,omitempty"`
	Target string `json:"target,omitempty"`
}

// Ensure returns a path to a working java executable. It tries the
// cached Mojang runtime for component first, then any other cached
// Mojang runtime, then system Java, and finally downloads the Mojang
// runtime for component.
func Ensure(component string, progress ProgressFunc) (string, error) {
	if component == "" {
		component = "java-runtime-gamma"
	}
	if j := Cached(component); j != "" && works(j) {
		return j, nil
	}
	if j := anyMojang(); j != "" {
		return j, nil
	}
	if j, ok := findSystem(); ok {
		return j, nil
	}
	return downloadMojang(component, progress)
}

// Cached returns the path to the cached Mojang java binary for
// component, or "" if no such runtime is on disk.
func Cached(component string) string {
	dir := filepath.Join(config.GameDir(), "runtime", component, config.MojangOSKey())
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return findBinary(dir)
}

func anyMojang() string {
	entries, err := os.ReadDir(filepath.Join(config.GameDir(), "runtime"))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		j := findBinary(filepath.Join(config.GameDir(), "runtime", e.Name()))
		if j != "" && works(j) {
			return j
		}
	}
	return ""
}

func findSystem() (string, bool) {
	if home := os.Getenv("JAVA_HOME"); home != "" {
		j := filepath.Join(home, "bin", "java")
		if works(j) {
			return j, true
		}
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("/usr/libexec/java_home").Output(); err == nil {
			j := filepath.Join(strings.TrimSpace(string(out)), "bin", "java")
			if works(j) {
				return j, true
			}
		}
	}
	for _, c := range platformPaths() {
		if works(c) {
			return c, true
		}
	}
	if path, err := exec.LookPath("java"); err == nil && works(path) {
		return path, true
	}
	return "", false
}

func works(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	out, err := exec.Command(path, "-version").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "version")
}

func findBinary(dir string) string {
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.exe"
	}
	var found string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if found != "" || info == nil || info.IsDir() {
			return nil
		}
		if info.Name() == name && filepath.Base(filepath.Dir(path)) == "bin" {
			found = path
		}
		return nil
	})
	return found
}

func downloadMojang(component string, progress ProgressFunc) (string, error) {
	progress("Fetching Java runtime list", 1, 100)
	var all allRuntimes
	if err := request.JSON(allRuntimesURL, &all); err != nil {
		return "", fmt.Errorf("java runtime list: %w", err)
	}

	osKey := config.MojangOSKey()
	entries := all[osKey][component]
	if len(entries) == 0 {
		return "", fmt.Errorf("java runtime %q not found for %s", component, osKey)
	}

	progress("Fetching Java manifest", 3, 100)
	var m runtimeManifest
	if err := request.JSON(entries[0].Manifest.URL, &m); err != nil {
		return "", fmt.Errorf("java manifest: %w", err)
	}

	destDir := filepath.Join(config.GameDir(), "runtime", component, osKey)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}

	if err := downloadRuntimeFiles(destDir, m.Files, progress); err != nil {
		return "", err
	}
	if err := createRuntimeSymlinks(destDir, m.Files); err != nil {
		return "", err
	}

	progress("Java ready", 100, 100)
	exe := findBinary(destDir)
	if exe == "" {
		return "", fmt.Errorf("java binary not found after download in %s", destDir)
	}
	return exe, nil
}

func downloadRuntimeFiles(destDir string, all map[string]runtimeFile, progress ProgressFunc) error {
	type item struct {
		path  string
		entry runtimeFile
	}
	files := make([]item, 0, len(all))
	for p, f := range all {
		if f.Type == "file" && f.Downloads != nil {
			files = append(files, item{p, f})
		}
	}
	total := len(files)

	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var dlErr error
	done := 0

	for _, it := range files {
		wg.Add(1)
		go func(path string, f runtimeFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := downloadOneRuntimeFile(filepath.Join(destDir, path), f); err != nil {
				mu.Lock()
				dlErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			done++
			if done%100 == 0 || done == total {
				progress("Downloading Java", 5+done*94/total, 100)
			}
			mu.Unlock()
		}(it.path, it.entry)
	}
	wg.Wait()
	return dlErr
}

func downloadOneRuntimeFile(dest string, f runtimeFile) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := download.File(f.Downloads.Raw.URL, dest); err != nil {
		return err
	}
	if f.Executable {
		return os.Chmod(dest, 0o755)
	}
	return nil
}

func createRuntimeSymlinks(destDir string, files map[string]runtimeFile) error {
	for path, f := range files {
		if f.Type != "link" || f.Target == "" {
			continue
		}
		link := filepath.Join(destDir, path)
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return err
		}
		_ = os.Remove(link) // ignore: link may not exist yet
		if err := os.Symlink(f.Target, link); err != nil {
			return err
		}
	}
	return nil
}

func platformPaths() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "linux":
		dirs := []string{"/usr/lib/jvm", "/usr/lib64/jvm", "/opt/jdk", "/opt/jdks"}
		paths := make([]string, 0, len(dirs))
		for _, dir := range dirs {
			paths = append(paths, scanDir(dir)...)
		}
		paths = append(paths, "/usr/bin/java")
		return paths
	case "darwin":
		paths := []string{
			"/opt/homebrew/opt/openjdk@21/bin/java",
			"/opt/homebrew/opt/openjdk/bin/java",
		}
		for _, dir := range []string{
			"/Library/Java/JavaVirtualMachines",
			"/System/Library/Java/JavaVirtualMachines",
			filepath.Join(home, "Library/Java/JavaVirtualMachines"),
		} {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				paths = append(paths, filepath.Join(dir, e.Name(), "Contents", "Home", "bin", "java"))
			}
		}
		sdkmanDir := os.Getenv("SDKMAN_DIR")
		if sdkmanDir == "" {
			sdkmanDir = filepath.Join(home, ".sdkman")
		}
		paths = append(paths, scanDir(filepath.Join(sdkmanDir, "candidates", "java"))...)
		asdfDir := os.Getenv("ASDF_DATA_DIR")
		if asdfDir == "" {
			asdfDir = filepath.Join(home, ".asdf")
		}
		paths = append(paths, scanDir(filepath.Join(asdfDir, "installs", "java"))...)
		return paths
	}
	return nil
}

func scanDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries)*2)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		base := filepath.Join(dir, e.Name())
		paths = append(paths,
			filepath.Join(base, "bin", "java"),
			filepath.Join(base, "jre", "bin", "java"),
		)
	}
	return paths
}
