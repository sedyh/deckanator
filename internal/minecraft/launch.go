package minecraft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"deckanator/internal/config"
	"deckanator/internal/maven"
	"deckanator/internal/profile"
)

// blockedJVMArgs are JVM flags from modern manifests that require a
// newer JDK than the one Mojang ships with the legacy manifest.
var blockedJVMArgs = []string{
	"--sun-misc-unsafe-memory-access",
}

// LaunchOptions bundles the optional external dependencies Launch needs.
// Keeping them explicit avoids an import cycle between this package and
// the java runtime resolver.
type LaunchOptions struct {
	EnsureJava func(component string, progress ProgressFunc) (string, error)
}

// Launch starts Minecraft for the given profile. It blocks up to 5s to
// confirm the JVM process survived startup; after that, it returns nil
// and lets the process keep running in the background.
func Launch(p profile.Profile, opts LaunchOptions) error {
	dir := config.GameDir()
	mcVersion := p.MCVersion
	if mcVersion == "" {
		return fmt.Errorf("no version selected")
	}

	vanilla, err := loadVersionDetailsFile(filepath.Join(dir, "versions", mcVersion, mcVersion+".json"))
	if err != nil {
		return fmt.Errorf("version json: %w", err)
	}

	component := "java-runtime-gamma"
	if vanilla.JavaVersion != nil && vanilla.JavaVersion.Component != "" {
		component = vanilla.JavaVersion.Component
	}
	java, err := opts.EnsureJava(component, func(string, int, int) {})
	if err != nil {
		return err
	}

	mainClass := vanilla.MainClass
	var extraLibs []Library

	if p.Loader == loaderFabric && p.FabricLoaderVersion != "" {
		id := fabricProfileID(mcVersion, p.FabricLoaderVersion)
		fabric, err := loadFabricProfileFile(filepath.Join(dir, "versions", id, id+".json"))
		if err != nil {
			return fmt.Errorf("fabric json: %w", err)
		}
		mainClass = fabric.MainClass
		extraLibs = fabric.Libraries
	}

	libDir := filepath.Join(dir, "libraries")
	nativesDir := filepath.Join(dir, "versions", mcVersion, "natives")
	gameDir := filepath.Join(dir, "profiles", p.ID)
	assetsDir := filepath.Join(dir, "assets")

	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return err
	}

	allLibs := append(FilterLibraries(vanilla.Libraries), FilterLibraries(extraLibs)...)
	classpath := buildClasspath(allLibs, libDir, filepath.Join(dir, "versions", mcVersion, mcVersion+".jar"))

	playerName := p.PlayerName
	if playerName == "" {
		playerName = "Player"
	}

	vars := map[string]string{
		"auth_player_name":  playerName,
		"version_name":      mcVersion,
		"game_directory":    gameDir,
		"assets_root":       assetsDir,
		"assets_index_name": vanilla.AssetIndex.ID,
		"auth_uuid":         "00000000-0000-0000-0000-000000000000",
		"auth_access_token": "0",
		"clientid":          "",
		"auth_xuid":         "",
		"user_type":         "legacy",
		"version_type":      "release",
		"natives_directory": nativesDir,
		"launcher_name":     "deckanator",
		"launcher_version":  "1.0",
		"classpath":         classpath,
	}

	jvmArgs, gameArgs := buildArgs(vanilla, vars)
	jvmArgs = append(jvmArgs, mainClass)
	jvmArgs = append(jvmArgs, gameArgs...)

	logPath := filepath.Join(dir, "launcher.log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)

	var stderrBuf bytes.Buffer
	cmd := exec.Command(java, jvmArgs...)
	cmd.Dir = gameDir
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	} else {
		cmd.Stderr = &stderrBuf
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if logFile != nil {
			_ = logFile.Close()
		}
		tail := extractLogTail(logPath)
		if err != nil {
			if tail != "" {
				return fmt.Errorf("minecraft crashed (%w):\n%s", err, tail)
			}
			return fmt.Errorf("minecraft crashed immediately (%w) - log: %s", err, logPath)
		}
		if tail != "" {
			return fmt.Errorf("minecraft exited immediately:\n%s", tail)
		}
		return fmt.Errorf("minecraft exited immediately - log: %s", logPath)
	case <-time.After(5 * time.Second):
		go func() {
			_ = cmd.Wait()
			if logFile != nil {
				_ = logFile.Close()
			}
		}()
		return nil
	}
}

