package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestSearchCommandDefaultsAndValidation(t *testing.T) {
	root := newRootCmd()
	search, _, err := root.Find([]string{"search"})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"plugin": "jackett", "timeout": "30s", "json": "false"} {
		flag := search.Flags().Lookup(name)
		if flag == nil || flag.DefValue != want {
			t.Errorf("default %s flag = %v, want %s", name, flag, want)
		}
	}
	root.SetArgs([]string{"search", "Ubuntu", "--timeout", "0s"})
	err = root.ExecuteContext(t.Context())
	var coded *qbt.CodedError
	if !errors.As(err, &coded) || coded.Code != exitBadArgs {
		t.Fatalf("invalid timeout error = %v, want argument error", err)
	}
}

func TestSearchCommandLifecycle(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	const (
		startPath   = "/api/v2/search/start"
		statusPath  = "/api/v2/search/status"
		resultsPath = "/api/v2/search/results"
		deletePath  = "/api/v2/search/delete"
	)
	for _, tt := range []struct {
		name         string
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
					case startPath:
						plugin := "jackett"
						if tt.name == "no results" {
							plugin = "enabled"
						}
						want := url.Values{"pattern": {"Ubuntu 24.04"}, "plugins": {plugin}, "category": {"all"}}
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
			args := []string{"--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "search", "Ubuntu", "24.04", "--json"}
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
	addRoot.SetArgs([]string{"--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "add", "--search-id", searchID})
	var addErr error
	addStdout, addStderr := captureOutput(t, func() { addErr = addRoot.ExecuteContext(t.Context()) })
	if addErr != nil || addStdout != "" || addStderr != "" {
		t.Fatalf("add by search ID returned %v, stdout %q, stderr %q", addErr, addStdout, addStderr)
	}
	if !reflect.DeepEqual(downloadForm, url.Values{"urls": {"https://jackett.example/api?apikey=mock-key"}, "downloader": {"jackett"}}) {
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
