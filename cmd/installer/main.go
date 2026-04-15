package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed assets/grid.png
var artGrid []byte

//go:embed assets/poster.png
var artPoster []byte

//go:embed assets/hero.png
var artHero []byte

//go:embed assets/icon.png
var artIcon []byte

const (
	appName    = "Deckanator"
	repo       = "sedyh/deckanator"
	installDir = ".local/share/deckanator"
	exeName    = "Deckanator"
)

// ── VDF binary format ──────────────────────────────────────────────────────

const (
	vdfMap    byte = 0x00
	vdfString byte = 0x01
	vdfInt32  byte = 0x02
	vdfEnd    byte = 0x08
)

type node struct {
	t     byte
	str   string
	num   int32
	sub   map[string]*node
	order []string
}

func newMap() *node { return &node{t: vdfMap, sub: make(map[string]*node)} }
func newStr(s string) *node    { return &node{t: vdfString, str: s} }
func newInt(v int32) *node     { return &node{t: vdfInt32, num: v} }

func (n *node) set(key string, child *node) {
	if _, exists := n.sub[key]; !exists {
		n.order = append(n.order, key)
	}
	n.sub[key] = child
}

func readVDF(data []byte) *node {
	r := bytes.NewReader(data)
	root := newMap()
	readChildren(r, root)
	return root
}

func readChildren(r *bytes.Reader, parent *node) {
	for {
		t, err := r.ReadByte()
		if err != nil || t == vdfEnd {
			return
		}
		key := readCString(r)
		switch t {
		case vdfMap:
			child := newMap()
			readChildren(r, child)
			parent.set(key, child)
		case vdfString:
			parent.set(key, newStr(readCString(r)))
		case vdfInt32:
			var v int32
			binary.Read(r, binary.LittleEndian, &v)
			parent.set(key, newInt(v))
		}
	}
}

func readCString(r *bytes.Reader) string {
	var buf []byte
	for {
		b, err := r.ReadByte()
		if err != nil || b == 0 {
			break
		}
		buf = append(buf, b)
	}
	return string(buf)
}

func writeVDF(root *node) []byte {
	var buf bytes.Buffer
	writeChildren(&buf, root)
	buf.WriteByte(vdfEnd)
	return buf.Bytes()
}

func writeChildren(buf *bytes.Buffer, n *node) {
	for _, k := range n.order {
		writeNode(buf, k, n.sub[k])
	}
}

func writeNode(buf *bytes.Buffer, key string, n *node) {
	buf.WriteByte(n.t)
	writeCString(buf, key)
	switch n.t {
	case vdfMap:
		writeChildren(buf, n)
		buf.WriteByte(vdfEnd)
	case vdfString:
		writeCString(buf, n.str)
	case vdfInt32:
		binary.Write(buf, binary.LittleEndian, n.num)
	}
}

func writeCString(buf *bytes.Buffer, s string) {
	buf.WriteString(s)
	buf.WriteByte(0)
}

// ── Steam helpers ──────────────────────────────────────────────────────────

func computeAppID(exe, name string) uint32 {
	crc := crc32.ChecksumIEEE([]byte(exe + name))
	return crc | 0x80000000
}

func findSteamDir(home string) string {
	candidates := []string{
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".steam", "Steam"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(filepath.Join(p, "userdata")); err == nil {
			return p
		}
	}
	return ""
}

func findSteamUser(steamDir string) string {
	userdataDir := filepath.Join(steamDir, "userdata")
	entries, err := os.ReadDir(userdataDir)
	if err != nil {
		return ""
	}

	var best string
	var bestTime int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// skip "0" which is a placeholder
		if e.Name() == "0" {
			continue
		}
		sc := filepath.Join(userdataDir, e.Name(), "config", "shortcuts.vdf")
		fi, err := os.Stat(sc)
		if err != nil {
			// no shortcuts.vdf yet - still a valid user if directory exists
			if best == "" {
				best = e.Name()
			}
			continue
		}
		if fi.ModTime().Unix() > bestTime {
			bestTime = fi.ModTime().Unix()
			best = e.Name()
		}
	}
	return best
}

