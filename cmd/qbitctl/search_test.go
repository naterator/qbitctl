package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/naterator/qbitctl/internal/qbtest"
	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestSearchCommandDefaultsAndValidation(t *testing.T) {
	root := newRootCmd()
	search, _, err := root.Find([]string{"search"})
	if err != nil {
		t.Fatal(err)
	}
	if search.Use != "search <pattern> | --list-plugins" {
		t.Fatalf("search usage = %q, want list mode in usage", search.Use)
	}
	for name, want := range map[string]string{"plugin": qbt.DefaultSearchPlugins, "timeout": "30s", "json": "false", "list-plugins": "false"} {
		flag := search.Flags().Lookup(name)
		if flag == nil || flag.DefValue != want {
			t.Errorf("default %s flag = %v, want %s", name, flag, want)
		}
	}
	for name, want := range map[string]string{
		"plugin": "ID or full name",
		"json":   "plugin listing",
	} {
		flag := search.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("help flag --%s is missing", name)
			continue
		}
		if !strings.Contains(flag.Usage, want) {
			t.Errorf("help for --%s = %q, want %q", name, flag.Usage, want)
		}
	}
	root.SetArgs([]string{"search", "Ubuntu", "--timeout", "0s"})
	err = root.ExecuteContext(t.Context())
	var coded *qbt.CodedError
	if !errors.As(err, &coded) || coded.Code != exitBadArgs {
		t.Fatalf("invalid timeout error = %v, want argument error", err)
	}
}

func TestSearchRecentReadsCacheWithoutAuthentication(t *testing.T) {
	const (
		resultID   int64 = 123456789012
		resultSize int64 = 4294967296
	)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path, err := qbt.SearchResultsCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := map[string]any{
		"version": 2,
		"searches": []any{map[string]any{
			"url":      "http://qbt.test/",
			"user":     "admin",
			"saved_at": time.Now().Unix(),
			"query":    "ubuntu",
			"results": []any{map[string]any{
				"id": resultID, "url": "https://download.example/ubuntu.torrent", "plugin": "enabled",
				"name": "Ubuntu Desktop", "size": resultSize, "seeders": 15, "leechers": 3, "source": "example.org",
			}},
		}},
	}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var requests int
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			return nil, errors.New("search --recent unexpectedly contacted qBittorrent")
		})}
	})
	t.Cleanup(restore)
	root := newRootCmd()
	root.SetArgs([]string{"--url", "http://qbt.test", "--user", "admin", "search", "--recent", "--json"})
	stdout, stderr := captureOutput(t, func() { err = root.ExecuteContext(t.Context()) })
	if err != nil || stderr != "" {
		t.Fatalf("search --recent returned %v, stdout %q, stderr %q", err, stdout, stderr)
	}
	var rows []qbt.SearchResult
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("search --recent JSON = %q: %v", stdout, err)
	}
	if len(rows) != 1 || rows[0].ID != resultID || rows[0].Name != "Ubuntu Desktop" || rows[0].Size != resultSize {
		t.Fatalf("search --recent rows = %#v", rows)
	}
	if requests != 0 {
		t.Fatalf("search --recent made %d qBittorrent requests", requests)
	}
}

