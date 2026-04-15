package internal

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
)

const base57Alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func newID() string {
	var b [8]byte
	rand.Read(b[:])
	num := new(big.Int).SetBytes(b[:])
	base := big.NewInt(57)
	mod := new(big.Int)
	result := make([]byte, 0, 11)
	for num.Sign() > 0 {
		num.DivMod(num, base, mod)
		result = append(result, base57Alphabet[mod.Int64()])
	}
	for len(result) < 11 {
		result = append(result, base57Alphabet[0])
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}

func profilePath() string {
	return filepath.Join(ConfigDir(), "profiles.json")
}

func LoadProfiles() ([]Profile, error) {
	data, err := os.ReadFile(profilePath())
	if os.IsNotExist(err) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var store ProfileStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return store.Profiles, nil
}

func SaveProfile(p Profile) error {
	profiles, err := LoadProfiles()
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
	return writeProfiles(profiles)
}

func DeleteProfile(id string) error {
	profiles, err := LoadProfiles()
	if err != nil {
		return err
	}
	filtered := make([]Profile, 0, len(profiles))
	for _, p := range profiles {
		if p.ID != id {
			filtered = append(filtered, p)
		}
	}
	return writeProfiles(filtered)
}

func CreateProfile() Profile {
	profiles, _ := LoadProfiles()
	name := fmt.Sprintf("Profile %d", len(profiles)+1)
	p := Profile{
		ID:     newID(),
		Name:   name,
		Icon:   RandomIcon(),
		Loader: "vanilla",
	}
	_ = SaveProfile(p)
	return p
}

func CleanGameData() error {
	return os.RemoveAll(GameDir())
}

func writeProfiles(profiles []Profile) error {
	if err := os.MkdirAll(ConfigDir(), 0755); err != nil {
		return err
	}
	store := ProfileStore{Profiles: profiles}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(profilePath(), data, 0644)
}

