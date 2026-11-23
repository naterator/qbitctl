package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/naterator/qbitctl/internal/configfile"
	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

// completionTimeout bounds the complete server interaction, including login,
// compatibility checks, and the torrent list request.
var completionTimeout = 3 * time.Second

const (
	completionCacheVersion    = 1
	completionCacheFresh      = 30 * time.Second
	completionCacheStaleLimit = 5 * time.Minute
	completionCacheScopeLimit = 32
	completionCacheDirName    = "qbitctl"
	completionCacheFileName   = "completion.json"
)

// The completion cache contains only sanitized torrent metadata. It is scoped
// to the configured Web API URL and username; passwords and session IDs never
// enter the cache, so a fresh lookup can avoid authentication entirely.
type completionCacheTorrent struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
}

type completionCacheSnapshot struct {
	URL       string                   `json:"url"`
	User      string                   `json:"user"`
	UpdatedAt int64                    `json:"updated_at"`
	Torrents  []completionCacheTorrent `json:"torrents"`
}

type completionCacheFile struct {
	Version   int                       `json:"version"`
	Snapshots []completionCacheSnapshot `json:"snapshots"`
}

func completionCachePath() (string, error) {
	base := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME"))
	if base == "" || !filepath.IsAbs(base) {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, completionCacheDirName, completionCacheFileName), nil
}

func completionCacheScope(setup qbt.Setup) (string, string, bool) {
	urlValue := strings.TrimRight(strings.TrimSpace(setup.URL), "/")
	user := strings.TrimSpace(setup.User)
	return urlValue, user, urlValue != "" && user != ""
}

func readCompletionCache(setup qbt.Setup, now time.Time) (completionCacheSnapshot, time.Duration, bool, error) {
	urlValue, user, ok := completionCacheScope(setup)
	if !ok {
		return completionCacheSnapshot{}, 0, false, nil
	}
	path, err := completionCachePath()
	if err != nil {
		return completionCacheSnapshot{}, 0, false, err
	}
	object, err := configfile.Read(path)
	if err != nil {
		return completionCacheSnapshot{}, 0, false, err
	}
	var cache completionCacheFile
	if raw := object["version"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &cache.Version); err != nil {
			return completionCacheSnapshot{}, 0, false, err
		}
	}
	if cache.Version != completionCacheVersion {
		return completionCacheSnapshot{}, 0, false, nil
	}
	if raw := object["snapshots"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &cache.Snapshots); err != nil {
			return completionCacheSnapshot{}, 0, false, err
		}
	}
	for _, snapshot := range cache.Snapshots {
		if snapshot.URL != urlValue || snapshot.User != user || snapshot.UpdatedAt <= 0 {
			continue
		}
		age := now.Sub(time.Unix(snapshot.UpdatedAt, 0))
		if age < 0 || age > completionCacheStaleLimit {
			return completionCacheSnapshot{}, 0, false, nil
		}
		return snapshot, age, true, nil
	}
	return completionCacheSnapshot{}, 0, false, nil
}

func writeCompletionCache(setup qbt.Setup, torrents []qbt.TorrentInfo, now time.Time) error {
	urlValue, user, ok := completionCacheScope(setup)
	if !ok {
		return nil
	}
	path, err := completionCachePath()
	if err != nil {
		return err
	}
	entries := make([]completionCacheTorrent, 0, len(torrents))
	for _, torrent := range torrents {
		hash := strings.ToLower(strings.TrimSpace(torrent.Hash))
		if !isFullTorrentHash(hash) {
			continue
		}
		entries = append(entries, completionCacheTorrent{
			Hash: hash,
			Name: qbt.SanitizeServerText(torrent.Name),
		})
	}
	snapshot := completionCacheSnapshot{URL: urlValue, User: user, UpdatedAt: now.Unix(), Torrents: entries}
	return configfile.UpdateCache(path, func(object configfile.Object) (bool, error) {
		cache := completionCacheFile{Version: completionCacheVersion}
		if raw := object["snapshots"]; len(raw) != 0 {
			_ = json.Unmarshal(raw, &cache.Snapshots)
		}
		updated := make([]completionCacheSnapshot, 0, min(len(cache.Snapshots)+1, completionCacheScopeLimit))
		updated = append(updated, snapshot)
		for _, previous := range cache.Snapshots {
			if len(updated) == completionCacheScopeLimit {
				break
			}
			if previous.URL == urlValue && previous.User == user {
				continue
			}
			age := now.Sub(time.Unix(previous.UpdatedAt, 0))
			if age < 0 || age > completionCacheStaleLimit {
				continue
			}
			updated = append(updated, previous)
		}
		encoded, err := json.Marshal(updated)
		if err != nil {
			return false, err
		}
		object["version"], object["snapshots"] = json.RawMessage("1"), encoded
		return true, nil
	})
}

