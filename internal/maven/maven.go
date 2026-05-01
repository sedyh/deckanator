// Package maven contains helpers for dealing with Maven coordinates and
// the layout they imply on disk and in remote repositories.
package maven

import "strings"

// LocalPath converts a Maven coordinate "group:artifact:version[:classifier]"
// to a repository-relative file path like
// "net/fabricmc/fabric-loader/0.16.14/fabric-loader-0.16.14.jar".
// It returns "" if the coordinate cannot be parsed.
func LocalPath(coord string) string {
	parts := strings.SplitN(coord, ":", 4)
	if len(parts) < 3 {
		return ""
	}
	group := strings.ReplaceAll(parts[0], ".", "/")
	artifact := parts[1]
	version := parts[2]
	file := artifact + "-" + version
	if len(parts) == 4 {
		file += "-" + parts[3]
	}
	file += ".jar"
	return group + "/" + artifact + "/" + version + "/" + file
}

// DownloadURL builds the full download URL for a Maven coordinate inside
// a given repository base URL.
func DownloadURL(baseURL, coord string) string {
	rel := LocalPath(coord)
	if rel == "" {
		return ""
	}
	return strings.TrimSuffix(baseURL, "/") + "/" + rel
}
