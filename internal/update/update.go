// Package update checks GitHub releases and swaps the running flatpak
// for a newer bundle. Updates deliberately avoid Flathub: the runtime
// is already installed, so the bundle from a GitHub asset is enough -
// GitHub stays reachable on networks where Flathub is blocked.
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	repo      = "sedyh/deckanator"
	assetName = "deckanator.flatpak"
)

// Info describes the update state shown in the settings panel.
type Info struct {
	Supported bool   `json:"supported"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

// Supported is true only for the flatpak build on Linux: that is the
// only distribution the updater knows how to replace.
func supported() bool {
	return runtime.GOOS == "linux" && os.Getenv("FLATPAK_ID") != ""
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchRelease(tag string) (*release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	if tag != "" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: %s", resp.Status)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// parseTag turns "v0.4.68" into comparable numbers. Non-release builds
// (short hashes, "dev") don't parse and never see updates.
func parseTag(tag string) ([]int, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(tag), "v"), ".")
	if len(parts) < 2 {
		return nil, false
	}
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		nums[i] = n
	}
	return nums, true
}

func newer(current, latest string) bool {
	c, okC := parseTag(current)
	l, okL := parseTag(latest)
	if !okC || !okL {
		return false
	}
	for i := 0; i < len(c) || i < len(l); i++ {
		var cv, lv int
		if i < len(c) {
			cv = c[i]
		}
		if i < len(l) {
			lv = l[i]
		}
		if lv != cv {
			return lv > cv
		}
	}
	return false
}

// Check compares the running version against the latest release.
func Check(current string) (Info, error) {
	info := Info{Supported: supported()}
	rel, err := fetchRelease("")
	if err != nil {
		return info, err
	}
	info.Version = rel.TagName
	info.Available = info.Supported && newer(current, rel.TagName)
	return info, nil
}

// Install downloads the release bundle and installs it over the current
// app. The bundle lands under the real home dir (shared with the host),
// so the host flatpak reached via flatpak-spawn can read it.
func Install(version string, progress func(stage string, current, total int)) error {
	rel, err := fetchRelease(version)
	if err != nil {
		return err
	}
	var url string
	for _, a := range rel.Assets {
		if a.Name == assetName {
			url = a.URL
			break
		}
	}
	if url == "" {
		return fmt.Errorf("release %s has no %s asset", rel.TagName, assetName)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".cache", "deckanator")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, assetName)
	defer func() { _ = os.Remove(path) }()

	if err := download(url, path, progress); err != nil {
		return err
	}

	progress("Installing...", 0, 0)
	args := []string{"install", "--user", "--noninteractive", "--no-related", "--or-update", path}
	var cmd *exec.Cmd
	if os.Getenv("FLATPAK_ID") != "" {
		cmd = exec.Command("flatpak-spawn", append([]string{"--host", "flatpak"}, args...)...)
	} else {
		cmd = exec.Command("flatpak", args...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		return fmt.Errorf("flatpak install: %w: %s", err, msg)
	}
	return nil
}

func download(url, path string, progress func(stage string, current, total int)) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", resp.Status)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	total := int(resp.ContentLength)
	var done int
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			done += n
			progress("Downloading...", done, total)
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}
