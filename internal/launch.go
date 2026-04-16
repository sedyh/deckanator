package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

// Launch starts Minecraft and waits up to 5s to confirm the process is alive.
// Returns nil if the process is still running after that time.
func Launch(p Profile) error {
	dir := GameDir()
	mcVersion := p.MCVersion
	if mcVersion == "" {
		return fmt.Errorf("no version selected")
	}

	versionDetailsPath := filepath.Join(dir, "versions", mcVersion, mcVersion+".json")
	vanillaDetails, err := loadVersionDetailsFile(versionDetailsPath)
	if err != nil {
		return fmt.Errorf("version json: %w", err)
	}

	component := "java-runtime-gamma"
	if vanillaDetails.JavaVersion != nil && vanillaDetails.JavaVersion.Component != "" {
		component = vanillaDetails.JavaVersion.Component
	}
	java, err := EnsureJava(component, func(string, int, int) {})
	if err != nil {
		return err
	}

	mainClass := vanillaDetails.MainClass
	var extraLibs []Library

	if p.Loader == "fabric" && p.FabricLoaderVersion != "" {
		id := fabricProfileID(mcVersion, p.FabricLoaderVersion)
		fabricPath := filepath.Join(dir, "versions", id, id+".json")
		fabricProf, err := loadFabricProfileFile(fabricPath)
		if err != nil {
			return fmt.Errorf("fabric json: %w", err)
		}
		mainClass = fabricProf.MainClass
		extraLibs = fabricProf.Libraries
	}

	libDir := filepath.Join(dir, "libraries")
	nativesDir := filepath.Join(dir, "versions", mcVersion, "natives")
	gameDir := filepath.Join(dir, "profiles", p.ID)
	assetsDir := filepath.Join(dir, "assets")

	if err := os.MkdirAll(gameDir, 0755); err != nil {
		return err
	}

	allLibs := filterLibraries(vanillaDetails.Libraries)
	allLibs = append(allLibs, filterLibraries(extraLibs)...)
	classpath := buildClasspath(allLibs, libDir, filepath.Join(dir, "versions", mcVersion, mcVersion+".jar"))

	playerName := p.PlayerName
	if playerName == "" {
		playerName = "Player"
	}
	playerUUID := "00000000-0000-0000-0000-000000000000"
	accessToken := "0"

	vars := map[string]string{
		"auth_player_name":  playerName,
		"version_name":      mcVersion,
		"game_directory":    gameDir,
		"assets_root":       assetsDir,
		"assets_index_name": vanillaDetails.AssetIndex.ID,
		"auth_uuid":         playerUUID,
		"auth_access_token": accessToken,
		"clientid":          "",
		"auth_xuid":         "",
		"user_type":         "legacy",
		"version_type":      "release",
		"natives_directory": nativesDir,
		"launcher_name":     "deckanator",
		"launcher_version":  "1.0",
		"classpath":         classpath,
	}

	var jvmArgs []string
	var gameArgs []string

	if vanillaDetails.Arguments != nil {
		jvmArgs = filterJVMArgs(extractArgs(vanillaDetails.Arguments.JVM, vars))
		gameArgs = extractArgs(vanillaDetails.Arguments.Game, vars)
	} else if vanillaDetails.MinecraftArguments != "" {
		jvmArgs = defaultJVMArgs(vars)
		for _, arg := range strings.Fields(vanillaDetails.MinecraftArguments) {
			gameArgs = append(gameArgs, applyTemplate(arg, vars))
		}
	}

	args := append(jvmArgs, mainClass)
	args = append(args, gameArgs...)

	logPath := filepath.Join(GameDir(), "launcher.log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)

	var stderrBuf bytes.Buffer
	cmd := exec.Command(java, args...)
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
			logFile.Close()
		}
		tail := extractLogTail(logPath)
		if err != nil {
			if tail != "" {
				return fmt.Errorf("Minecraft crashed (%v):\n%s", err, tail)
			}
			return fmt.Errorf("Minecraft crashed immediately (%v) - log: %s", err, logPath)
		}
		if tail != "" {
			return fmt.Errorf("Minecraft exited immediately:\n%s", tail)
		}
		return fmt.Errorf("Minecraft exited immediately - log: %s", logPath)
	case <-time.After(5 * time.Second):
		go func() {
			cmd.Wait()
			if logFile != nil {
				logFile.Close()
			}
		}()
		return nil
	}
}


func buildClasspath(libs []Library, libDir, clientJar string) string {
	sep := ":"
	if goruntime.GOOS == "windows" {
		sep = ";"
	}
	var parts []string
	seen := map[string]bool{}
	for _, lib := range libs {
		var rel string
		if lib.Downloads != nil && lib.Downloads.Artifact != nil {
			rel = lib.Downloads.Artifact.Path
		} else if lib.Name != "" {
			rel = mavenLocalPath(lib.Name)
		}
		if rel == "" {
			continue
		}
		path := filepath.Join(libDir, rel)
		if !seen[path] {
			parts = append(parts, path)
			seen[path] = true
		}
	}
	parts = append(parts, clientJar)
	return strings.Join(parts, sep)
}

func extractArgs(rawArgs []interface{}, vars map[string]string) []string {
	var result []string
	for _, raw := range rawArgs {
		switch v := raw.(type) {
		case string:
			result = append(result, applyTemplate(v, vars))
		case map[string]interface{}:
			rules, _ := v["rules"].([]interface{})
			if !checkArgRules(rules) {
				continue
			}
			switch val := v["value"].(type) {
			case string:
				result = append(result, applyTemplate(val, vars))
			case []interface{}:
				for _, s := range val {
					if str, ok := s.(string); ok {
						result = append(result, applyTemplate(str, vars))
					}
				}
			}
		}
	}
	return result
}

func checkArgRules(rules []interface{}) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range rules {
		rule, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		action, _ := rule["action"].(string)
		osRule, hasOS := rule["os"].(map[string]interface{})
		features, hasFeatures := rule["features"].(map[string]interface{})

		if hasFeatures && len(features) > 0 {
			continue
		}
		if hasOS {
			osName_, _ := osRule["name"].(string)
			if osName_ == osName() {
				allowed = action == "allow"
			}
		} else {
			allowed = action == "allow"
		}
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

// filterJVMArgs removes JVM flags that require Java 23+ but Mojang ships Java 21.
func filterJVMArgs(args []string) []string {
	blocked := []string{
		"--sun-misc-unsafe-memory-access",
	}
	out := args[:0:0]
	for _, a := range args {
		skip := false
		for _, b := range blocked {
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
