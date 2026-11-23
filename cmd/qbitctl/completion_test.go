package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

func TestCompletionProtocolStaticArguments(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		want      []string
		directive cobra.ShellCompDirective
	}{
		{name: "get fields", args: []string{"__complete", "get", ""}, want: append(append([]string{}, qbt.GetFieldNames...), "tracker-list"), directive: cobra.ShellCompDirectiveNoFileComp},
		{name: "set toggles", args: []string{"__complete", "set", "seqdl", ""}, want: []string{"true", "false"}, directive: cobra.ShellCompDirectiveNoFileComp},
		{name: "set seedtime", args: []string{"__complete", "set", "seedtime-limit", "1"}, want: []string{"1h", "1d"}, directive: cobra.ShellCompDirectiveNoFileComp},
		{name: "move path", args: []string{"__complete", "move", ""}, want: nil, directive: cobra.ShellCompDirectiveNoFileComp},
		{name: "add file", args: []string{"__complete", "add", ""}, want: []string{"torrent"}, directive: cobra.ShellCompDirectiveFilterFileExt},
		{name: "tui default", args: []string{"__complete", "tui-default", "o"}, want: []string{"on", "off"}, directive: cobra.ShellCompDirectiveNoFileComp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, directive := runCompletion(t, tt.args)
			if !equalStrings(got, tt.want) {
				t.Fatalf("completions = %v, want %v", got, tt.want)
			}
			if directive != tt.directive {
				t.Fatalf("directive = %d, want %d", directive, tt.directive)
			}
		})
	}
}

func TestCompletionProtocolTorrentHashes(t *testing.T) {
	server := newQBTestServer(t)
	first := "0123456789abcdef0123456789abcdef01234567"
	second := "89abcdef0123456789abcdef0123456789abcdef"
	server.Torrents = []TorrentInfo{
		{Name: "Ubuntu ISO", Hash: first},
		{Name: "Fedora ISO", Hash: second},
	}
	withQBServerClientFactory(t, server)
	writeCompletionConfig(t, server)

	got, directive := runCompletion(t, []string{"__complete", "show", ""})
	if !equalStrings(got, []string{first + "\tUbuntu ISO", second + "\tFedora ISO"}) {
		t.Fatalf("show completions = %v", got)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("show directive = %d, want %d", directive, cobra.ShellCompDirectiveNoFileComp)
	}

	got, _ = runCompletion(t, []string{"__complete", "start", first, ""})
	if !equalStrings(got, []string{second + "\tFedora ISO"}) {
		t.Fatalf("excluded completions = %v", got)
	}

	got, _ = runCompletion(t, []string{"__complete", "show", "ubu"})
	if !equalStrings(got, []string{first + "\tUbuntu ISO"}) {
		t.Fatalf("name completions = %v", got)
	}
}

func TestCompletionSanitizesTorrentNames(t *testing.T) {
	server := newQBTestServer(t)
	hash := "0123456789abcdef0123456789abcdef01234567"
	server.Torrents = []TorrentInfo{{Name: "Ubuntu\nISO\t[unsafe]", Hash: hash}}
	withQBServerClientFactory(t, server)
	writeCompletionConfig(t, server)

	got, _ := runCompletion(t, []string{"__complete", "show", ""})
	if !equalStrings(got, []string{hash + "\tUbuntu ISO [unsafe]"}) {
		t.Fatalf("sanitized completions = %v", got)
	}
}

func TestCompletionWithoutSetupDoesNotLogin(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	got, directive := runCompletion(t, []string{"__complete", "show", ""})
	if len(got) != 0 || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("completion = %v, directive %d; want no results and no-file", got, directive)
	}
	if server.LoginCalls != 0 {
		t.Fatalf("LoginCalls = %d, want 0", server.LoginCalls)
	}
}

func TestCompletionAcceptsEnvironmentPasswordWithoutSavedSetup(t *testing.T) {
	server := newQBTestServer(t)
	hash := "0123456789abcdef0123456789abcdef01234567"
	server.Torrents = []TorrentInfo{{Name: "Ubuntu ISO", Hash: hash}}
	withQBServerClientFactory(t, server)
	creds := server.creds()
	t.Setenv("QBITCTL_PASSWORD", creds.Pass)

	got, directive := completeTorrentHashes(&CLIOptions{URL: creds.URL, User: creds.User}, nil, "ubu")
	if want := []string{hash + "\tUbuntu ISO"}; !equalStrings(got, want) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("completion = %v, directive %d; want %v", got, directive, want)
	}
	if server.LoginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", server.LoginCalls)
	}
}

