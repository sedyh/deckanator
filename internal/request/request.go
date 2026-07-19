// Package request offers small helpers around net/http that apps use
// to pull bytes or decode JSON from an endpoint in one call.
package request

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"deckanator/internal/config"
	"deckanator/internal/errs"
)

// Bytes performs a GET and returns the response body on HTTP 200.
// Non-200 statuses are reported as errors.
func Bytes(url string) (_ []byte, e error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer errs.Close(&e, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// JSON performs a GET and decodes the response body into v on HTTP 200.
func JSON(url string, v any) error {
	data, err := Bytes(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

var (
	cacheMu  sync.Mutex
	memCache = map[string]cacheEntry{}
)

type cacheEntry struct {
	data []byte
	at   time.Time
}

func cachePath(url string) string {
	sum := sha1.Sum([]byte(url))
	return filepath.Join(config.ConfigDir(), "cache", hex.EncodeToString(sum[:])+".json")
}

// CachedJSON is JSON with a two-level cache: an in-memory map for the
// session and a disk cache under the config dir across sessions.
// Entries younger than ttl are served without touching the network; on
// fetch failure a stale entry of any age is used as a fallback.
func CachedJSON(url string, v any, ttl time.Duration) error {
	cacheMu.Lock()
	entry, inMem := memCache[url]
	cacheMu.Unlock()

	if inMem && time.Since(entry.at) < ttl {
		return json.Unmarshal(entry.data, v)
	}

	path := cachePath(url)
	var diskData []byte
	var diskAt time.Time
	if fi, err := os.Stat(path); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			diskData, diskAt = data, fi.ModTime()
		}
	}
	if diskData != nil && time.Since(diskAt) < ttl {
		cacheMu.Lock()
		memCache[url] = cacheEntry{diskData, diskAt}
		cacheMu.Unlock()
		return json.Unmarshal(diskData, v)
	}

	data, err := Bytes(url)
	if err == nil {
		if json.Unmarshal(data, v) == nil {
			cacheMu.Lock()
			memCache[url] = cacheEntry{data, time.Now()}
			cacheMu.Unlock()
			if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr == nil {
				_ = os.WriteFile(path, data, 0o644)
			}
			return nil
		}
		return fmt.Errorf("invalid JSON from %s", url)
	}

	// Network failed: fall back to any stale copy.
	if inMem {
		return json.Unmarshal(entry.data, v)
	}
	if diskData != nil {
		return json.Unmarshal(diskData, v)
	}
	return err
}