func TestSearchListPlugins(t *testing.T) {
	for _, tt := range []struct {
		name     string
		json     bool
		wantText string
	}{
		{
			name: "table",
			wantText: "Name\tID\tEnabled\n" +
				"Jackett\tjackett\ttrue\n" +
				"Enabled search\tenabled\tfalse\n",
		},
		{
			name:     "json",
			json:     true,
			wantText: `[{"name":"jackett","fullName":"Jackett","enabled":true},{"name":"enabled","fullName":"Enabled search","enabled":false}]` + "\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			var requests []string
			restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
				return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
					path := req.URL.Path
					requests = append(requests, path)
					resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}
					var body string
					switch path {
					case "/api/v2/auth/login":
						resp.StatusCode = http.StatusNoContent
						resp.Header.Set("Set-Cookie", "QBT_SID_8080=plugin-session; Path=/")
					case "/api/v2/app/version":
						body = "v5.3.0"
					case "/api/v2/app/webapiVersion":
						body = "2.16.2"
					case "/api/v2/search/plugins":
						body = `[{"name":"jackett","fullName":"Jackett","enabled":true},{"name":"enabled","fullName":"Enabled search","enabled":false}]`
					case "/api/v2/auth/logout":
						resp.StatusCode = http.StatusNoContent
					default:
						t.Fatalf("unexpected path: %s", path)
					}
					resp.Body = io.NopCloser(strings.NewReader(body))
					return resp, nil
				})}
			})
			t.Cleanup(restore)

			root := newRootCmd()
			args := []string{"--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "search", "--list-plugins"}
			if tt.json {
				args = append(args, "--json")
			}
			root.SetArgs(args)
			var commandErr error
			stdout, stderr := captureOutput(t, func() { commandErr = root.ExecuteContext(t.Context()) })
			if commandErr != nil || stderr != "" || stdout != tt.wantText {
				t.Fatalf("plugin listing returned %v, stdout %q, stderr %q", commandErr, stdout, stderr)
			}
			wantPaths := []string{"/api/v2/auth/login", "/api/v2/app/version", "/api/v2/app/webapiVersion", "/api/v2/search/plugins", "/api/v2/auth/logout"}
			if !slices.Equal(requests, wantPaths) {
				t.Fatalf("plugin listing requests = %v, want %v", requests, wantPaths)
			}
		})
	}
}

func TestSearchUsesSavedDefaultProvider(t *testing.T) {
	const catalog = `[{"name":"jackett","fullName":"Jackett","enabled":true},{"name":"disabled","fullName":"Disabled","enabled":false}]`
	for _, tt := range []struct {
		name             string
		saved            string
		explicit         string
		catalogStatus    int
		catalogBody      string
		wantPlugin       string
		wantCatalogCalls int
	}{
		{name: "missing uses enabled", wantPlugin: qbt.DefaultSearchPlugins},
		{name: "enabled group skips discovery", saved: qbt.DefaultSearchPlugins, wantPlugin: qbt.DefaultSearchPlugins},
		{name: "all group skips discovery", saved: "all", wantPlugin: "all"},
		{name: "saved ID is validated", saved: "jackett", catalogBody: catalog, wantPlugin: "jackett", wantCatalogCalls: 1},
		{name: "disabled saved ID falls back", saved: "disabled", catalogBody: catalog, wantPlugin: qbt.DefaultSearchPlugins, wantCatalogCalls: 1},
		{name: "stale saved ID falls back", saved: "missing", catalogBody: catalog, wantPlugin: qbt.DefaultSearchPlugins, wantCatalogCalls: 1},
		{name: "catalog failure falls back", saved: "jackett", catalogStatus: http.StatusInternalServerError, wantPlugin: qbt.DefaultSearchPlugins, wantCatalogCalls: 1},
		{name: "explicit plugin overrides saved", saved: "jackett", explicit: "all", wantPlugin: "all"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.saved != "" {
				if err := qbt.SetDefaultSearchPlugin(configPath, tt.saved); err != nil {
					t.Fatalf("SetDefaultSearchPlugin(%q): %v", tt.saved, err)
				}
			}

			var requests []string
			catalogCalls := 0
			restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
				return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
					path := req.URL.Path
					requests = append(requests, path)
					resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}
					var body string
					switch path {
					case "/api/v2/auth/login":
						resp.StatusCode = http.StatusNoContent
						resp.Header.Set("Set-Cookie", "QBT_SID_8080=saved-provider-session; Path=/")
					case "/api/v2/app/version":
						body = "v5.3.0"
					case "/api/v2/app/webapiVersion":
						body = "2.16.2"
					case "/api/v2/search/plugins":
						catalogCalls++
						if tt.catalogStatus != 0 {
							resp.StatusCode = tt.catalogStatus
						}
						body = tt.catalogBody
					case "/api/v2/search/start":
						if err := req.ParseForm(); err != nil {
							t.Fatal(err)
						}
						if got := req.Form.Get("plugins"); got != tt.wantPlugin {
							t.Errorf("search plugin = %q, want %q", got, tt.wantPlugin)
						}
						body = `{"id":42}`
					case "/api/v2/search/status":
						body = `[{"id":42,"status":"Stopped"}]`
					case "/api/v2/search/results":
						body = `{"results":[],"status":"Stopped","total":0}`
					case "/api/v2/search/delete":
						resp.StatusCode = http.StatusNotFound
					case "/api/v2/auth/logout":
						resp.StatusCode = http.StatusNoContent
					default:
						t.Fatalf("unexpected path: %s", path)
					}
					resp.Body = io.NopCloser(strings.NewReader(body))
					return resp, nil
				})}
			})
			t.Cleanup(restore)

			root := newRootCmd()
			args := []string{"--config", configPath, "--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "search", "plugins"}
			if tt.explicit != "" {
				args = append(args, "--plugin", tt.explicit)
			}
			args = append(args, "--json")
			root.SetArgs(args)
			var commandErr error
			stdout, stderr := captureOutput(t, func() { commandErr = root.ExecuteContext(t.Context()) })
			if commandErr != nil || stdout != "[]\n" || stderr != "" {
				t.Fatalf("saved provider search returned %v, stdout %q, stderr %q", commandErr, stdout, stderr)
			}

			wantPaths := []string{"/api/v2/auth/login", "/api/v2/app/version", "/api/v2/app/webapiVersion"}
			if tt.wantCatalogCalls > 0 {
				wantPaths = append(wantPaths, "/api/v2/search/plugins")
			}
			wantPaths = append(wantPaths, "/api/v2/search/start", "/api/v2/search/status", "/api/v2/search/results", "/api/v2/search/delete", "/api/v2/auth/logout")
			if !slices.Equal(requests, wantPaths) {
				t.Fatalf("saved provider requests = %v, want %v", requests, wantPaths)
			}
			if catalogCalls != tt.wantCatalogCalls {
				t.Fatalf("catalog calls = %d, want %d", catalogCalls, tt.wantCatalogCalls)
			}
		})
	}
}