func buildShortcut(appID uint32, name, exe, startDir, iconPath, launchOptions string) *node {
	n := newMap()
	n.set("appid", newInt(int32(appID)))
	n.set("AppName", newStr(name))
	n.set("Exe", newStr(exe))
	n.set("StartDir", newStr(startDir))
	n.set("icon", newStr(iconPath))
	n.set("ShortcutPath", newStr(""))
	n.set("LaunchOptions", newStr(launchOptions))
	n.set("IsHidden", newInt(0))
	n.set("AllowDesktopConfig", newInt(1))
	n.set("AllowOverlay", newInt(1))
	n.set("openvr", newInt(0))
	n.set("Devkit", newInt(0))
	n.set("DevkitGameID", newStr(""))
	n.set("DevkitOverrideAppID", newInt(0))
	n.set("LastPlayTime", newInt(0))
	n.set("FlatpakAppID", newStr(""))
	n.set("tags", newMap())
	return n
}

func addToShortcuts(steamDir, userID, exeField, launchOptions, startDir, iconPath string, appID uint32) error {
	scDir := filepath.Join(steamDir, "userdata", userID, "config")
	scPath := filepath.Join(scDir, "shortcuts.vdf")

	var root *node
	data, err := os.ReadFile(scPath)
	if err != nil {
		root = newMap()
		root.set("shortcuts", newMap())
	} else {
		root = readVDF(data)
		if root.sub["shortcuts"] == nil {
			root.set("shortcuts", newMap())
		}
	}

	shortcuts := root.sub["shortcuts"]

	// find existing entry by AppName, or assign next index
	existingKey := ""
	maxIdx := -1
	for _, k := range shortcuts.order {
		idx, _ := strconv.Atoi(k)
		if idx > maxIdx {
			maxIdx = idx
		}
		if e := shortcuts.sub[k]; e != nil {
			if nameNode := e.sub["AppName"]; nameNode != nil && nameNode.str == appName {
				existingKey = k
			}
		}
	}

	key := existingKey
	if key == "" {
		key = strconv.Itoa(maxIdx + 1)
	}

	// stop Steam before modifying shortcuts.vdf - otherwise Steam overwrites our changes on exit
	exec.Command("pkill", "-x", "steam").Run()
	exec.Command("pkill", "-x", "Steam").Run()
	// wait until Steam process is actually gone (up to 5s)
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		out, _ := exec.Command("pgrep", "-x", "steam").Output()
		out2, _ := exec.Command("pgrep", "-x", "Steam").Output()
		if len(out) == 0 && len(out2) == 0 {
			break
		}
		if i == 9 {
			exec.Command("pkill", "-9", "-x", "steam").Run()
			exec.Command("pkill", "-9", "-x", "Steam").Run()
			time.Sleep(500 * time.Millisecond)
		}
	}

	shortcuts.set(key, buildShortcut(appID, appName, exeField, startDir, iconPath, launchOptions))

	// sort keys numerically for clean output
	sort.Slice(shortcuts.order, func(i, j int) bool {
		a, _ := strconv.Atoi(shortcuts.order[i])
		b, _ := strconv.Atoi(shortcuts.order[j])
		return a < b
	})

	if err := os.MkdirAll(scDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(scPath, writeVDF(root), 0644)
}

func removeFromShortcuts(steamDir, userID string) error {
	scPath := filepath.Join(steamDir, "userdata", userID, "config", "shortcuts.vdf")
	data, err := os.ReadFile(scPath)
	if err != nil {
		return nil
	}
	root := readVDF(data)
	shortcuts := root.sub["shortcuts"]
	if shortcuts == nil {
		return nil
	}

	removeKey := ""
	for _, k := range shortcuts.order {
		if e := shortcuts.sub[k]; e != nil {
			if nameNode := e.sub["AppName"]; nameNode != nil && nameNode.str == appName {
				removeKey = k
				break
			}
		}
	}
	if removeKey == "" {
		return nil
	}

	delete(shortcuts.sub, removeKey)
	newOrder := shortcuts.order[:0]
	for _, k := range shortcuts.order {
		if k != removeKey {
			newOrder = append(newOrder, k)
		}
	}
	shortcuts.order = newOrder

	return os.WriteFile(scPath, writeVDF(root), 0644)
}

func removeArtwork(steamDir, userID string, appID uint32) {
	gridDir := filepath.Join(steamDir, "userdata", userID, "config", "grid")
	id := strconv.FormatUint(uint64(appID), 10)
	for _, name := range []string{
		id + ".png", id + "p.png", id + "_hero.png", id + "_logo.png", id + "_icon.png",
	} {
		os.Remove(filepath.Join(gridDir, name))
	}
}

