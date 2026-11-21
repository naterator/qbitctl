package client

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/naterator/qbitctl/internal/configfile"
)

const (
	searchCacheTTL       = 24 * time.Hour
	searchCacheVersion   = 2
	searchCachePerScope  = 8
	searchCacheTotal     = 64
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
	Version int `json:"version"`
	searchSnapshot
	Searches []searchSnapshot `json:"searches"`
}

type searchScope struct {
	URL  string `json:"url"`
	User string `json:"user"`
}

type searchSnapshot struct {
	searchScope
	SavedAt int64               `json:"saved_at"`
	Results []searchCacheResult `json:"results"`
}

type searchCacheResult struct {
	ID     int64  `json:"id"`
	URL    string `json:"url"`
	Plugin string `json:"plugin"`
}

// CacheSearchResults records a bounded history for the current server and user.
// Only rows with a usable download URL receive an ID.
func (a *App) CacheSearchResults(results []SearchResult) error {
	path, err := a.searchCachePath()
	if err != nil {
		return errSearchCacheWrite
	}
	if err := prepareSearchCache(path); err != nil {
		return errSearchCacheWrite
	}
	ids := make([]int64, len(results))
	for i := range results {
		results[i].ID = 0
	}
	err = configfile.UpdateCache(path, func(object configfile.Object) (bool, error) {
		data, err := json.Marshal(object)
		if err != nil {
			return false, err
		}
		now := time.Now()
		previous := retainSearches(decodeSearches(data), now)
		usedIDs := make(map[int64]struct{})
		for _, snapshot := range previous {
			for _, result := range snapshot.Results {
				usedIDs[result.ID] = struct{}{}
			}
		}
		latest := searchSnapshot{searchScope: a.searchScope(), SavedAt: now.Unix()}
		for i, result := range results {
			if !isSearchDownloadURL(result.downloadURL) {
				continue
			}
			id, err := newSearchCacheID(usedIDs)
			if err != nil {
				return false, err
			}
			usedIDs[id] = struct{}{}
			ids[i] = id
			latest.Results = append(latest.Results, searchCacheResult{ID: id, URL: result.downloadURL, Plugin: result.pluginName})
		}
		if len(latest.Results) == 0 {
			return false, nil
		}
		searches := retainSearches(append([]searchSnapshot{latest}, previous...), now)
		data, err = json.Marshal(searches)
		if err != nil {
			return false, err
		}
		clear(object)
		object["version"] = json.RawMessage(strconv.Itoa(searchCacheVersion))
		object["searches"] = data
		return true, nil
	})
	if err != nil {
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
	scope := a.searchScope()
	for _, snapshot := range retainSearches(decodeSearches(data), time.Now()) {
		if snapshot.searchScope != scope {
			continue
		}
		for _, result := range snapshot.Results {
			if result.ID == id && isSearchDownloadURL(result.URL) {
				return cachedSearchTarget{URL: result.URL, Plugin: result.Plugin}, nil
			}
		}
	}
	return cachedSearchTarget{}, errSearchCacheUnavailable
}

func (a *App) searchScope() searchScope {
	return searchScope{URL: searchCacheScopeURL(a.creds.URL), User: a.creds.User}
}

func decodeSearches(data []byte) []searchSnapshot {
	var cache searchCacheFile
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	switch cache.Version {
	case 1:
		return []searchSnapshot{cache.searchSnapshot}
	case searchCacheVersion:
		return cache.Searches
	default:
		return nil
	}
}

// Search snapshots are stored newest first. Retain each profile independently
// up to the per-profile cap, with a global cap for abandoned profiles.
func retainSearches(searches []searchSnapshot, now time.Time) []searchSnapshot {
	counts := make(map[searchScope]int)
	retained := make([]searchSnapshot, 0, min(len(searches), searchCacheTotal))
	for _, snapshot := range searches {
		age := now.Sub(time.Unix(snapshot.SavedAt, 0))
		if snapshot.SavedAt <= 0 || age < 0 || age >= searchCacheTTL || counts[snapshot.searchScope] >= searchCachePerScope {
			continue
		}
		counts[snapshot.searchScope]++
		retained = append(retained, snapshot)
		if len(retained) == searchCacheTotal {
			break
		}
	}
	return retained
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

func prepareSearchCache(path string) error {
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
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("invalid search cache file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
