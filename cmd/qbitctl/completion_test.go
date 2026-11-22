package main

import (
	"bytes"
	"io"
	"net/http"
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