func uninstall(home, steamDir, userID string, appID uint32) {
	binPath := filepath.Join(home, installDir, exeName)
	iconPath := filepath.Join(home, installDir, "icon.png")
	desktopDir := filepath.Join(home, ".local", "share", "applications")

	step(1, "Removing binary...")
	os.Remove(binPath)
	os.Remove(iconPath)
	os.Remove(filepath.Join(home, installDir))

	step(2, "Removing .desktop entries...")
	os.Remove(filepath.Join(desktopDir, "deckanator.desktop"))
	os.Remove(filepath.Join(desktopDir, "deckanator-uninstall.desktop"))

	if steamDir != "" && userID != "" {
		step(3, "Removing Steam shortcut...")
		if err := removeFromShortcuts(steamDir, userID); err != nil {
			fmt.Fprintf(os.Stderr, "    warning: %v\n", err)
		} else {
			fmt.Println("    done")
		}

		step(4, "Removing Steam artwork...")
		removeArtwork(steamDir, userID, appID)
		fmt.Println("    done")
	}

	fmt.Println("\nDeckanator uninstalled. Restart Steam to apply changes.")
}

func copyArtwork(steamDir, userID string, appID uint32, iconPath string) error {
	gridDir := filepath.Join(steamDir, "userdata", userID, "config", "grid")
	if err := os.MkdirAll(gridDir, 0755); err != nil {
		return err
	}

	id := strconv.FormatUint(uint64(appID), 10)

	files := map[string][]byte{
		id + ".png":        artGrid,
		id + "p.png":       artPoster,
		id + "_hero.png":   artHero,
		id + "_logo.png":   artIcon,
		id + "_icon.png":   artIcon,
	}
	for name, data := range files {
		dst := filepath.Join(gridDir, name)
		// don't overwrite if user already placed custom artwork
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// ── GitHub release download ────────────────────────────────────────────────

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchRelease(version string) (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	if version != "" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, version)
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func downloadFile(url, dst string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	// kill running process before replacing binary ("text file busy")
	exec.Command("pkill", "-f", dst).Run()

	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err = io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()

	return os.Rename(tmp, dst)
}

func ensureWebkit() error {
	out, _ := exec.Command("ldconfig", "-p").Output()
	if strings.Contains(string(out), "libwebkit2gtk-4.1") {
		fmt.Println("    found")
		return nil
	}
	return fmt.Errorf("libwebkit2gtk-4.1 not found - install webkit2gtk-4.1 manually")
}

func step(n int, msg string) {
	fmt.Printf("[%d] %s\n", n, msg)
}

func fail(msg string, err error) {
	fmt.Fprintf(os.Stderr, "error: %s: %v\n", msg, err)
	os.Exit(1)
}

// ── Main ───────────────────────────────────────────────────────────────────

const flatpakAppID = "io.github.sedyh.Deckanator"

func main() {
	version      := flag.String("version", "", "version to install (e.g. v0.2.0), default: latest")
	skipDownload := flag.Bool("skip-download", false, "skip downloading binary (use existing)")
	doUninstall  := flag.Bool("uninstall", false, "remove Deckanator and Steam shortcut")
	useFlatpak   := flag.Bool("flatpak", false, "configure Steam for Flatpak installation")
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		fail("home dir", err)
	}

	binPath  := filepath.Join(home, installDir, exeName)
	iconPath := filepath.Join(home, installDir, "icon.png")

	// exe and launch options for Steam shortcut
	var exeField, launchOptions string
	if *useFlatpak {
		exeField = "/usr/bin/flatpak"
		launchOptions = "run " + flatpakAppID
	} else {
		exeField = "\"" + binPath + "\""
		launchOptions = ""
	}
	// appID must be stable across install methods - always derive from flatpak ID
	appID := computeAppID("/usr/bin/flatpak", appName)

	steamDir := findSteamDir(home)
	userID := ""
	if steamDir != "" {
		userID = findSteamUser(steamDir)
	}

	if *doUninstall {
		fmt.Printf("Deckanator uninstaller\n\n")
		uninstall(home, steamDir, userID, appID)
		return
	}

	fmt.Printf("Deckanator installer\n")
	fmt.Printf("App ID: %d\n\n", appID)

	if !*useFlatpak {
		// 0. System dependencies (only needed for direct binary)
		step(0, "Checking dependencies...")
		if err := ensureWebkit(); err != nil {
			fmt.Fprintf(os.Stderr, "    warning: %v\n", err)
			fmt.Fprintln(os.Stderr, "    try manually: sudo steamos-readonly disable && sudo pacman -S --noconfirm webkit2gtk")
		}
	}

	// 1. Download binary
	if !*useFlatpak && !*skipDownload {
		step(1, "Fetching release info...")
		rel, err := fetchRelease(*version)
		if err != nil {
			fail("fetch release", err)
		}
		fmt.Printf("    version: %s\n", rel.TagName)

		assetURL := ""
		for _, a := range rel.Assets {
			if strings.Contains(a.Name, "linux") && strings.Contains(a.Name, "amd64") && !strings.Contains(a.Name, "installer") {
				assetURL = a.BrowserDownloadURL
				break
			}
		}
		if assetURL == "" {
			fmt.Fprintln(os.Stderr, "error: linux/amd64 asset not found in release")
			os.Exit(1)
		}

		step(1, fmt.Sprintf("Downloading %s...", rel.TagName))
		if err := downloadFile(assetURL, binPath); err != nil {
			fail("download binary", err)
		}
		fmt.Printf("    installed to %s\n", binPath)
	} else if !*useFlatpak {
		step(1, fmt.Sprintf("Skipping download, using %s", binPath))
	} else {
		step(1, "Using Flatpak installation")
	}

	// 2. Icon (always install for Steam artwork)
	step(2, "Installing icon...")
	if err := os.MkdirAll(filepath.Dir(iconPath), 0755); err != nil {
		fail("create install dir", err)
	}
	if err := os.WriteFile(iconPath, artIcon, 0644); err != nil {
		fail("write icon", err)
	}

	// 3. .desktop entries
	step(3, "Creating .desktop entries...")
	installerPath, _ := os.Executable()
	desktopDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(desktopDir, 0755); err != nil {
		fail("create desktop dir", err)
	}
	writeDesktop := func(name, filename, exec, comment string) {
		content := strings.Join([]string{
			"[Desktop Entry]",
			"Name=" + name,
			"Comment=" + comment,
			"Exec=" + exec,
			"Icon=" + iconPath,
			"Type=Application",
			"Categories=Game;",
			"StartupNotify=false",
		}, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(desktopDir, filename), []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "    warning: %v\n", err)
		}
	}
	launchExec := binPath
	if *useFlatpak {
		launchExec = "flatpak run " + flatpakAppID
	}
	writeDesktop("Deckanator", "deckanator.desktop", launchExec, "Minecraft launcher for Steam Deck")
	writeDesktop("Uninstall Deckanator", "deckanator-uninstall.desktop",
		installerPath+" --uninstall", "Remove Deckanator and Steam shortcut")

	// 4. Steam integration
	step(4, "Looking for Steam...")
	if steamDir == "" {
		fmt.Println("    Steam not found - skipping Steam integration")
		printDone("")
		return
	}
	fmt.Printf("    found: %s\n", steamDir)

	if userID == "" {
		fmt.Println("    no Steam users found - skipping Steam integration")
		printDone("")
		return
	}
	fmt.Printf("    user: %s\n", userID)

	step(5, "Adding to Steam shortcuts...")
	if err := addToShortcuts(steamDir, userID, exeField, launchOptions, filepath.Dir(binPath), iconPath, appID); err != nil {
		fmt.Fprintf(os.Stderr, "    warning: could not update shortcuts.vdf: %v\n", err)
	} else {
		fmt.Println("    done")
	}

	step(6, "Copying Steam artwork...")
	if err := copyArtwork(steamDir, userID, appID, iconPath); err != nil {
		fmt.Fprintf(os.Stderr, "    warning: could not copy artwork: %v\n", err)
	} else {
		fmt.Println("    done (existing artwork preserved)")
	}

	// Reload Steam shortcuts without full restart (best-effort)
	exec.Command("bash", "-c",
		`if pgrep -x steam > /dev/null; then steam steam://open/games 2>/dev/null; fi`,
	).Run()

	if *useFlatpak {
		printDone("Run: flatpak run " + flatpakAppID)
	} else {
		printDone("Binary: " + binPath)
	}
}

func printDone(info string) {
	fmt.Println()
	fmt.Println("Done!")
	if info != "" {
		fmt.Println(info)
	}
	fmt.Println("Restart Steam to see Deckanator in your library.")
}
