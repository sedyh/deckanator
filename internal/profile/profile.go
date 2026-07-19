// Package profile owns the launcher's user profile model and its JSON
// persistence layer.
package profile

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"deckanator/internal/config"
	"deckanator/internal/icons"
)

// Profile describes a single Minecraft profile shown in the launcher UI.
type Profile struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Icon                string `json:"icon"`
	Loader              string `json:"loader"`
	MCVersion           string `json:"mcVersion"`
	FabricLoaderVersion string `json:"fabricLoaderVersion,omitempty"`
	PlayerName          string `json:"playerName,omitempty"`
}

// Store is the on-disk shape persisted to profiles.json.
type Store struct {
	Profiles    []Profile `json:"profiles"`
	LastProfile string    `json:"lastProfile"`
}

const base57Alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// NewID returns a short, URL-safe random ID suitable for filesystem paths.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	num := new(big.Int).SetBytes(b[:])
	base := big.NewInt(int64(len(base57Alphabet)))
	mod := new(big.Int)
	out := make([]byte, 0, 11)
	for num.Sign() > 0 {
		num.DivMod(num, base, mod)
		out = append(out, base57Alphabet[mod.Int64()])
	}
	for len(out) < 11 {
		out = append(out, base57Alphabet[0])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func storePath() string {
	return filepath.Join(config.ConfigDir(), "profiles.json")
}

// Load reads all profiles from disk. A missing file produces an empty slice.
func Load() ([]Profile, error) {
	data, err := os.ReadFile(storePath())
	if os.IsNotExist(err) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return s.Profiles, nil
}

// Save writes p, either replacing an existing profile with the same ID
// or appending it.
func Save(p Profile) error {
	profiles, err := Load()
	if err != nil {
		return err
	}
	found := false
	for i, existing := range profiles {
		if existing.ID == p.ID {
			profiles[i] = p
			found = true
			break
		}
	}
	if !found {
		profiles = append(profiles, p)
	}
	return writeAll(profiles)
}

// Delete removes the profile with the given ID and any game data that
// is not used by remaining profiles. Removing the last profile wipes the
// entire game directory.
func Delete(id string) error {
	profiles, err := Load()
	if err != nil {
		return err
	}

	var deleted Profile
	remaining := make([]Profile, 0, len(profiles))
	for _, p := range profiles {
		if p.ID == id {
			deleted = p
			continue
		}
		remaining = append(remaining, p)
	}

	if err := writeAll(remaining); err != nil {
		return err
	}

	if len(remaining) == 0 {
		return CleanGameData()
	}

	_ = os.RemoveAll(filepath.Join(config.GameDir(), "profiles", id))

	if deleted.MCVersion == "" {
		return nil
	}

	mcUsed, loaderUsed := false, false
	for _, p := range remaining {
		if p.MCVersion != deleted.MCVersion {
			continue
		}
		mcUsed = true
		if p.Loader == deleted.Loader && p.FabricLoaderVersion == deleted.FabricLoaderVersion {
			loaderUsed = true
		}
	}

	dir := config.GameDir()
	if deleted.Loader != "" && deleted.Loader != "vanilla" && deleted.FabricLoaderVersion != "" && !loaderUsed {
		id := LoaderID(deleted.Loader, deleted.MCVersion, deleted.FabricLoaderVersion)
		_ = os.RemoveAll(filepath.Join(dir, "versions", id))
	}
	if !mcUsed {
		_ = os.RemoveAll(filepath.Join(dir, "versions", deleted.MCVersion))
	}
	return nil
}

// Create makes a new profile with a generated ID, default name and icon,
// persists it, and returns it.
func Create() Profile {
	profiles, _ := Load()
	p := Profile{
		ID:     NewID(),
		Name:   fmt.Sprintf("Profile %d", len(profiles)+1),
		Icon:   icons.Random(),
		Loader: "vanilla",
	}
	_ = Save(p)
	return p
}

// CleanGameData wipes the entire game directory.
func CleanGameData() error {
	return os.RemoveAll(config.GameDir())
}

// LoaderID returns the canonical Mojang-style version ID used by
// fabric-like loaders ("fabric-loader-<v>-<mc>", "quilt-loader-<v>-<mc>").
func LoaderID(loader, mcVersion, loaderVersion string) string {
	return fmt.Sprintf("%s-loader-%s-%s", loader, loaderVersion, mcVersion)
}

func writeAll(profiles []Profile) error {
	if err := os.MkdirAll(config.ConfigDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(Store{Profiles: profiles}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(storePath(), data, 0o644)
}