func completeStatic(values []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	completions := make([]cobra.Completion, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, toComplete) {
			completions = append(completions, value)
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeGetArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) == 0 {
			fields := append(append([]string{}, qbt.GetFieldNames...), "tracker-list")
			return completeStatic(fields, toComplete)
		}
		if len(args) > 1 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeTorrentHashes(opts, nil, toComplete)
	}
}

func completeSetArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return completeStatic(qbt.SetFieldNames, toComplete)
		case 1:
			return completeSetValue(args[0], toComplete)
		case 2:
			return completeTorrentHashes(opts, args[2:], toComplete)
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}
}

func completeSetValue(field, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	var values []string
	switch qbt.CanonicalSetField(field) {
	case "seqdl", "autotmm", "superseed":
		values = []string{"true", "false"}
	case "seedtime-limit":
		values = []string{"-1", "-2", "1h", "1d"}
	}
	return completeStatic(values, toComplete)
}

func completeMoveArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return cobra.NoFileCompletions(nil, nil, toComplete)
		case 1:
			return completeTorrentHashes(opts, args[1:], toComplete)
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}
}

func completeTorrentHashArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return completeTorrentHashes(opts, args, toComplete)
	}
}

func completeSingleTorrentHashArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeTorrentHashes(opts, nil, toComplete)
	}
}

func completeAddArgs(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return []cobra.Completion{"torrent"}, cobra.ShellCompDirectiveFilterFileExt
}

// completeTorrentHashes uses the saved configuration only when it is already
// usable. Completion must never invoke first-run setup or read a password from
// stdin: Cobra's hidden __complete command bypasses PersistentPreRunE.
func completeTorrentHashes(opts *CLIOptions, exclude []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	authOpts := *opts
	if authOpts.Pass == "" {
		if password, ok := os.LookupEnv("QBITCTL_PASSWORD"); ok && password != "" {
			authOpts.Pass = password
		}
	}
	setup, err := qbt.ReadSetup(authOpts)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	now := time.Now()
	cached, age, hasCache, _ := readCompletionCache(setup, now)
	if hasCache && age < completionCacheFresh {
		return filterCompletionTorrents(cached.Torrents, exclude, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	var stale []completionCacheTorrent
	if hasCache {
		stale = cached.Torrents
	}
	if !setup.Ready() {
		if stale != nil {
			return filterCompletionTorrents(stale, exclude, toComplete), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	defer cancel()
	authOpts.UseCompatibilityCache = true
	app, err := qbt.NewClientWithOutput(ctx, &authOpts, io.Discard, io.Discard)
	if err != nil {
		if stale != nil {
			return filterCompletionTorrents(stale, exclude, toComplete), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveError
	}
	defer app.Logout()

	torrents, _, err := app.FetchAllTorrentsContext(ctx)
	if err != nil {
		if stale != nil {
			return filterCompletionTorrents(stale, exclude, toComplete), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveError
	}
	_ = writeCompletionCache(setup, torrents, time.Now())
	return filterCompletionTorrentsFromInfo(torrents, exclude, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func filterCompletionTorrentsFromInfo(torrents []qbt.TorrentInfo, exclude []string, toComplete string) []cobra.Completion {
	entries := make([]completionCacheTorrent, 0, len(torrents))
	for _, torrent := range torrents {
		entries = append(entries, completionCacheTorrent{Hash: torrent.Hash, Name: torrent.Name})
	}
	return filterCompletionTorrents(entries, exclude, toComplete)
}

func filterCompletionTorrents(torrents []completionCacheTorrent, exclude []string, toComplete string) []cobra.Completion {
	excluded := make([]string, 0, len(exclude))
	for _, value := range exclude {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			excluded = append(excluded, value)
		}
	}
	toComplete = strings.ToLower(toComplete)
	completions := make([]cobra.Completion, 0, min(len(torrents), 200))
	for _, torrent := range torrents {
		hash := strings.ToLower(strings.TrimSpace(torrent.Hash))
		if !isFullTorrentHash(hash) || excludedHash(hash, excluded) {
			continue
		}
		if !strings.HasPrefix(hash, toComplete) && !strings.HasPrefix(strings.ToLower(torrent.Name), toComplete) {
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(hash, qbt.SanitizeServerText(torrent.Name)))
		if len(completions) == 200 {
			break
		}
	}
	return completions
}

func excludedHash(hash string, excluded []string) bool {
	for _, value := range excluded {
		if strings.HasPrefix(hash, value) {
			return true
		}
	}
	return false
}

func isFullTorrentHash(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
