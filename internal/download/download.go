// Package download provides atomic file download and jar native extraction.
package download

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"deckanator/internal/errs"
)

// File downloads url into dest atomically: data is written to dest+".tmp"
// and renamed on success. If dest already exists, the download is skipped.
// On any failure the temporary file is cleaned up.
func File(url, dest string) (e error) {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer errs.Close(&e, f)
	defer errs.DoSilentOnError(&e, errs.Remove(tmp))

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer errs.Close(&e, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// ExtractNatives copies the platform-native libraries out of jarPath into
// destDir, flattening any directory structure inside the archive.
func ExtractNatives(jarPath, destDir string) (e error) {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return err
	}
	defer errs.Close(&e, r)

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := f.Name
		if !isNativeLib(name) {
			continue
		}
		if strings.Contains(name, "/") {
			name = filepath.Base(name)
		}
		if err := copyZipEntry(f, filepath.Join(destDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func isNativeLib(name string) bool {
	return strings.HasSuffix(name, ".so") ||
		strings.HasSuffix(name, ".dylib") ||
		strings.HasSuffix(name, ".dll")
}

func copyZipEntry(f *zip.File, dest string) (e error) {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer errs.Close(&e, rc)

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer errs.Close(&e, out)

	_, err = io.Copy(out, rc)
	return err
}
