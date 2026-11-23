package client

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
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
	errSearchCacheUnreadable  = errors.New("could not read the local search cache")
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
	Query   string              `json:"query,omitempty"`
	Results []searchCacheResult `json:"results"`
}

type searchCacheResult struct {
	ID       int64  `json:"id"`
	URL      string `json:"url"`
	Plugin   string `json:"plugin"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Seeders  int    `json:"seeders,omitempty"`
	Leechers int    `json:"leechers,omitempty"`
	Source   string `json:"source,omitempty"`
}

// CachedSearch is one unexpired local search snapshot. It contains only the
// display data needed to reprint results and the IDs used by add --search-id.
// Download URLs are deliberately kept out of the public history response even
// though an in-memory SearchResult can expose one to an interactive client.
type CachedSearch struct {
	SavedAt time.Time
	Query   string
	Results []CachedSearchResult
}

// CachedSearchResult is a result retained in a local search snapshot.
type CachedSearchResult struct {
	ID       int64
	Name     string
	Size     int64
	Seeders  int
	Leechers int
	Source   string
}

// CacheSearchResults records a bounded history for the current server and user.
// Only rows with a usable download URL receive an ID.
func (a *App) CacheSearchResults(results []SearchResult) error {
	return a.CacheSearchResultsForQuery("", results)
}

// CacheSearchResultsForQuery records a bounded history for the current server
// and user, retaining the query and display fields for offline reprinting.
func (a *App) CacheSearchResultsForQuery(query string, results []SearchResult) error {
	path, err := SearchResultsCachePath()
	if err != nil {
		return fmt.Errorf("%w: %w", errSearchCacheWrite, err)
	}
	if err := prepareSearchCache(path); err != nil {
		return fmt.Errorf("%w: %w", errSearchCacheWrite, err)
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
		latest := searchSnapshot{searchScope: a.searchScope(), SavedAt: now.Unix(), Query: strings.TrimSpace(query)}
		for i, result := range results {
			downloadURL := result.DownloadURL()
			if !isSearchDownloadURL(downloadURL) {
				continue
			}
			id, err := newSearchCacheID(usedIDs)
			if err != nil {
				return false, err
			}
			usedIDs[id] = struct{}{}
			ids[i] = id
			latest.Results = append(latest.Results, searchCacheResult{
				ID: id, URL: downloadURL, Plugin: result.pluginName,
				Name: result.Name, Size: result.Size, Seeders: result.Seeders,
				Leechers: result.Leechers, Source: result.Source,
			})
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
		return fmt.Errorf("%w: %w", errSearchCacheWrite, err)
	}
	for i, id := range ids {
		results[i].ID = id
	}
	return nil
}

// CachedSearchHistory reads unexpired snapshots for the app's server and user
// without contacting qBittorrent. Results are returned newest first.
func (a *App) CachedSearchHistory() ([]CachedSearch, error) {
	path, err := SearchResultsCachePath()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSearchCacheUnreadable, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errSearchCacheUnavailable
		}
		return nil, fmt.Errorf("%w: %w", errSearchCacheUnreadable, err)
	}
	searches, err := parseSearches(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSearchCacheUnreadable, err)
	}
	scope := a.searchScope()
	history := make([]CachedSearch, 0)
	for _, snapshot := range retainSearches(searches, time.Now()) {
		if snapshot.searchScope != scope {
			continue
		}
		results := make([]CachedSearchResult, 0, len(snapshot.Results))
		for _, result := range snapshot.Results {
			results = append(results, CachedSearchResult{
				ID: result.ID, Name: result.Name, Size: result.Size,
				Seeders: result.Seeders, Leechers: result.Leechers, Source: result.Source,
			})
		}
		history = append(history, CachedSearch{SavedAt: time.Unix(snapshot.SavedAt, 0), Query: snapshot.Query, Results: results})
	}
	if len(history) == 0 {
		return nil, errSearchCacheUnavailable
	}
	return history, nil
}

// ReadCachedSearchHistory loads local search history from the selected config
// and command-line server metadata without authenticating to qBittorrent.
func ReadCachedSearchHistory(opts Options) ([]CachedSearch, error) {
	setup, err := ReadSetup(opts)
	if err != nil {
		// A cache lookup can still be scoped when the caller supplied both
		// server identity flags explicitly, even if no config file exists yet.
		if strings.TrimSpace(opts.URL) == "" || strings.TrimSpace(opts.User) == "" {
			return nil, err
		}
		setup = Setup{URL: opts.URL, User: opts.User}
	}
	if strings.TrimSpace(setup.URL) == "" && strings.TrimSpace(opts.URL) != "" {
		setup.URL = opts.URL
	}
	if strings.TrimSpace(setup.User) == "" && strings.TrimSpace(opts.User) != "" {
		setup.User = opts.User
	}
	if strings.TrimSpace(setup.URL) == "" || strings.TrimSpace(setup.User) == "" {
		return nil, errors.New("server address and username are required to identify search history")
	}
	app := &App{creds: Credentials{URL: setup.URL, User: setup.User}}
	return app.CachedSearchHistory()
}

func (a *App) cachedSearchResult(id int64) (cachedSearchTarget, error) {
	if id <= 0 {
		return cachedSearchTarget{}, errSearchCacheUnavailable
	}
	path, err := SearchResultsCachePath()
	if err != nil {
		return cachedSearchTarget{}, fmt.Errorf("%w: %w", errSearchCacheUnreadable, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return cachedSearchTarget{}, fmt.Errorf("%w: %w", errSearchCacheUnreadable, err)
		}
		return cachedSearchTarget{}, errSearchCacheUnavailable
	}
	searches, err := parseSearches(data)
	if err != nil {
		return cachedSearchTarget{}, fmt.Errorf("%w: %w", errSearchCacheUnreadable, err)
	}
	scope := a.searchScope()
	for _, snapshot := range retainSearches(searches, time.Now()) {
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
	searches, _ := parseSearches(data)
	return searches
}

func parseSearches(data []byte) ([]searchSnapshot, error) {
	var cache searchCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	switch cache.Version {
	case 1:
		return []searchSnapshot{cache.searchSnapshot}, nil
	case searchCacheVersion:
		return cache.Searches, nil
	default:
		return nil, nil
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

// SearchResultsCachePath returns the local path shared by CLI and TUI search.
func SearchResultsCachePath() (string, error) {
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
	for current := dir; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("search cache directory %q is not a directory", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		if err != nil {
			return err
		}
		return fmt.Errorf("search cache directory %q is not a directory", dir)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("search cache file %q is not a regular file", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