func TestCompletionOverArityDoesNotLogin(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "get", args: []string{"__complete", "get", "name", "hash", "extra", ""}},
		{name: "set", args: []string{"__complete", "set", "seqdl", "true", "hash", "extra", ""}},
		{name: "show", args: []string{"__complete", "show", "hash", "extra", ""}},
		{name: "move", args: []string{"__complete", "move", "/downloads", "hash", "extra", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newQBTestServer(t)
			withQBServerClientFactory(t, server)
			writeCompletionConfig(t, server)

			got, directive := runCompletion(t, tt.args)
			if len(got) != 0 || directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf("completion = %v, directive %d; want no candidates and no-file", got, directive)
			}
			if server.LoginCalls != 0 {
				t.Fatalf("LoginCalls = %d, want 0", server.LoginCalls)
			}
		})
	}
}

func TestCompletionTimeoutReturnsErrorDirective(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	writeCompletionConfig(t, server)
	oldTimeout := completionTimeout
	completionTimeout = 100 * time.Millisecond
	t.Cleanup(func() { completionTimeout = oldTimeout })
	release := make(chan struct{})
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/v2/torrents/info" {
				select {
				case <-release:
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
			}
			return server.roundTrip(req), nil
		})}
	})
	t.Cleanup(func() {
		close(release)
		restore()
	})

	started := time.Now()
	got, directive := runCompletion(t, []string{"__complete", "show", ""})
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("timeout completion took %s", elapsed)
	}
	if len(got) != 0 || directive&cobra.ShellCompDirectiveError == 0 {
		t.Fatalf("timeout completion = %v, directive %d; want error", got, directive)
	}
}

func TestCompletionFreshCacheAvoidsNetworkAndPasswordDecryption(t *testing.T) {
	server := newQBTestServer(t)
	hash := "0123456789abcdef0123456789abcdef01234567"
	server.Torrents = []TorrentInfo{{Name: "Ubuntu ISO", Hash: hash}}
	withQBServerClientFactory(t, server)
	writeCompletionConfig(t, server)

	got, directive := runCompletion(t, []string{"__complete", "show", ""})
	if !equalStrings(got, []string{hash + "\tUbuntu ISO"}) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("initial completion = %v, directive %d", got, directive)
	}
	if server.LoginCalls != 1 || server.InfoCalls != 1 {
		t.Fatalf("initial request counts = login %d/info %d, want 1/1", server.LoginCalls, server.InfoCalls)
	}
	cachePath, err := completionCachePath()
	if err != nil {
		t.Fatalf("completion cache path: %v", err)
	}
	cacheData, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read completion cache: %v", err)
	}
	if strings.Contains(string(cacheData), "password") || strings.Contains(string(cacheData), "SID") {
		t.Fatalf("completion cache contains authentication data: %s", cacheData)
	}

	// A fresh cache only needs the non-secret setup scope. Clearing the key
	// means a network path would fail, so a successful completion proves that
	// it neither decrypts the saved password nor constructs an HTTP client.
	t.Setenv("QBITCTL_MASTER_KEY", "")
	factoryCalls := 0
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		factoryCalls++
		return &http.Client{Jar: jar, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network should not be used for a fresh completion cache")
		})}
	})
	t.Cleanup(restore)

	got, directive = runCompletion(t, []string{"__complete", "show", "ubu"})
	if !equalStrings(got, []string{hash + "\tUbuntu ISO"}) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("fresh completion = %v, directive %d", got, directive)
	}
	if factoryCalls != 0 || server.LoginCalls != 1 || server.InfoCalls != 1 {
		t.Fatalf("fresh request counts = factory %d/login %d/info %d, want 0/1/1", factoryCalls, server.LoginCalls, server.InfoCalls)
	}
}

