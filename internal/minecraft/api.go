package minecraft

import (
	"fmt"

	"deckanator/internal/request"
)

const (
	manifestURL         = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
	fabricLoadersByGame = "https://meta.fabricmc.net/v2/versions/loader/%s"
	fabricGameURL       = "https://meta.fabricmc.net/v2/versions/game"
	fabricProfileURL    = "https://meta.fabricmc.net/v2/versions/loader/%s/%s/profile/json"
)

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

// FetchFabricGameVersions returns stable game versions supported by
// Fabric.
func FetchFabricGameVersions() ([]string, error) {
	var raw []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := request.JSON(fabricGameURL, &raw); err != nil {
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

// FetchFabricLoaderVersions returns loader versions available for
// mcVersion.
func FetchFabricLoaderVersions(mcVersion string) ([]FabricLoaderVersion, error) {
	var entries []struct {
		Loader FabricLoaderVersion `json:"loader"`
	}
	if err := request.JSON(fmt.Sprintf(fabricLoadersByGame, mcVersion), &entries); err != nil {
		return nil, err
	}
	versions := make([]FabricLoaderVersion, len(entries))
	for i, e := range entries {
		versions[i] = e.Loader
	}
	return versions, nil
}

func fetchManifest() (*VersionManifest, error) {
	var m VersionManifest
	if err := request.JSON(manifestURL, &m); err != nil {
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

func fetchFabricProfile(mcVersion, loaderVersion string) (*FabricProfile, error) {
	var p FabricProfile
	if err := request.JSON(fmt.Sprintf(fabricProfileURL, mcVersion, loaderVersion), &p); err != nil {
		return nil, err
	}
	return &p, nil
}
