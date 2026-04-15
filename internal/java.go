package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
)

const javaAllRuntimesURL = "https://piston-meta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

type javaAllRuntimes map[string]map[string][]javaRuntimeEntry

type javaRuntimeEntry struct {
	Manifest struct {
		URL string `json:"url"`
	} `json:"manifest"`
}

type javaRuntimeManifest struct {
	Files map[string]javaRuntimeFileEntry `json:"files"`
}

type javaRuntimeFileEntry struct {
	Type       string `json:"type"`
	Executable bool   `json:"executable"`
	Downloads  *struct {
		Raw struct {
			URL string `json:"url"`
		} `json:"raw"`
	} `json:"downloads,omitempty"`
	Target string `json:"target,omitempty"`
}

// EnsureJava returns a path to a working java executable.
// Prefers the Mojang-cached runtime for the required component,
// then any other cached Mojang runtime, then system Java, then downloads.
func EnsureJava(component string, progress ProgressFunc) (string, error) {
	if component == "" {
		component = "java-runtime-gamma"
	}

	if j := cachedJavaPath(component); j != "" && javaWorks(j) {
		return j, nil
	}

	if j := anyMojangJavaPath(); j != "" {
		return j, nil
	}

	if j, ok := findSystemJava(); ok {
		return j, nil
	}

	return downloadMojangJava(component, progress)
}

// anyMojangJavaPath returns a working java from any already-downloaded Mojang runtime.
func anyMojangJavaPath() string {
	runtimeDir := filepath.Join(GameDir(), "runtime")
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		j := findJavaBinaryInDir(filepath.Join(runtimeDir, e.Name()))
		if j != "" && javaWorks(j) {
			return j
		}
	}
	return ""
}

func findSystemJava() (string, bool) {
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		j := filepath.Join(jh, "bin", "java")
		if javaWorks(j) {
			return j, true
		}
	}
	if goruntime.GOOS == "darwin" {
		if out, err := exec.Command("/usr/libexec/java_home").Output(); err == nil {
			j := filepath.Join(strings.TrimSpace(string(out)), "bin", "java")
			if javaWorks(j) {
				return j, true
			}
		}
	}
	for _, c := range platformJavaPaths() {
		if javaWorks(c) {
			return c, true
		}
	}
	if path, err := exec.LookPath("java"); err == nil && javaWorks(path) {
		return path, true
	}
	return "", false
}

func javaWorks(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	// java -version writes to stderr, CombinedOutput captures both
	out, err := exec.Command(path, "-version").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "version")
}

func cachedJavaPath(component string) string {
	destDir := filepath.Join(GameDir(), "runtime", component, mojangOSKey())
	if _, err := os.Stat(destDir); err != nil {
		return ""
	}
	return findJavaBinaryInDir(destDir)
}

func findJavaBinaryInDir(dir string) string {
	name := "java"
	if goruntime.GOOS == "windows" {
		name = "java.exe"
	}
	var found string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if found != "" || err != nil || info == nil || info.IsDir() {
			return nil
		}
		if info.Name() == name && filepath.Base(filepath.Dir(path)) == "bin" {
			found = path
		}
		return nil
	})
	return found
}

func downloadMojangJava(component string, progress ProgressFunc) (string, error) {
	progress("Fetching Java runtime list", 1, 100)
	data, err := httpGet(javaAllRuntimesURL)
	if err != nil {
		return "", fmt.Errorf("Java runtime list: %w", err)
	}

	var all javaAllRuntimes
	if err := json.Unmarshal(data, &all); err != nil {
		return "", err
	}

	osKey := mojangOSKey()
	entries := all[osKey][component]
	if len(entries) == 0 {
		return "", fmt.Errorf("Java runtime %q not found for %s", component, osKey)
	}

	progress("Fetching Java manifest", 3, 100)
	manifestData, err := httpGet(entries[0].Manifest.URL)
	if err != nil {
		return "", fmt.Errorf("Java manifest: %w", err)
	}

	var manifest javaRuntimeManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return "", err
	}

	destDir := filepath.Join(GameDir(), "runtime", component, osKey)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}

	type item struct {
		path  string
		entry javaRuntimeFileEntry
	}
	var files []item
	for p, f := range manifest.Files {
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
		go func(path string, f javaRuntimeFileEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			dest := filepath.Join(destDir, path)
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				mu.Lock()
				dlErr = err
				mu.Unlock()
				return
			}
			if err := downloadFile(f.Downloads.Raw.URL, dest); err != nil {
				mu.Lock()
				dlErr = err
				mu.Unlock()
				return
			}
			if f.Executable {
				os.Chmod(dest, 0755)
			}
			mu.Lock()
			done++
			if done%100 == 0 || done == total {
				pct := 5 + done*94/total
				progress("Downloading Java", pct, 100)
			}
			mu.Unlock()
		}(it.path, it.entry)
	}
	wg.Wait()
	if dlErr != nil {
		return "", dlErr
	}

	for path, f := range manifest.Files {
		if f.Type == "link" && f.Target != "" {
			link := filepath.Join(destDir, path)
			os.MkdirAll(filepath.Dir(link), 0755)
			os.Remove(link)
			os.Symlink(f.Target, link)
		}
	}

	progress("Java ready", 100, 100)
	exe := findJavaBinaryInDir(destDir)
	if exe == "" {
		return "", fmt.Errorf("java binary not found after download in %s", destDir)
	}
	return exe, nil
}

func mojangOSKey() string {
	switch goruntime.GOOS {
	case "darwin":
		if goruntime.GOARCH == "arm64" {
			return "mac-os-arm64"
		}
		return "mac-os"
	case "windows":
		switch goruntime.GOARCH {
		case "arm64":
			return "windows-arm64"
		case "386":
			return "windows-x86"
		}
		return "windows-x64"
	default:
		return "linux"
	}
}

func platformJavaPaths() []string {
	home, _ := os.UserHomeDir()
	switch goruntime.GOOS {
	case "linux":
		var paths []string
		for _, dir := range []string{"/usr/lib/jvm", "/usr/lib64/jvm", "/opt/jdk", "/opt/jdks"} {
			paths = append(paths, scanJVMDir(dir)...)
		}
		paths = append(paths, "/usr/bin/java")
		return paths
	case "darwin":
		var paths []string
		// homebrew
		paths = append(paths,
			"/opt/homebrew/opt/openjdk@21/bin/java",
			"/opt/homebrew/opt/openjdk/bin/java",
		)
		// system-wide JVMs (like PrismLauncher does)
		for _, dir := range []string{
			"/Library/Java/JavaVirtualMachines",
			"/System/Library/Java/JavaVirtualMachines",
			filepath.Join(home, "Library/Java/JavaVirtualMachines"),
		} {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				base := filepath.Join(dir, e.Name(), "Contents", "Home", "bin", "java")
				paths = append(paths, base)
			}
		}
		// sdkman / asdf
		sdkmanDir := os.Getenv("SDKMAN_DIR")
		if sdkmanDir == "" {
			sdkmanDir = filepath.Join(home, ".sdkman")
		}
		paths = append(paths, scanJVMDir(filepath.Join(sdkmanDir, "candidates", "java"))...)
		asdfDir := os.Getenv("ASDF_DATA_DIR")
		if asdfDir == "" {
			asdfDir = filepath.Join(home, ".asdf")
		}
		paths = append(paths, scanJVMDir(filepath.Join(asdfDir, "installs", "java"))...)
		return paths
	}
	return nil
}

func scanJVMDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
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
