package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestParseDataUnits(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  qbt.DataUnits
	}{
		{value: "", want: qbt.DataUnits("")},
		{value: "human", want: qbt.DataUnits("human")},
		{value: "HUMAN", want: qbt.DataUnits("human")},
		{value: "bytes", want: qbt.DataUnits("bytes")},
		{value: " Bytes ", want: qbt.DataUnits("bytes")},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, err := parseDataUnits(tt.value)
			if err != nil || got != tt.want {
				t.Fatalf("parseDataUnits(%q) = %q, %v; want %q", tt.value, got, err, tt.want)
			}
		})
	}
	if _, err := parseDataUnits("decimal"); err == nil {
		t.Fatal("parseDataUnits accepted an unsupported unit mode")
	} else {
		var coded *qbt.CodedError
		if !errors.As(err, &coded) || coded.Code != exitBadArgs {
			t.Fatalf("parseDataUnits error = %v, want ExitBadArgs", err)
		}
	}
}

func TestUnitsFlagRejectsInvalidValueBeforePasswordRead(t *testing.T) {
	var loginCalls int
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/v2/auth/login" {
				loginCalls++
			}
			return nil, errors.New("invalid units should prevent authentication")
		})}
	})
	t.Cleanup(restore)

	input := &countingReader{reader: strings.NewReader("secret\nremaining")}
	root := newRootCmd()
	root.SetIn(input)
	root.SetArgs([]string{"--units", "decimal", "--pass-stdin", "search", "Ubuntu"})
	err := root.ExecuteContext(t.Context())
	var exit exitError
	if !errors.As(err, &exit) || exit.code != exitBadArgs {
		t.Fatalf("invalid units returned %v, want ExitBadArgs", err)
	}
	if input.reads != 0 || loginCalls != 0 {
		t.Fatalf("invalid units read password/authenticated: reads=%d logins=%d", input.reads, loginCalls)
	}
}

func TestSearchUnitsModesEndToEnd(t *testing.T) {
	for _, tt := range []struct {
		name  string
		units string
		json  bool
	}{
		{name: "config human fallback"},
		{name: "explicit human", units: "human"},
		{name: "explicit bytes", units: "bytes"},
		{name: "bytes JSON", units: "bytes", json: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
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
						resp.Header.Set("Set-Cookie", "QBT_SID_8080=units-session; Path=/")
					case "/api/v2/app/version":
						body = "v5.3.0"
					case "/api/v2/app/webapiVersion":
						body = "2.16.2"
					case "/api/v2/search/start":
						if err := req.ParseForm(); err != nil {
							t.Fatal(err)
						}
						if got := req.Form.Get("plugins"); got != qbt.DefaultSearchPlugins {
							t.Errorf("search plugin = %q, want %q", got, qbt.DefaultSearchPlugins)
						}
						body = `{"id":42}`
					case "/api/v2/search/status":
						body = `[{"id":42,"status":"Stopped"}]`
					case "/api/v2/search/results":
						body = `{"results":[{"fileName":"Ubuntu","fileSize":1536,"nbSeeders":4,"nbLeechers":1,"descrLink":"https://tracker.example/42","engineName":"enabled"}],"status":"Stopped","total":1}`
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
			args := []string{"--config", configPath, "--url", "http://qbt.test", "--user", "admin", "--pass", "secret"}
			if tt.units != "" {
				args = append(args, "--units", tt.units)
			}
			args = append(args, "search", "Ubuntu")
			if tt.json {
				args = append(args, "--json")
			}
			root.SetArgs(args)
			var commandErr error
			stdout, stderr := captureOutput(t, func() { commandErr = root.ExecuteContext(t.Context()) })
			if commandErr != nil || stderr != "" {
				t.Fatalf("units search returned %v, stdout %q, stderr %q", commandErr, stdout, stderr)
			}
			if tt.json {
				var results []qbt.SearchResult
				if err := json.Unmarshal([]byte(stdout), &results); err != nil || len(results) != 1 || results[0].Size != 1536 {
					t.Fatalf("JSON output = %q, want numeric size 1536", stdout)
				}
				if strings.Contains(stdout, " B") || strings.Contains(stdout, "KiB") {
					t.Fatalf("JSON output formatted the numeric size: %q", stdout)
				}
			} else if tt.units == "bytes" {
				if !strings.Contains(stdout, "Size (bytes)") || !strings.Contains(stdout, "1536 B") || strings.Contains(stdout, "KiB") {
					t.Fatalf("bytes output = %q, want precise bytes", stdout)
				}
			} else if !strings.Contains(stdout, "Size") || !strings.Contains(stdout, "1.5 KiB") || strings.Contains(stdout, "1536 B") {
				t.Fatalf("human output = %q, want human size", stdout)
			}
			wantPaths := []string{"/api/v2/auth/login", "/api/v2/app/version", "/api/v2/app/webapiVersion", "/api/v2/search/start", "/api/v2/search/status", "/api/v2/search/results", "/api/v2/search/delete", "/api/v2/auth/logout"}
			if !slices.Equal(requests, wantPaths) {
				t.Fatalf("units requests = %v, want %v", requests, wantPaths)
			}
		})
	}
}
