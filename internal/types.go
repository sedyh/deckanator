package internal

type Profile struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Icon                string `json:"icon"`
	Loader              string `json:"loader"`
	MCVersion           string `json:"mcVersion"`
	FabricLoaderVersion string `json:"fabricLoaderVersion,omitempty"`
	PlayerName          string `json:"playerName,omitempty"`
}

type ProfileStore struct {
	Profiles    []Profile `json:"profiles"`
	LastProfile string    `json:"lastProfile"`
}

type VersionEntry struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type VersionManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []VersionEntry `json:"versions"`
}

type VersionDetails struct {
	ID        string `json:"id"`
	MainClass string `json:"mainClass"`
	Arguments *struct {
		Game []interface{} `json:"game"`
		JVM  []interface{} `json:"jvm"`
	} `json:"arguments,omitempty"`
	MinecraftArguments string              `json:"minecraftArguments,omitempty"`
	AssetIndex         AssetIndexRef       `json:"assetIndex"`
	Assets             string              `json:"assets"`
	Downloads          map[string]Download `json:"downloads"`
	Libraries          []Library           `json:"libraries"`
	JavaVersion        *struct {
		Component    string `json:"component"`
		MajorVersion int    `json:"majorVersion"`
	} `json:"javaVersion,omitempty"`
}

type AssetIndexRef struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type Download struct {
	SHA1 string `json:"sha1"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

type Library struct {
	Name      string `json:"name"`
	Downloads *struct {
		Artifact    *LibraryFile            `json:"artifact,omitempty"`
		Classifiers map[string]*LibraryFile `json:"classifiers,omitempty"`
	} `json:"downloads,omitempty"`
	Rules   []Rule            `json:"rules,omitempty"`
	Natives map[string]string `json:"natives,omitempty"`
	URL     string            `json:"url,omitempty"`
}

type LibraryFile struct {
	Path string `json:"path"`
	SHA1 string `json:"sha1"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

type Rule struct {
	Action string `json:"action"`
	OS     *struct {
		Name string `json:"name"`
	} `json:"os,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

type AssetIndex struct {
	Objects map[string]AssetObject `json:"objects"`
}

type AssetObject struct {
	Hash string `json:"hash"`
	Size int    `json:"size"`
}

type FabricLoaderVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type FabricProfile struct {
	ID        string    `json:"id"`
	MainClass string    `json:"mainClass"`
	Libraries []Library `json:"libraries"`
	Arguments *struct {
		Game []interface{} `json:"game"`
		JVM  []interface{} `json:"jvm"`
	} `json:"arguments,omitempty"`
}

type ProgressFunc func(stage string, current, total int)
