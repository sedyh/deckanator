// Package icons holds the curated list of profile icons exposed to the UI.
package icons

import "math/rand"

// Icon is a single selectable profile icon.
type Icon struct {
	ID    string `json:"id"`
	Emoji string `json:"emoji"`
	BG    string `json:"bg"`
}

// All is the full icon set exposed to the UI.
var All = []Icon{
	{ID: "creeper", Emoji: "💀", BG: "#2d4a2d"},
	{ID: "diamond", Emoji: "💎", BG: "#1a5c7a"},
	{ID: "fire", Emoji: "🔥", BG: "#7a2a1a"},
	{ID: "grass", Emoji: "🌿", BG: "#2d5c1a"},
	{ID: "sword", Emoji: "⚔️", BG: "#4a4a5c"},
	{ID: "pickaxe", Emoji: "⛏️", BG: "#5c3d1a"},
	{ID: "tnt", Emoji: "💥", BG: "#7a2a2a"},
	{ID: "enderman", Emoji: "🌑", BG: "#1a1a2d"},
	{ID: "skeleton", Emoji: "🦴", BG: "#4a4a4a"},
	{ID: "pig", Emoji: "🐷", BG: "#7a3d4a"},
	{ID: "wolf", Emoji: "🐺", BG: "#3d3d4a"},
	{ID: "dragon", Emoji: "🐲", BG: "#2d1a5c"},
	{ID: "moon", Emoji: "🌙", BG: "#1a2d5c"},
	{ID: "mountain", Emoji: "🏔️", BG: "#3d3d3d"},
	{ID: "star", Emoji: "⭐", BG: "#5c4a1a"},
	{ID: "chest", Emoji: "📦", BG: "#5c3a1a"},
}

// Random returns the ID of a randomly picked icon.
func Random() string {
	return All[rand.Intn(len(All))].ID
}
