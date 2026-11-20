package client

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	searchCacheTTL       = 24 * time.Hour
	searchCacheVersion   = 1
	searchCacheDirName   = "qbitctl"
	searchCacheFileName  = "search-results.json"
	searchCacheResultMin = int64(100_000_000_000)
	searchCacheResultMax = int64(999_999_999_999)
)

var (
	errSearchCacheUnavailable = errors.New("search result ID is unavailable or expired. Run qbitctl search again")
	errSearchCacheWrite       = errors.New("could not save result IDs to the local search cache")
)

type cachedSearchTarget struct {
	URL    string
	Plugin string
}

type searchCacheFile struct {
	Version int                 `json:"version"`
	URL     string              `json:"url"`
	User    string              `json:"user"`
	SavedAt int64               `json:"saved_at"`
	Results []searchCacheResult `json:"results"`
}

type searchCacheResult struct {
	ID       int64  `json:"id"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Seeders  int    `json:"seeders,omitempty"`
	Leechers int    `json:"leechers,omitempty"`
	Source   string `json:"source,omitempty"`
	URL      string `json:"url"`
	Plugin   string `json:"plugin"`
}

// CacheSearchResults records the latest search results for the current server
// and user. Only rows with a usable download URL receive an ID. The cache is
// deliberately replaced as one snapshot so IDs from an older search cannot
// select a row from a newer search.
func (a *App) CacheSearchResults(results []SearchResult) error {
	path, err := a.searchCachePath()
	if err != nil {
		return errSearchCacheWrite
	}

	previousIDs := readSearchCacheIDs(path)
	usedIDs := make(map[int64]struct{}, len(previousIDs)+len(results))
	for id := range previousIDs {
		usedIDs[id] = struct{}{}
	}

	disk := searchCacheFile{
		Version: searchCacheVersion,
		URL:     searchCacheScopeURL(a.creds.URL),
		User:    a.creds.User,
		SavedAt: time.Now().Unix(),
		Results: make([]searchCacheResult, 0, len(results)),
	}
	ids := make([]int64, len(results))
	for i := range results {
		results[i].ID = 0
		if !isSearchDownloadURL(results[i].downloadURL) {
			continue
		}
		id, err := newSearchCacheID(usedIDs)
		if err != nil {
			return errSearchCacheWrite
		}
		usedIDs[id] = struct{}{}
		ids[i] = id
		disk.Results = append(disk.Results, searchCacheResult{
			ID: id, Name: results[i].Name, Size: results[i].Size,
			Seeders: results[i].Seeders, Leechers: results[i].Leechers,
			Source: results[i].Source, URL: results[i].downloadURL,
			Plugin: results[i].pluginName,
		})
	}

	if err := writeSearchCache(path, disk); err != nil {
		return errSearchCacheWrite
	}
	for i, id := range ids {
		results[i].ID = id
	}
	return nil
}

func (a *App) cachedSearchResult(id int64) (cachedSearchTarget, error) {
	if id <= 0 {
		return cachedSearchTarget{}, errSearchCacheUnavailable
	}
	path, err := a.searchCachePath()
	if err != nil {
		return cachedSearchTarget{}, errSearchCacheUnavailable
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cachedSearchTarget{}, errSearchCacheUnavailable
	}
	var cache searchCacheFile
	if json.Unmarshal(data, &cache) != nil || cache.Version != searchCacheVersion ||
		cache.URL != searchCacheScopeURL(a.creds.URL) || cache.User != a.creds.User ||
		cache.SavedAt <= 0 || time.Since(time.Unix(cache.SavedAt, 0)) > searchCacheTTL {
		return cachedSearchTarget{}, errSearchCacheUnavailable
	}
	for _, result := range cache.Results {
		if result.ID == id && isSearchDownloadURL(result.URL) {
			return cachedSearchTarget{URL: result.URL, Plugin: result.Plugin}, nil
		}
	}
	return cachedSearchTarget{}, errSearchCacheUnavailable
}

func (a *App) searchCachePath() (string, error) {
	base := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME"))
	if base == "" || !filepath.IsAbs(base) {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, searchCacheDirName)
	return filepath.Join(dir, searchCacheFileName), nil
}

func searchCacheScopeURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/") + "/"
}

func newSearchCacheID(used map[int64]struct{}) (int64, error) {
	var buf [8]byte
	for attempt := 0; attempt < 32; attempt++ {
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, err
		}
		raw := binary.BigEndian.Uint64(buf[:])
		span := uint64(searchCacheResultMax - searchCacheResultMin + 1)
		id := searchCacheResultMin + int64(raw%span)
		if _, exists := used[id]; !exists {
			return id, nil
		}
	}
	return 0, errors.New("could not allocate a search result ID")
}

func readSearchCacheIDs(path string) map[int64]struct{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache searchCacheFile
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	ids := make(map[int64]struct{}, len(cache.Results))
	for _, result := range cache.Results {
		if result.ID >= searchCacheResultMin && result.ID <= searchCacheResultMax {
			ids[result.ID] = struct{}{}
		}
	}
	return ids
}

func writeSearchCache(path string, cache searchCacheFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return errors.New("invalid search cache directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".search-results-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
