package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	manifestURL         = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
	fabricLoadersByGame = "https://meta.fabricmc.net/v2/versions/loader/%s"
	fabricGameURL       = "https://meta.fabricmc.net/v2/versions/game"
	fabricProfURL       = "https://meta.fabricmc.net/v2/versions/loader/%s/%s/profile/json"
)

func FetchVanillaVersions() ([]VersionEntry, error) {
	manifest, err := fetchManifest()
	if err != nil {
		return nil, err
	}
	var releases []VersionEntry
	for _, v := range manifest.Versions {
		if v.Type == "release" {
			releases = append(releases, v)
		}
	}
	return releases, nil
}

type fabricLoaderEntry struct {
	Loader FabricLoaderVersion `json:"loader"`
}

func FetchFabricGameVersions() ([]string, error) {
	data, err := httpGet(fabricGameURL)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	var out []string
	for _, v := range raw {
		if v.Stable {
			out = append(out, v.Version)
		}
	}
	return out, nil
}

func FetchFabricLoaderVersions(mcVersion string) ([]FabricLoaderVersion, error) {
	url := fmt.Sprintf(fabricLoadersByGame, mcVersion)
	data, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	var entries []fabricLoaderEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	versions := make([]FabricLoaderVersion, len(entries))
	for i, e := range entries {
		versions[i] = e.Loader
	}
	return versions, nil
}

func fetchManifest() (*VersionManifest, error) {
	data, err := httpGet(manifestURL)
	if err != nil {
		return nil, err
	}
	var manifest VersionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func fetchVersionDetails(url string) (*VersionDetails, error) {
	data, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	var details VersionDetails
	if err := json.Unmarshal(data, &details); err != nil {
		return nil, err
	}
	return &details, nil
}

func fetchFabricProfile(mcVersion, loaderVersion string) (*FabricProfile, error) {
	url := fmt.Sprintf(fabricProfURL, mcVersion, loaderVersion)
	data, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	var profile FabricProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

func httpGet(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
