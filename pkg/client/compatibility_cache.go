package client

// The compatibility cache deliberately contains only the two public version
// strings and their timestamp. Authentication cookies and credentials never
// enter this file; they remain in the authenticated App/HTTP client only.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/naterator/qbitctl/internal/configfile"
)

const (
	compatibilityCacheVersion    = 1
	compatibilityCacheTTL        = 24 * time.Hour
	compatibilityCacheMaxEntries = 32
	compatibilityCacheMaxBytes   = 64 * 1024
	compatibilityCacheFileName   = ".compatibility-cache.json"
)

type compatibilityCacheEntry struct {
	URL        string `json:"url"`
	User       string `json:"user"`
	Profile    string `json:"profile,omitempty"`
	AppVersion string `json:"app_version"`
	APIVersion string `json:"api_version"`
	CheckedAt  int64  `json:"checked_at"`
	// Origin identifies the config file that owns this cache. The cache file
	// already lives next to the origin, but retaining it makes origin changes
	// explicit and prevents an accidentally copied cache from being reused.
	Origin string `json:"origin,omitempty"`
}

type compatibilityCacheData struct {
	Version int                       `json:"version"`
	Entries []compatibilityCacheEntry `json:"entries"`
}

type compatibilitySource uint8

const (
	compatibilitySourceNone compatibilitySource = iota
	compatibilitySourceSession
	compatibilitySourceDisk
)

func (a *App) compatibilityCachePath() string {
	if a == nil || !a.compatibilityCacheEnabled {
		return ""
	}
	searchPath, err := SearchResultsCachePath()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(searchPath), compatibilityCacheFileName)
}

func (a *App) compatibilityOrigin() string {
	if a == nil || strings.TrimSpace(a.configPath) == "" {
		return ""
	}
	path, err := filepath.Abs(a.configPath)
	if err != nil {
		return filepath.Clean(a.configPath)
	}
	return filepath.Clean(path)
}

func (a *App) compatibilityScope() compatibilityCacheEntry {
	return compatibilityCacheEntry{
		URL:     strings.TrimRight(strings.TrimSpace(a.creds.URL), "/"),
		User:    a.creds.User,
		Profile: a.profileName,
		Origin:  a.compatibilityOrigin(),
	}
}

func validCompatibilityVersions(appVersion, apiVersion string) bool {
	return strings.TrimSpace(appVersion) != "" && strings.TrimSpace(apiVersion) != ""
}

func (a *App) loadCompatibilityCache() (appVersion, apiVersion string, ok bool) {
	path := a.compatibilityCachePath()
	if path == "" {
		return "", "", false
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", false
	}
	if !info.Mode().IsRegular() || info.Size() > compatibilityCacheMaxBytes {
		return "", "", false
	}
	object, err := configfile.Read(path)
	if err != nil {
		return "", "", false
	}
	data, err := json.Marshal(object)
	if err != nil {
		return "", "", false
	}
	var cache compatibilityCacheData
	if err := json.Unmarshal(data, &cache); err != nil || cache.Version != compatibilityCacheVersion {
		return "", "", false
	}
	scope := a.compatibilityScope()
	now := time.Now()
	for _, entry := range cache.Entries {
		if entry.URL != scope.URL || entry.User != scope.User || entry.Profile != scope.Profile || entry.Origin != scope.Origin {
			continue
		}
		age := now.Sub(time.Unix(entry.CheckedAt, 0))
		if entry.CheckedAt <= 0 || age < 0 || age >= compatibilityCacheTTL {
			continue
		}
		if !validCompatibilityVersions(entry.AppVersion, entry.APIVersion) {
			continue
		}
		return strings.TrimSpace(entry.AppVersion), strings.TrimSpace(entry.APIVersion), true
	}
	return "", "", false
}