func TestCompletionCacheIsScopedByServerAndUser(t *testing.T) {
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Current server", Hash: "0123456789abcdef0123456789abcdef01234567"}}
	withQBServerClientFactory(t, server)
	writeCompletionConfig(t, server)

	setup, err := qbt.ReadSetup(qbt.Options{})
	if err != nil {
		t.Fatalf("read setup: %v", err)
	}
	otherTorrents := []qbt.TorrentInfo{{Name: "Wrong scope", Hash: "89abcdef0123456789abcdef0123456789abcdef"}}
	for _, other := range []qbt.Setup{
		{URL: setup.URL, User: "different-user"},
		{URL: "http://other-qbt.test", User: setup.User},
	} {
		if err := writeCompletionCache(other, otherTorrents, time.Now()); err != nil {
			t.Fatalf("write scoped cache: %v", err)
		}
	}

	got, directive := runCompletion(t, []string{"__complete", "show", ""})
	want := []string{"0123456789abcdef0123456789abcdef01234567\tCurrent server"}
	if !equalStrings(got, want) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("scoped completion = %v, directive %d; want %v", got, directive, want)
	}
	if server.LoginCalls != 1 || server.InfoCalls != 1 {
		t.Fatalf("scoped request counts = login %d/info %d, want 1/1", server.LoginCalls, server.InfoCalls)
	}
}

func TestCompletionStaleCacheFallsBackWhenRefreshFails(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	writeCompletionConfig(t, server)
	setup, err := qbt.ReadSetup(qbt.Options{})
	if err != nil {
		t.Fatalf("read setup: %v", err)
	}
	hash := "0123456789abcdef0123456789abcdef01234567"
	if err := writeCompletionCache(setup, []qbt.TorrentInfo{{Name: "Cached torrent", Hash: hash}}, time.Now().Add(-31*time.Second)); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("offline")
		})}
	})
	t.Cleanup(restore)

	got, directive := runCompletion(t, []string{"__complete", "show", ""})
	if !equalStrings(got, []string{hash + "\tCached torrent"}) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("stale fallback = %v, directive %d", got, directive)
	}
}

func TestCompletionExpiredCacheDoesNotHideRefreshFailure(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	writeCompletionConfig(t, server)
	setup, err := qbt.ReadSetup(qbt.Options{})
	if err != nil {
		t.Fatalf("read setup: %v", err)
	}
	if err := writeCompletionCache(setup, []qbt.TorrentInfo{{Name: "Too old", Hash: "0123456789abcdef0123456789abcdef01234567"}}, time.Now().Add(-6*time.Minute)); err != nil {
		t.Fatalf("write expired cache: %v", err)
	}
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("offline")
		})}
	})
	t.Cleanup(restore)

	got, directive := runCompletion(t, []string{"__complete", "show", ""})
	if len(got) != 0 || directive&cobra.ShellCompDirectiveError == 0 {
		t.Fatalf("expired cache completion = %v, directive %d; want refresh error", got, directive)
	}
}

func TestCompletionCacheReadFailureRefreshesNormally(t *testing.T) {
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Recovered server", Hash: "0123456789abcdef0123456789abcdef01234567"}}
	withQBServerClientFactory(t, server)
	writeCompletionConfig(t, server)
	cachePath, err := completionCachePath()
	if err != nil {
		t.Fatalf("completion cache path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("create cache directory: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write malformed cache: %v", err)
	}

	got, directive := runCompletion(t, []string{"__complete", "show", ""})
	want := []string{"0123456789abcdef0123456789abcdef01234567\tRecovered server"}
	if !equalStrings(got, want) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("malformed-cache completion = %v, directive %d; want %v", got, directive, want)
	}
	if server.LoginCalls != 1 || server.InfoCalls != 1 {
		t.Fatalf("malformed-cache request counts = login %d/info %d, want 1/1", server.LoginCalls, server.InfoCalls)
	}
}

func writeCompletionConfig(t *testing.T, server *qbTestServer) {
	t.Helper()
	t.Setenv("QBITCTL_MASTER_KEY", "completion-test-master")
	if err := qbt.ExecuteConfigWrite(qbt.Options{
		URL:  server.creds().URL,
		User: "admin",
		Pass: "secret",
	}, "", io.Discard); err != nil {
		t.Fatalf("write completion config: %v", err)
	}
}

func runCompletion(t *testing.T, args []string) ([]string, cobra.ShellCompDirective) {
	t.Helper()
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("completion returned error: %v (stderr %q)", err, stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[len(lines)-1], ":") {
		t.Fatalf("completion output = %q", stdout.String())
	}
	directive, err := strconv.Atoi(strings.TrimPrefix(lines[len(lines)-1], ":"))
	if err != nil {
		t.Fatalf("completion directive = %q: %v", lines[len(lines)-1], err)
	}
	return lines[:len(lines)-1], cobra.ShellCompDirective(directive)
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
