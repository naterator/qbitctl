package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
		if info.Mode().Perm() != want {
			t.Errorf("%s permissions = %v, want %v", cachePath, info.Mode().Perm(), want)
		}
	}
}

func TestCacheSearchResultsReplacesSnapshotAndRejectsOldScope(t *testing.T) {
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
	if _, err := app.cachedSearchResult(oldID); !errors.Is(err, errSearchCacheUnavailable) {
		t.Fatalf("old ID error = %v", err)
	}
	for _, creds := range []Credentials{
		{URL: "https://other.example/qbt", User: "admin"},
		{URL: app.creds.URL, User: "another-user"},
	} {
		other := &App{creds: creds}
		if _, err := other.cachedSearchResult(second[0].ID); !errors.Is(err, errSearchCacheUnavailable) {
			t.Errorf("wrong-scope error = %v", err)
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
		Version: searchCacheVersion, URL: searchCacheScopeURL(app.creds.URL), User: app.creds.User,
		SavedAt: time.Now().Add(-searchCacheTTL - time.Minute).Unix(),
		Results: []searchCacheResult{{ID: 123456789012, URL: "https://download.example/old.torrent", Plugin: "jackett"}},
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(cache)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.cachedSearchResult(123456789012); err == nil || !errors.Is(err, errSearchCacheUnavailable) {
		t.Fatalf("expired cache error = %v", err)
	}
}