func TestSearchListPluginsRejectsPatternAndSearchFlagsBeforeAuth(t *testing.T) {
	var loginCalls int
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/v2/auth/login" {
				loginCalls++
			}
			return nil, errors.New("list validation should prevent authentication")
		})}
	})
	t.Cleanup(restore)

	for _, args := range [][]string{
		{"search", "--list-plugins", "plugins"},
		{"search", "--list-plugins", "--plugin", "jackett"},
		{"search", "--list-plugins", "--timeout", "1s"},
		{"search", "--list-plugins", "--sort", "seeders-desc"},
		{"search", "--list-plugins", "--min-seeders", "1"},
		{"search", "--list-plugins", "--min-size", "1MB"},
		{"search", "--list-plugins", "--max-size", "1GB"},
		{"search", "--list-plugins", "--filter", "foo"},
		{"search", "--list-plugins", "--limit", "1"},
	} {
		root := newRootCmd()
		root.SetArgs(args)
		err := root.ExecuteContext(t.Context())
		var coded *qbt.CodedError
		if !errors.As(err, &coded) || coded.Code != exitBadArgs {
			t.Fatalf("args %v returned %v, want ExitBadArgs", args, err)
		}
	}
	if loginCalls != 0 {
		t.Fatalf("list validation attempted %d logins", loginCalls)
	}
}

