package minecraft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	// OnStarted is called with the game process right after it starts,
	// letting the caller expose a kill switch for hung games.
	OnStarted func(p *os.Process)
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

	// A profile whose saved loader version was lost must not silently
	// launch as vanilla: recover the version from the versions dir.
	if IsFabricLike(p.Loader) && p.FabricLoaderVersion == "" {
		p.FabricLoaderVersion = InstalledLoaderVersion(p.Loader, mcVersion)
	}

	if IsFabricLike(p.Loader) && p.FabricLoaderVersion != "" {
		id := loaderProfileID(p.Loader, mcVersion, p.FabricLoaderVersion)
		prof, err := loadFabricProfileFile(filepath.Join(dir, "versions", id, id+".json"))
		if err != nil {
			return fmt.Errorf("%s json: %w", p.Loader, err)
		}
		mainClass = prof.MainClass
		extraLibs = prof.Libraries
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
	if IsFabricLike(p.Loader) {
		// Suppress the loader's own Swing error dialog so crashes surface
		// only through our error panel. fabric.noGui covers Fabric;
		// loader.gui.disabled covers Quilt.
		jvmArgs = append(jvmArgs, "-Dfabric.noGui=true", "-Dloader.gui.disabled=true")
	}
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
	if opts.OnStarted != nil {
		opts.OnStarted(cmd.Process)
	}

	// Block until the game exits: a crash at any point (not just within
	// a startup window) must surface its trace instead of the launcher
	// quitting itself while the game is still booting.
	started := time.Now()
	waitErr, hung := waitWithWatchdog(cmd, logPath)
	ran := time.Since(started)
	if logFile != nil {
		_ = logFile.Close()
	}
	tail, foundChain := extractLogTail(logPath)
	if hung {
		if tail != "" {
			return fmt.Errorf("minecraft hung during startup and was stopped\n\n%s", tail)
		}
		return fmt.Errorf("minecraft hung during startup and was stopped - log: %s", logPath)
	}
	if waitErr != nil {
		if tail != "" {
			return fmt.Errorf("minecraft crashed (%w)\n\n%s", waitErr, tail)
		}
		return fmt.Errorf("minecraft crashed (%w) - log: %s", waitErr, logPath)
	}
	// Exit code 0 can still be a crash: a dead main thread winds the JVM
	// down "cleanly" (Quilt's error path does exactly this). A short run
	// that left an exception chain in the log is not a normal quit.
	if foundChain && ran < 2*time.Minute {
		return fmt.Errorf("minecraft exited unexpectedly\n\n%s", tail)
	}
	return nil
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

// waitWithWatchdog waits for the game to exit. During the startup
// window a booting game logs constantly, so a long silence there means
// it hung (e.g. a loader stuck on an error it can't display): the
// process is killed and hung=true is returned. After the window the
// game is assumed interactive and quiet logs are normal.
func waitWithWatchdog(cmd *exec.Cmd, logPath string) (waitErr error, hung bool) {
	const (
		startupWindow = 5 * time.Minute
		stallLimit    = 90 * time.Second
		pollEvery     = 2 * time.Second
	)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.Now().Add(startupWindow)
	lastSize := int64(-1)
	lastGrowth := time.Now()
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			return err, false
		case <-ticker.C:
			if time.Now().After(deadline) {
				continue
			}
			if fi, err := os.Stat(logPath); err == nil && fi.Size() != lastSize {
				lastSize = fi.Size()
				lastGrowth = time.Now()
				continue
			}
			if time.Since(lastGrowth) > stallLimit {
				_ = cmd.Process.Kill()
				return <-done, true
			}
		}
	}
}

// exceptionHeaderRe matches a Java exception header line: an optional
// "Caused by:" prefix, a dotted class path ending in Exception/Error/
// Throwable, and an optional message.
var exceptionHeaderRe = regexp.MustCompile(`^(Exception in thread "[^"]*" )?(Caused by: |Suppressed: )?([\w$]+\.)+[\w$]+(Exception|Error|Throwable)\b`)

func isFrameLine(s string) bool {
	return strings.HasPrefix(s, "at ") || strings.HasPrefix(s, "... ")
}

func isHeaderLine(s string) bool {
	return !isFrameLine(s) && exceptionHeaderRe.MatchString(s)
}

// extractLogTail pulls the last exception chain out of the launcher
// log. The boolean reports whether an actual exception chain was found
// (as opposed to the generic last-lines fallback).
func extractLogTail(logPath string) (string, bool) {
	data, err := os.ReadFile(logPath)
	if err != nil || len(data) == 0 {
		return "", false
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	// Walk up from the end over frame/header lines; the topmost header of
	// that contiguous block starts the final exception chain.
	end, topHeader := -1, -1
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			if end != -1 {
				break
			}
			continue
		}
		switch {
		case isFrameLine(t):
			if end == -1 {
				end = i
			}
		case isHeaderLine(t):
			if end == -1 {
				end = i
			}
			topHeader = i
		default:
			if end != -1 {
				i = -1 // stop: left the exception block
			}
		}
		if end != -1 && i == -1 {
			break
		}
	}
	if topHeader >= 0 && end >= topHeader {
		return formatExceptionChain(lines[topHeader : end+1]), true
	}

	// No exception chain: fall back to the last few meaningful lines.
	tail := make([]string, 0, 5)
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			tail = append(tail, t)
		}
	}
	if len(tail) > 5 {
		tail = tail[len(tail)-5:]
	}
	return strings.Join(tail, "\n"), false
}

// formatExceptionChain re-indents a header+frames block. The full chain
// is kept (the UI scrolls it, pinned to the end where the root cause
// lives); only absurdly long chains are trimmed from the top.
func formatExceptionChain(chain []string) string {
	var out []string
	for _, l := range chain {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if isFrameLine(t) {
			out = append(out, "  "+t)
			continue
		}
		out = append(out, t)
	}
	const maxLines = 200
	if len(out) > maxLines {
		skipped := len(out) - maxLines
		out = append([]string{fmt.Sprintf("… %d earlier lines …", skipped)}, out[len(out)-maxLines:]...)
	}
	return strings.Join(out, "\n")
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
