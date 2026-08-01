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

	"deckanator/internal/settings"
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
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchReleases() ([]release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=30", repo)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: %s", resp.Status)
	}
	var rels []release
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, err
	}
	return rels, nil
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

// version is a parsed tag: core numbers plus the prerelease iteration
// (pre < 0 means a stable release, which outranks any candidate of the
// same core: v0.5.0-rc.2 < v0.5.0).
type version struct {
	nums []int
	pre  int
}

// parseTag turns "v0.4.68" or "v0.5.0-rc.1" into a comparable version.
// Non-release builds (short hashes, "dev") don't parse and never see
// updates.
func parseTag(tag string) (version, bool) {
	s := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	core, suffix, hasSuffix := strings.Cut(s, "-")

	parts := strings.Split(core, ".")
	if len(parts) < 2 {
		return version{}, false
	}
	v := version{nums: make([]int, len(parts)), pre: -1}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return version{}, false
		}
		v.nums[i] = n
	}

	if hasSuffix {
		// Accept rc.N, rcN, beta.N and similar: the trailing number is
		// the candidate iteration; a bare word counts as iteration 0.
		digits := strings.TrimLeft(suffix, "abcdefghijklmnopqrstuvwxyz.")
		if digits == "" {
			v.pre = 0
		} else {
			n, err := strconv.Atoi(digits)
			if err != nil {
				return version{}, false
			}
			v.pre = n
		}
	}
	return v, true
}

func less(a, b version) bool {
	for i := 0; i < len(a.nums) || i < len(b.nums); i++ {
		var av, bv int
		if i < len(a.nums) {
			av = a.nums[i]
		}
		if i < len(b.nums) {
			bv = b.nums[i]
		}
		if av != bv {
			return av < bv
		}
	}
	// Same core: a stable release outranks any candidate.
	if (a.pre < 0) != (b.pre < 0) {
		return a.pre >= 0
	}
	return a.pre < b.pre
}

func newer(current, latest string) bool {
	c, okC := parseTag(current)
	l, okL := parseTag(latest)
	if !okC || !okL {
		return false
	}
	return less(c, l)
}

// Check compares the running version against the newest release the
// selected update channel offers: the stable channel skips release
// candidates, the beta channel includes them.
func Check(current string) (Info, error) {
	info := Info{Supported: supported()}
	rels, err := fetchReleases()
	if err != nil {
		return info, err
	}
	beta := settings.Load().UpdateChannel == "beta"

	var bestTag string
	var best version
	for _, r := range rels {
		if r.Prerelease && !beta {
			continue
		}
		v, ok := parseTag(r.TagName)
		if !ok {
			continue
		}
		if bestTag == "" || less(best, v) {
			bestTag = r.TagName
			best = v
		}
	}
	if bestTag == "" {
		return info, fmt.Errorf("no releases found")
	}
	info.Version = bestTag
	info.Available = info.Supported && newer(current, bestTag)
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