func TestSearchCommandLifecycle(t *testing.T) {
	const (
		startPath   = "/api/v2/search/start"
		statusPath  = "/api/v2/search/status"
		resultsPath = "/api/v2/search/results"
		deletePath  = "/api/v2/search/delete"
	)
	for _, tt := range []struct {
		name         string
		pattern      string
		flags        []string
		afterStart   []string
		cancelAt     string
		statusError  bool
		resultsError bool
		resultBody   string
		wantOut      string
		wantIDs      []bool
		wantError    string
		wantWarning  string
		waitForPoll  bool
		deleteStatus int
	}{
		{
			name: "success", afterStart: []string{statusPath, statusPath, resultsPath}, waitForPoll: true,
			resultBody: `{"results":[{"fileName":"Ubuntu Desktop","fileSize":4294967296,"nbSeeders":15,"nbLeechers":3,"descrLink":"https://tracker.example/42","siteUrl":"https://jackett.example","fileUrl":"https://jackett.example/api?apikey=mock-key","engineName":"jackett"},{"fileName":"Ubuntu Server","fileSize":2147483648,"nbSeeders":8,"nbLeechers":1,"siteUrl":"https://fallback.example/path"}],"status":"Stopped","total":2}`,
			wantOut:    `[{"name":"Ubuntu Desktop","size":4294967296,"seeders":15,"leechers":3,"source":"tracker.example"},{"name":"Ubuntu Server","size":2147483648,"seeders":8,"leechers":1,"source":"fallback.example"}]`,
			wantIDs:    []bool{true, false},
		},
		{
			name: "no results", flags: []string{"--plugin", "enabled"}, afterStart: []string{statusPath, resultsPath},
			resultBody: `{"results":[],"status":"Stopped","total":0}`, wantOut: "[]", deleteStatus: http.StatusNotFound,
		},
		{
			name: "literal plugins pattern", pattern: "plugins", afterStart: []string{statusPath, resultsPath},
			resultBody: `{"results":[],"status":"Stopped","total":0}`, wantOut: "[]", deleteStatus: http.StatusNotFound,
		},
		{
			name: "multiple plugin selection", flags: []string{"--plugin", "jackett|enabled"}, afterStart: []string{statusPath, resultsPath},
			resultBody: `{"results":[],"status":"Stopped","total":0}`, wantOut: "[]", deleteStatus: http.StatusNotFound,
		},
		{
			name: "full plugin name", flags: []string{"--plugin", "Jackett"}, afterStart: []string{statusPath, resultsPath},
			resultBody: `{"results":[],"status":"Stopped","total":0}`, wantOut: "[]", deleteStatus: http.StatusNotFound,
		},
		{
			name: "filtered and limited", flags: []string{"--sort", "seeders-desc", "--min-seeders", "10", "--limit", "1"},
			afterStart: []string{statusPath, resultsPath},
			resultBody: `{"results":[{"fileName":"Ubuntu Desktop","fileSize":4294967296,"nbSeeders":15,"nbLeechers":3,"descrLink":"https://tracker.example/42","siteUrl":"https://jackett.example","fileUrl":"https://jackett.example/api?apikey=mock-key","engineName":"jackett"},{"fileName":"Ubuntu Server","fileSize":2147483648,"nbSeeders":8,"nbLeechers":1,"siteUrl":"https://fallback.example/path"}],"status":"Stopped","total":2}`,
			wantOut:    `[{"name":"Ubuntu Desktop","size":4294967296,"seeders":15,"leechers":3,"source":"tracker.example"}]`, wantIDs: []bool{true},
		},
		{
			name: "timeout", flags: []string{"--timeout", "100ms"}, afterStart: []string{statusPath, resultsPath},
			waitForPoll: true,
			resultBody:  `{"results":[{"fileName":"Ubuntu partial","fileSize":123,"nbSeeders":4,"nbLeechers":2,"descrLink":"https://tracker.example/partial","fileUrl":"magnet:?xt=urn:btih:partial","engineName":"jackett"}],"status":"Running","total":1}`,
			wantOut:     `[{"name":"Ubuntu partial","size":123,"seeders":4,"leechers":2,"source":"tracker.example"}]`,
			wantIDs:     []bool{true},
			wantWarning: "[WARN] Search reached the 100ms limit. Returning the results found so far.\n",
		},
		{
			name: "timeout with no results", flags: []string{"--timeout", "100ms"}, afterStart: []string{statusPath, resultsPath},
			waitForPoll: true, resultBody: `{"results":[],"status":"Running","total":0}`, wantOut: "[]",
			wantWarning: "[WARN] Search reached the 100ms limit. Returning the results found so far.\n",
		},
		{
			name: "timeout results failure", flags: []string{"--timeout", "100ms"}, afterStart: []string{statusPath, resultsPath},
			waitForPoll: true, resultsError: true,
			wantError: "Search reached the 100ms limit and could not fetch results: http error: 500",
		},
		{
			name: "cancellation during search", cancelAt: statusPath, afterStart: []string{statusPath},
			wantError: "Search canceled",
		},
		{
			name: "cancellation before start response", cancelAt: startPath,
			wantError: "Search canceled",
		},
		{
			name: "status failure", afterStart: []string{statusPath}, statusError: true,
			wantError: "Failed to check search status: http error: 500 ([{\"id\":42,\"status\":\"Stopped\"}])",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var requests []string
			var polls int
			restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
				return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
					path := req.URL.Path
					requests = append(requests, path)
					resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}
					if path != "/api/v2/auth/login" {
						cookie, err := req.Cookie("QBT_SID_8080")
						if err != nil || cookie.Value != "search-session" {
							t.Fatalf("%s did not use the command session", path)
						}
					}
					if path == tt.cancelAt {
						cancel()
						if path != startPath {
							return nil, req.Context().Err()
						}
						if req.Context().Err() != nil {
							t.Fatal("cancellation interrupted the search start response")
						}
					}
					if path == startPath || path == deletePath {
						if err := req.ParseForm(); err != nil || req.Method != http.MethodPost {
							t.Fatalf("invalid POST %s: method=%s error=%v", path, req.Method, err)
						}
					}
					if path == statusPath || path == resultsPath {
						if req.Method != http.MethodGet || req.URL.Query().Get("id") != "42" {
							t.Fatalf("invalid search request: %s %s", req.Method, req.URL)
						}
					}
					var body string
					switch path {
					case "/api/v2/auth/login":
						resp.StatusCode = http.StatusNoContent
						resp.Header.Set("Set-Cookie", "QBT_SID_8080=search-session; Path=/")
					case "/api/v2/app/version":
						body = "v5.3.0"
					case "/api/v2/app/webapiVersion":
						body = "2.16.2"
					case "/api/v2/search/plugins":
						body = `[{"name":"jackett","fullName":"Jackett","enabled":true},{"name":"enabled","fullName":"Enabled","enabled":true}]`
					case startPath:
						plugin := qbt.DefaultSearchPlugins
						if tt.name == "no results" {
							plugin = "enabled"
						}
						if tt.name == "multiple plugin selection" {
							plugin = "jackett|enabled"
						}
						if tt.name == "full plugin name" {
							plugin = "jackett"
						}
						pattern := "Ubuntu 24.04"
						if tt.pattern != "" {
							pattern = tt.pattern
						}
						want := url.Values{"pattern": {pattern}, "plugins": {plugin}, "category": {"all"}}
						if !reflect.DeepEqual(req.Form, want) {
							t.Errorf("start form = %v, want %v", req.Form, want)
						}
						body = `{"id":42}`
					case statusPath:
						polls++
						body = `[{"id":42,"status":"Stopped"}]`
						if tt.waitForPoll && polls == 1 {
							body = `[{"id":42,"status":"Running"}]`
						}
						if tt.statusError {
							resp.StatusCode = http.StatusInternalServerError
						}
					case resultsPath:
						if req.Context().Err() != nil {
							t.Errorf("results request used an expired context: %v", req.Context().Err())
						}
						body = tt.resultBody
						if tt.resultsError {
							resp.StatusCode = http.StatusInternalServerError
						}
					case deletePath:
						if req.Form.Get("id") != "42" || req.Context().Err() != nil {
							t.Errorf("delete did not use a fresh context for job 42: form=%v err=%v", req.Form, req.Context().Err())
						}
						if tt.deleteStatus != 0 {
							resp.StatusCode = tt.deleteStatus
						}
					case "/api/v2/auth/logout":
						resp.StatusCode = http.StatusNoContent
					default:
						t.Fatalf("unexpected path: %s", path)
					}
					resp.Body = io.NopCloser(strings.NewReader(body))
					return resp, nil
				})}
			})
			t.Cleanup(restore)

			root := newRootCmd()
			patternArgs := []string{"Ubuntu", "24.04"}
			if tt.pattern != "" {
				patternArgs = []string{tt.pattern}
			}
			args := append([]string{"--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "search"}, patternArgs...)
			args = append(args, "--json")
			root.SetArgs(append(args, tt.flags...))
			var commandErr error
			stdout, stderr := captureOutput(t, func() { commandErr = root.ExecuteContext(ctx) })
			if tt.wantError == "" {
				if commandErr != nil || !searchJSONMatches(stdout, tt.wantOut, tt.wantIDs) || stderr != tt.wantWarning {
					t.Errorf("search returned %v, stdout %q, stderr %q", commandErr, stdout, stderr)
				}
			} else {
				var exit exitError
				if !errors.As(commandErr, &exit) || exit.code != exitFetchFail || stdout != "" || stderr != "[ERROR] "+tt.wantError+"\n" {
					t.Errorf("failed search returned %#v, stdout %q, stderr %q", commandErr, stdout, stderr)
				}
			}
			wantPaths := []string{"/api/v2/auth/login", "/api/v2/app/version", "/api/v2/app/webapiVersion", startPath}
			if tt.name == "multiple plugin selection" || tt.name == "full plugin name" {
				wantPaths = append(wantPaths[:3], "/api/v2/search/plugins", startPath)
			}
			wantPaths = append(wantPaths, tt.afterStart...)
			wantPaths = append(wantPaths, deletePath, "/api/v2/auth/logout")
			if !slices.Equal(requests, wantPaths) {
				t.Errorf("request paths = %v, want %v", requests, wantPaths)
			}
		})
	}
}