func (a *App) saveCompatibilityCache(appVersion, apiVersion string) {
	path := a.compatibilityCachePath()
	if path == "" || !validCompatibilityVersions(appVersion, apiVersion) {
		return
	}
	scope := a.compatibilityScope()
	entry := compatibilityCacheEntry{
		URL:        scope.URL,
		User:       scope.User,
		Profile:    scope.Profile,
		Origin:     scope.Origin,
		AppVersion: strings.TrimSpace(appVersion),
		APIVersion: strings.TrimSpace(apiVersion),
		CheckedAt:  time.Now().Unix(),
	}
	_ = configfile.UpdateCache(path, func(object configfile.Object) (bool, error) {
		cache := compatibilityCacheData{Version: compatibilityCacheVersion}
		if raw, ok := object["entries"]; ok {
			_ = json.Unmarshal(raw, &cache.Entries)
		}
		retained := make([]compatibilityCacheEntry, 0, compatibilityCacheMaxEntries)
		for _, existing := range cache.Entries {
			if existing.URL == entry.URL && existing.User == entry.User && existing.Profile == entry.Profile && existing.Origin == entry.Origin {
				continue
			}
			if len(retained) >= compatibilityCacheMaxEntries-1 {
				break
			}
			retained = append(retained, existing)
		}
		cache.Entries = append([]compatibilityCacheEntry{entry}, retained...)
		encoded, err := json.Marshal(cache.Entries)
		if err != nil {
			return false, err
		}
		clear(object)
		object["version"] = json.RawMessage("1")
		object["entries"] = encoded
		return true, nil
	})
}

// invalidateCompatibilityCache removes the current server/user scope. It is
// intentionally best-effort: a disposable optimization must never turn a
// valid authenticated operation into a failure.
func (a *App) invalidateCompatibilityCache() {
	path := a.compatibilityCachePath()
	if path == "" {
		return
	}
	scope := a.compatibilityScope()
	_ = configfile.UpdateCache(path, func(object configfile.Object) (bool, error) {
		var entries []compatibilityCacheEntry
		if raw, ok := object["entries"]; ok {
			_ = json.Unmarshal(raw, &entries)
		}
		retained := entries[:0]
		for _, entry := range entries {
			if entry.URL == scope.URL && entry.User == scope.User && entry.Profile == scope.Profile && entry.Origin == scope.Origin {
				continue
			}
			retained = append(retained, entry)
		}
		if len(retained) == len(entries) {
			return false, nil
		}
		encoded, err := json.Marshal(retained)
		if err != nil {
			return false, err
		}
		clear(object)
		object["version"] = json.RawMessage("1")
		object["entries"] = encoded
		return true, nil
	})
}

// refreshCompatibility forces a fresh version probe and refreshes the
// reusable compatibility metadata. It is useful after a cached feature gate
// reports that a newer qBittorrent capability may be available.
func (a *App) refreshCompatibility() error {
	appVersion, err := a.fetchAppVersionFresh()
	if err != nil {
		a.invalidateCompatibilityCache()
		return err
	}
	apiVersion, err := a.fetchAPIVersionFresh()
	if err != nil {
		a.invalidateCompatibilityCache()
		return err
	}
	if err := validateCompatibilityVersions(appVersion, apiVersion); err != nil {
		a.invalidateCompatibilityCache()
		return err
	}
	a.appVersion = appVersion
	a.apiVersion = apiVersion
	a.compatibilitySource = compatibilitySourceSession
	if a.compatibilityCacheEnabled {
		a.saveCompatibilityCache(appVersion, apiVersion)
	}
	return nil
}

func validateCompatibilityVersions(appVersion, apiVersion string) error {
	if compareDottedVersion(appVersion, minQbittorrentVersion) < 0 {
		return codedErrf(ExitLoginFail, "Unsupported qBittorrent version %s; qbitctl requires qBittorrent %s or later", appVersion, minQbittorrentVersion)
	}
	if compareDottedVersion(apiVersion, minWebAPIVersion) < 0 {
		return codedErrf(ExitLoginFail, "Unsupported qBittorrent WebAPI version %s; qbitctl requires WebAPI %s or later", apiVersion, minWebAPIVersion)
	}
	return nil
}
