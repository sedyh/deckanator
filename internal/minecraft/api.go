package minecraft

import (
	"fmt"
	"strings"
	"time"

	"deckanator/internal/request"
)

// versionListTTL is how long version lists (Mojang manifest, loader and
// game version lists) are served from cache before re-fetching.
const versionListTTL = time.Hour

const manifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"

// loaderEndpoints describes the meta API of a fabric-like loader (Quilt
// deliberately mirrors Fabric's API and profile JSON format).
type loaderEndpoints struct {
	loadersByGame string
	game          string
	profile       string
}

var fabricLike = map[string]loaderEndpoints{
	"fabric": {
		loadersByGame: "https://meta.fabricmc.net/v2/versions/loader/%s",
		game:          "https://meta.fabricmc.net/v2/versions/game",
		profile:       "https://meta.fabricmc.net/v2/versions/loader/%s/%s/profile/json",
	},
	"quilt": {
		loadersByGame: "https://meta.quiltmc.org/v3/versions/loader/%s",
		game:          "https://meta.quiltmc.org/v3/versions/game",
		profile:       "https://meta.quiltmc.org/v3/versions/loader/%s/%s/profile/json",
	},
}

// IsFabricLike reports whether loader uses a Fabric-style meta API and
// launch profile.
func IsFabricLike(loader string) bool {
	_, ok := fabricLike[loader]
	return ok
}

// FetchVanillaVersions returns only "release" versions from Mojang's
// manifest, preserving manifest order (newest first).
func FetchVanillaVersions() ([]VersionEntry, error) {
	m, err := fetchManifest()
	if err != nil {
		return nil, err
	}
	releases := make([]VersionEntry, 0, len(m.Versions))
	for _, v := range m.Versions {
		if v.Type == "release" {
			releases = append(releases, v)
		}
	}
	return releases, nil
}

// FetchLoaderGameVersions returns stable game versions supported by the
// given fabric-like loader.
func FetchLoaderGameVersions(loader string) ([]string, error) {
	ep, ok := fabricLike[loader]
	if !ok {
		return nil, fmt.Errorf("unsupported loader %q", loader)
	}
	var raw []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := request.CachedJSON(ep.game, &raw, versionListTTL); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if v.Stable {
			out = append(out, v.Version)
		}
	}
	return out, nil
}

// FetchLoaderVersions returns loader versions of the given fabric-like
// loader available for mcVersion. Pre-release versions are dropped when
// at least one stable version exists (Quilt's list is dominated by
// betas and its meta has no stable flag, so filter by version string).
func FetchLoaderVersions(loader, mcVersion string) ([]FabricLoaderVersion, error) {
	ep, ok := fabricLike[loader]
	if !ok {
		return nil, fmt.Errorf("unsupported loader %q", loader)
	}
	var entries []struct {
		Loader FabricLoaderVersion `json:"loader"`
	}
	if err := request.CachedJSON(fmt.Sprintf(ep.loadersByGame, mcVersion), &entries, versionListTTL); err != nil {
		return nil, err
	}
	all := make([]FabricLoaderVersion, len(entries))
	for i, e := range entries {
		all[i] = e.Loader
	}
	stable := make([]FabricLoaderVersion, 0, len(all))
	for _, v := range all {
		if !isPrerelease(v.Version) {
			stable = append(stable, v)
		}
	}
	if len(stable) == 0 {
		return all, nil
	}
	return stable, nil
}

func isPrerelease(version string) bool {
	for _, marker := range []string{"-beta", "-pre", "-rc", "-alpha"} {
		if strings.Contains(version, marker) {
			return true
		}
	}
	return false
}

func fetchManifest() (*VersionManifest, error) {
	var m VersionManifest
	if err := request.CachedJSON(manifestURL, &m, versionListTTL); err != nil {
		return nil, err
	}
	return &m, nil
}

func fetchVersionDetails(url string) (*VersionDetails, error) {
	var d VersionDetails
	if err := request.JSON(url, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func fetchLoaderProfile(loader, mcVersion, loaderVersion string) (*FabricProfile, error) {
	ep, ok := fabricLike[loader]
	if !ok {
		return nil, fmt.Errorf("unsupported loader %q", loader)
	}
	var p FabricProfile
	if err := request.JSON(fmt.Sprintf(ep.profile, mcVersion, loaderVersion), &p); err != nil {
		return nil, err
	}
	return &p, nil
}