func searchJSONMatches(output, expected string, wantIDs []bool) bool {
	var got, want []qbt.SearchResult
	if json.Unmarshal([]byte(output), &got) != nil || json.Unmarshal([]byte(expected), &want) != nil || got == nil {
		return false
	}
	for i := range got {
		hasID := got[i].ID >= 100_000_000_000 && got[i].ID <= 999_999_999_999
		if hasID != (i < len(wantIDs) && wantIDs[i]) || (!hasID && got[i].ID != 0) {
			return false
		}
		got[i].ID = 0
	}
	return slices.Equal(got, want)
}

func TestSearchResultCanBeAddedByID(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	const (
		startPath    = "/api/v2/search/start"
		statusPath   = "/api/v2/search/status"
		resultsPath  = "/api/v2/search/results"
		deletePath   = "/api/v2/search/delete"
		metadataPath = "/api/v2/torrents/fetchMetadata"
		addPath      = "/api/v2/torrents/add"
		logoutPath   = "/api/v2/auth/logout"
	)
	var requests []string
	var deleteForm, downloadForm url.Values
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			requests = append(requests, path)
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}
			if path != "/api/v2/auth/login" {
				cookie, err := req.Cookie("QBT_SID_8080")
				if err != nil || cookie.Value != "search-session" {
					t.Fatalf("%s did not use the command session", path)
				}
			}
			var body string
			switch path {
			case "/api/v2/auth/login":
				resp.StatusCode = http.StatusNoContent
				resp.Header.Set("Set-Cookie", "QBT_SID_8080=search-session; Path=/")
			case "/api/v2/app/version":
				body = "v5.3.0"
			case "/api/v2/app/webapiVersion":
				body = "2.16.2"
			case startPath:
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				body = `{"id":42}`
			case statusPath:
				body = `[{"id":42,"status":"Stopped"}]`
			case resultsPath:
				body = `{"results":[{"fileName":"Ubuntu Desktop","fileSize":4294967296,"nbSeeders":15,"nbLeechers":3,"descrLink":"https://tracker.example/42","siteUrl":"https://jackett.example","fileUrl":"https://jackett.example/api?apikey=mock-key","engineName":"jackett"}],"status":"Stopped","total":1}`
			case deletePath:
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				deleteForm = qbtest.CloneValues(req.Form)
				resp.StatusCode = http.StatusNoContent
			case metadataPath:
				body = `{}`
			case addPath:
				if err := req.ParseMultipartForm(32 << 10); err != nil {
					t.Fatal(err)
				}
				downloadForm = qbtest.CloneValues(req.MultipartForm.Value)
				body = `{"success_count":1}`
			case logoutPath:
				resp.StatusCode = http.StatusNoContent
			default:
				t.Fatalf("unexpected path: %s", path)
			}
			resp.Body = io.NopCloser(strings.NewReader(body))
			return resp, nil
		})}
	})
	t.Cleanup(restore)

	searchRoot := newRootCmd()
	searchRoot.SetArgs([]string{"--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "search", "Ubuntu", "--json"})
	var searchErr error
	stdout, stderr := captureOutput(t, func() { searchErr = searchRoot.ExecuteContext(t.Context()) })
	if searchErr != nil || stderr != "" || strings.Contains(stdout, "mock-key") {
		t.Fatalf("search returned %v, stdout %q, stderr %q", searchErr, stdout, stderr)
	}
	var rows []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil || len(rows) != 1 || rows[0].ID <= 0 {
		t.Fatalf("search output = %q, want one addable result ID", stdout)
	}
	searchID := strconv.FormatInt(rows[0].ID, 10)
	if !reflect.DeepEqual(deleteForm, url.Values{"id": {"42"}}) {
		t.Fatalf("search delete form = %v, want job 42", deleteForm)
	}
	if deleteIndex := slices.Index(requests, deletePath); deleteIndex < 0 || deleteIndex+1 >= len(requests) || requests[deleteIndex+1] != logoutPath {
		t.Fatalf("search cleanup order = %v, want delete immediately before logout", requests)
	}

	addRoot := newRootCmd()
	addRoot.SetArgs([]string{"--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "add", "--search-id", searchID, "--category", "linux", "--save-path", "/downloads/linux", "--tags", "iso,linux", "--stopped"})
	var addErr error
	addStdout, addStderr := captureOutput(t, func() { addErr = addRoot.ExecuteContext(t.Context()) })
	if addErr != nil || addStdout != "" || addStderr != "" {
		t.Fatalf("add by search ID returned %v, stdout %q, stderr %q", addErr, addStdout, addStderr)
	}
	if !reflect.DeepEqual(downloadForm, url.Values{
		"urls":       {"https://jackett.example/api?apikey=mock-key"},
		"downloader": {"jackett"},
		"category":   {"linux"},
		"savepath":   {"/downloads/linux"},
		"autoTMM":    {"false"},
		"tags":       {"iso,linux"},
		"stopped":    {"true"},
	}) {
		t.Fatalf("download form = %v, want cached result URL and plugin", downloadForm)
	}
	metadataIndex := slices.Index(requests, metadataPath)
	if metadataIndex < 0 || !slices.Equal(requests[metadataIndex:], []string{metadataPath, addPath, logoutPath}) {
		t.Fatalf("request paths = %v, want fetch metadata and add before logout", requests)
	}
}

func TestAddRejectsSearchIDWithPositionalSourceBeforeLogin(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var loginCalls int
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/v2/auth/login" {
				loginCalls++
			}
			return nil, errors.New("authentication should not start for invalid add arguments")
		})}
	})
	t.Cleanup(restore)
	root := newRootCmd()
	root.SetArgs([]string{"--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "add", "ubuntu.torrent", "--search-id", "123456789012"})
	err := root.ExecuteContext(t.Context())
	var coded *qbt.CodedError
	if !errors.As(err, &coded) || coded.Code != exitBadArgs || loginCalls != 0 {
		t.Fatalf("invalid add arguments returned %v with %d login calls, want ExitBadArgs before auth", err, loginCalls)
	}
}
