package internal

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// mavenLocalPath converts a Maven coordinate "group:artifact:version[:classifier]"
// to a relative file path like "net/fabricmc/fabric-loader/0.16.14/fabric-loader-0.16.14.jar".
func mavenLocalPath(name string) string {
	parts := strings.SplitN(name, ":", 4)
	if len(parts) < 3 {
		return ""
	}
	group    := strings.ReplaceAll(parts[0], ".", "/")
	artifact := parts[1]
	version  := parts[2]
	file := artifact + "-" + version
	if len(parts) == 4 {
		file += "-" + parts[3]
	}
	file += ".jar"
	return group + "/" + artifact + "/" + version + "/" + file
}

// mavenDownloadURL builds the full download URL from a base repo URL and Maven coordinate.
func mavenDownloadURL(baseURL, name string) string {
	rel := mavenLocalPath(name)
	if rel == "" {
		return ""
	}
	return strings.TrimSuffix(baseURL, "/") + "/" + rel
}

func downloadFile(url, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	resp, err := http.Get(url)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, dest)
}

func extractNatives(jarPath, destDir string) error {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := f.Name
		if !strings.HasSuffix(name, ".so") &&
			!strings.HasSuffix(name, ".dylib") &&
			!strings.HasSuffix(name, ".dll") {
			continue
		}
		if strings.Contains(name, "/") {
			name = filepath.Base(name)
		}
		dest := filepath.Join(destDir, name)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
