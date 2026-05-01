// Package minecraft implements the parts of the vanilla and Fabric
// launcher protocols that the UI needs: version metadata, installation,
// and launch.
package minecraft

// VersionEntry is the short-form version record from Mojang's manifest.
type VersionEntry struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

// VersionManifest is the root document at version_manifest_v2.json.
type VersionManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []VersionEntry `json:"versions"`
}

// VersionDetails is the per-version JSON downloaded from
// VersionEntry.URL.
type VersionDetails struct {
	ID                 string              `json:"id"`
	MainClass          string              `json:"mainClass"`
	Arguments          *Arguments          `json:"arguments,omitempty"`
	MinecraftArguments string              `json:"minecraftArguments,omitempty"`
	AssetIndex         AssetIndexRef       `json:"assetIndex"`
	Assets             string              `json:"assets"`
	Downloads          map[string]Download `json:"downloads"`
	Libraries          []Library           `json:"libraries"`
	JavaVersion        *JavaVersion        `json:"javaVersion,omitempty"`
}

// Arguments is the "arguments" block of a modern version manifest.
type Arguments struct {
	Game []any `json:"game"`
	JVM  []any `json:"jvm"`
}

// JavaVersion is the javaVersion block of a version manifest.
type JavaVersion struct {
	Component    string `json:"component"`
	MajorVersion int    `json:"majorVersion"`
}

// AssetIndexRef points at the assets index JSON for a version.
type AssetIndexRef struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// Download is a single download entry in a Mojang manifest.
type Download struct {
	SHA1 string `json:"sha1"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

// Library describes a classpath or native library dependency.
type Library struct {
	Name      string            `json:"name"`
	Downloads *LibraryDownloads `json:"downloads,omitempty"`
	Rules     []Rule            `json:"rules,omitempty"`
	Natives   map[string]string `json:"natives,omitempty"`
	URL       string            `json:"url,omitempty"`
}

// LibraryDownloads groups classpath and native artefacts.
type LibraryDownloads struct {
	Artifact    *LibraryFile            `json:"artifact,omitempty"`
	Classifiers map[string]*LibraryFile `json:"classifiers,omitempty"`
}

// LibraryFile is a single downloadable artefact.
type LibraryFile struct {
	Path string `json:"path"`
	SHA1 string `json:"sha1"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

// Rule is one entry of the rules array governing library inclusion and
// argument applicability.
type Rule struct {
	Action   string          `json:"action"`
	OS       *RuleOS         `json:"os,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

// RuleOS narrows a Rule to a specific operating system.
type RuleOS struct {
	Name string `json:"name"`
}

// AssetIndex is the asset index JSON, mapping logical names to hashes.
type AssetIndex struct {
	Objects map[string]AssetObject `json:"objects"`
}

// AssetObject identifies a single asset blob by hash and size.
type AssetObject struct {
	Hash string `json:"hash"`
	Size int    `json:"size"`
}

// FabricLoaderVersion is a single Fabric loader version.
type FabricLoaderVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// FabricProfile is a version-manifest-shaped document returned by the
// Fabric meta API for a concrete (mcVersion, loaderVersion) pair.
type FabricProfile struct {
	ID        string     `json:"id"`
	MainClass string     `json:"mainClass"`
	Libraries []Library  `json:"libraries"`
	Arguments *Arguments `json:"arguments,omitempty"`
}

// ProgressFunc is how long-running operations report progress.
type ProgressFunc func(stage string, current, total int)
