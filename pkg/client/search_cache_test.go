package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func newSearchCacheTestApp(t *testing.T, server, user string) *App {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return &App{creds: Credentials{URL: server, User: user}}
}

func TestCacheSearchResultsRoundTripAndPermissions(t *testing.T) {
	app := newSearchCacheTestApp(t, "https://torrent.example/qbt", "admin")
	results := []SearchResult{
		{Name: "Ubuntu 24.04", Size: 100, Seeders: 4, downloadURL: "magnet:?xt=urn:btih:abc", pluginName: "jackett"},
		{Name: "Jackett diagnostic", downloadURL: "", pluginName: "jackett"},
	}
	if err := app.CacheSearchResults(results); err != nil {
		t.Fatal(err)
	}
	if results[0].ID < searchCacheResultMin || results[0].ID > searchCacheResultMax || results[1].ID != 0 {
		t.Fatalf("cached IDs = %d, %d", results[0].ID, results[1].ID)
	}
	target, err := app.cachedSearchResult(results[0].ID)
	if err != nil || target.URL != "magnet:?xt=urn:btih:abc" || target.Plugin != "jackett" {
		t.Fatalf("cached target = %#v, %v", target, err)
	}
	path, err := app.searchCachePath()
	if err != nil {
		t.Fatal(err)
	}
	for cachePath, want := range map[string]os.FileMode{filepath.Dir(path): 0o700, path: 0o600} {
		info, err := os.Stat(cachePath)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != want {
			t.Errorf("%s permissions = %v, want %v", cachePath, info.Mode().Perm(), want)
		}
	}
}

func TestCacheSearchResultsRetainsSnapshotsAndRejectsOtherScopes(t *testing.T) {
	app := newSearchCacheTestApp(t, "https://torrent.example/qbt", "admin")
	first := []SearchResult{{Name: "first", downloadURL: "https://download.example/1.torrent", pluginName: "jackett"}}
	if err := app.CacheSearchResults(first); err != nil {
		t.Fatal(err)
	}
	oldID := first[0].ID
	second := []SearchResult{{Name: "second", downloadURL: "https://download.example/2.torrent", pluginName: "jackett"}}
	if err := app.CacheSearchResults(second); err != nil {
		t.Fatal(err)
	}
	if second[0].ID == oldID {
		t.Fatal("new search reused a stale result ID")
	}
	if target, err := app.cachedSearchResult(oldID); err != nil || target.URL != first[0].downloadURL {
		t.Fatalf("first search target = %v, %v", target, err)
	}
	for _, creds := range []Credentials{
		{URL: "https://other.example/qbt", User: "admin"},
		{URL: app.creds.URL, User: "another-user"},
	} {
		other := &App{creds: creds}
		if _, err := other.cachedSearchResult(second[0].ID); !errors.Is(err, errSearchCacheUnavailable) {
			t.Errorf("wrong-scope error = %v", err)
		}
		otherResults := []SearchResult{{downloadURL: "https://download.example/other.torrent"}}
		if err := other.CacheSearchResults(otherResults); err != nil {
			t.Fatal(err)
		}
		if _, err := app.cachedSearchResult(otherResults[0].ID); !errors.Is(err, errSearchCacheUnavailable) {
			t.Errorf("ID from another profile error = %v", err)
		}
		if _, err := app.cachedSearchResult(second[0].ID); err != nil {
			t.Fatalf("search in another profile invalidated result ID: %v", err)
		}
	}
}

func TestCachedSearchResultRejectsExpiredCache(t *testing.T) {
	app := newSearchCacheTestApp(t, "https://torrent.example/qbt", "admin")
	path, err := app.searchCachePath()
	if err != nil {
		t.Fatal(err)
	}
	cache := searchCacheFile{
		Version: searchCacheVersion,
		Searches: []searchSnapshot{
			{searchScope: app.searchScope(), SavedAt: time.Now().Add(time.Minute).Unix(), Results: []searchCacheResult{{ID: 123456789011, URL: "https://download.example/future.torrent"}}},
			{searchScope: app.searchScope(), SavedAt: time.Now().Add(-searchCacheTTL - time.Minute).Unix(), Results: []searchCacheResult{{ID: 123456789012, URL: "https://download.example/old.torrent"}}},
		},
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(cache)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{123456789011, 123456789012} {
		if _, err := app.cachedSearchResult(id); !errors.Is(err, errSearchCacheUnavailable) {
			t.Fatalf("invalid timestamp for %d returned error %v", id, err)
		}
	}
}

func TestSearchCachePreservesRecentLegacyIDs(t *testing.T) {
	app := newSearchCacheTestApp(t, "https://torrent.example/qbt", "admin")
	path, err := app.searchCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := fmt.Sprintf(`{"version":1,"url":"https://torrent.example/qbt/","user":"admin","saved_at":%d,"results":[{"id":123456789012,"url":"https://download.example/legacy.torrent","plugin":"jackett"}]}`, time.Now().Unix())
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < searchCachePerScope; i++ {
		if _, err := app.cachedSearchResult(123456789012); err != nil {
			t.Fatalf("legacy ID unexpectedly evicted after %d searches: %v", i, err)
		}
		if err := app.CacheSearchResults([]SearchResult{{downloadURL: "https://download.example/current.torrent"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.cachedSearchResult(123456789012); !errors.Is(err, errSearchCacheUnavailable) {
		t.Fatalf("oldest search was not evicted at the per-profile cap: %v", err)
	}
}

func TestSearchCacheRecoversFromInvalidFile(t *testing.T) {
	app := newSearchCacheTestApp(t, "https://torrent.example/qbt", "admin")
	path, err := app.searchCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	results := []SearchResult{{downloadURL: "https://download.example/fresh.torrent"}}
	if err := app.CacheSearchResults(results); err != nil {
		t.Fatal(err)
	}
	if target, err := app.cachedSearchResult(results[0].ID); err != nil || target.URL != results[0].downloadURL {
		t.Fatalf("cached target = %v, %v", target, err)
	}
}

func TestSearchCacheSerializesConcurrentWriters(t *testing.T) {
	app := newSearchCacheTestApp(t, "https://torrent.example/qbt", "admin")
	const writers = 6
	results := make([]SearchResult, writers)
	var group sync.WaitGroup
	for i := range results {
		results[i].downloadURL = fmt.Sprintf("https://download.example/%d.torrent", i)
		group.Go(func() {
			if err := app.CacheSearchResults(results[i : i+1]); err != nil {
				t.Errorf("cache search %d: %v", i, err)
			}
		})
	}
	group.Wait()
	for _, result := range results {
		target, err := app.cachedSearchResult(result.ID)
		if err != nil || target.URL != result.downloadURL {
			t.Errorf("lost result %d, target = %v, %v", result.ID, target, err)
		}
	}
}