func buildArgs(v *VersionDetails, vars map[string]string) (jvm, game []string) {
	switch {
	case v.Arguments != nil:
		jvm = filterJVMArgs(extractArgs(v.Arguments.JVM, vars))
		game = extractArgs(v.Arguments.Game, vars)
	case v.MinecraftArguments != "":
		jvm = defaultJVMArgs(vars)
		for _, arg := range strings.Fields(v.MinecraftArguments) {
			game = append(game, applyTemplate(arg, vars))
		}
	}
	return jvm, game
}

func buildClasspath(libs []Library, libDir, clientJar string) string {
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	parts := make([]string, 0, len(libs)+1)
	seen := make(map[string]bool, len(libs))
	for _, lib := range libs {
		var rel string
		switch {
		case lib.Downloads != nil && lib.Downloads.Artifact != nil:
			rel = lib.Downloads.Artifact.Path
		case lib.Name != "":
			rel = maven.LocalPath(lib.Name)
		}
		if rel == "" {
			continue
		}
		path := filepath.Join(libDir, rel)
		if seen[path] {
			continue
		}
		seen[path] = true
		parts = append(parts, path)
	}
	parts = append(parts, clientJar)
	return strings.Join(parts, sep)
}

func extractArgs(raw []any, vars map[string]string) []string {
	var out []string
	for _, r := range raw {
		switch v := r.(type) {
		case string:
			out = append(out, applyTemplate(v, vars))
		case map[string]any:
			rules, _ := v["rules"].([]any)
			if !checkArgRules(rules) {
				continue
			}
			switch val := v["value"].(type) {
			case string:
				out = append(out, applyTemplate(val, vars))
			case []any:
				for _, s := range val {
					if str, ok := s.(string); ok {
						out = append(out, applyTemplate(str, vars))
					}
				}
			}
		}
	}
	return out
}

func checkArgRules(rules []any) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range rules {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		action, _ := rule["action"].(string)
		osRule, hasOS := rule["os"].(map[string]any)
		features, hasFeatures := rule["features"].(map[string]any)

		if hasFeatures && len(features) > 0 {
			continue
		}
		if hasOS {
			if name, _ := osRule["name"].(string); name == config.OSName() {
				allowed = action == ruleAllow
			}
			continue
		}
		allowed = action == "allow"
	}
	return allowed
}

func applyTemplate(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return s
}

func defaultJVMArgs(vars map[string]string) []string {
	return []string{
		"-Xss1M",
		"-Djava.library.path=" + vars["natives_directory"],
		"-Dminecraft.launcher.brand=" + vars["launcher_name"],
		"-Dminecraft.launcher.version=" + vars["launcher_version"],
		"-cp", vars["classpath"],
	}
}

func filterJVMArgs(args []string) []string {
	out := args[:0:0]
	for _, a := range args {
		skip := false
		for _, b := range blockedJVMArgs {
			if strings.HasPrefix(a, b) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, a)
		}
	}
	return out
}

func extractLogTail(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil || len(data) == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var relevant []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "Error") ||
			strings.HasPrefix(l, "Exception") ||
			strings.HasPrefix(l, "Caused by") ||
			strings.HasPrefix(l, "Unrecognized") ||
			strings.Contains(l, "fatal") ||
			strings.Contains(l, "Fatal") {
			relevant = append(relevant, l)
		}
	}
	if len(relevant) == 0 {
		n := 5
		if len(lines) < n {
			n = len(lines)
		}
		relevant = lines[len(lines)-n:]
	}
	if len(relevant) > 6 {
		relevant = relevant[len(relevant)-6:]
	}
	return strings.Join(relevant, "\n")
}

func loadVersionDetailsFile(path string) (*VersionDetails, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d VersionDetails
	return &d, json.Unmarshal(data, &d)
}

func loadFabricProfileFile(path string) (*FabricProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p FabricProfile
	return &p, json.Unmarshal(data, &p)
}
