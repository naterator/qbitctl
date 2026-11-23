package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestSearchPluginManagementCLI(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		path string
		form url.Values
	}{
		{name: "list", args: []string{"plugins", "list", "--json"}, path: "/api/v2/search/plugins"},
		{name: "install", args: []string{"plugins", "install", "https://plugins.example/jackett.py"}, path: "/api/v2/search/installPlugin", form: url.Values{"sources": {"https://plugins.example/jackett.py"}}},
		{name: "enable", args: []string{"plugins", "enable", "jackett"}, path: "/api/v2/search/enablePlugin", form: url.Values{"names": {"jackett"}, "enable": {"true"}}},
		{name: "disable", args: []string{"plugins", "disable", "jackett"}, path: "/api/v2/search/enablePlugin", form: url.Values{"names": {"jackett"}, "enable": {"false"}}},
		{name: "uninstall", args: []string{"plugins", "uninstall", "jackett"}, path: "/api/v2/search/uninstallPlugin", form: url.Values{"names": {"jackett"}}},
		{name: "update", args: []string{"plugins", "update"}, path: "/api/v2/search/updatePlugins", form: url.Values{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, []byte(`{"url":"http://qbt.test","user":"admin"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.name == "uninstall" {
				if err := qbt.SetDefaultSearchPlugin(configPath, "jackett"); err != nil {
					t.Fatal(err)
				}
			}
			var requests []string
			var gotForm url.Values
			restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
				return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
					requests = append(requests, req.URL.Path)
					resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req, Body: http.NoBody}
					var body string
					switch req.URL.Path {
					case "/api/v2/auth/login":
						resp.StatusCode = http.StatusNoContent
						resp.Header.Set("Set-Cookie", "SID=plugin-session; Path=/")
					case "/api/v2/app/version":
						body = "v5.3.0"
					case "/api/v2/app/webapiVersion":
						body = "2.16.2"
					case "/api/v2/search/plugins":
						body = `[{"name":"jackett","fullName":"Jackett","enabled":true}]`
					case "/api/v2/search/installPlugin", "/api/v2/search/enablePlugin", "/api/v2/search/uninstallPlugin", "/api/v2/search/updatePlugins":
						if req.ContentLength != 0 {
							if err := req.ParseForm(); err != nil {
								t.Fatal(err)
							}
						} else {
							gotForm = url.Values{}
						}
						if req.ContentLength != 0 {
							gotForm = req.Form
						}
					case "/api/v2/auth/logout":
						resp.StatusCode = http.StatusNoContent
					default:
						t.Fatalf("unexpected path: %s", req.URL.Path)
					}
					resp.Body = io.NopCloser(strings.NewReader(body))
					return resp, nil
				})}
			})
			t.Cleanup(restore)

			root := newRootCmd()
			root.SetArgs(append([]string{"--config", configPath, "--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "search"}, tt.args...))
			var commandErr error
			stdout, stderr := captureOutput(t, func() { commandErr = root.ExecuteContext(t.Context()) })
			if commandErr != nil || stderr != "" {
				t.Fatalf("management command returned %v, stdout %q, stderr %q", commandErr, stdout, stderr)
			}
			if tt.name == "list" {
				var plugins []qbt.SearchPlugin
				if err := json.Unmarshal([]byte(stdout), &plugins); err != nil || len(plugins) != 1 {
					t.Fatalf("list output = %q, err %v", stdout, err)
				}
			} else if !reflect.DeepEqual(gotForm, tt.form) {
				t.Fatalf("form = %v, want %v", gotForm, tt.form)
			}
			if tt.name == "uninstall" {
				if got, err := qbt.ReadDefaultSearchPlugin(configPath); err != nil || got != qbt.DefaultSearchPlugins {
					t.Fatalf("uninstall saved provider = %q, %v; want %q", got, err, qbt.DefaultSearchPlugins)
				}
			}
			if len(requests) < 4 || requests[0] != "/api/v2/auth/login" || requests[len(requests)-1] != "/api/v2/auth/logout" {
				t.Fatalf("request lifecycle = %v", requests)
			}
		})
	}
}

func TestSearchPluginManagementValidationBeforeAuthentication(t *testing.T) {
	var logins int
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/v2/auth/login" {
				logins++
			}
			return nil, io.ErrUnexpectedEOF
		})}
	})
	t.Cleanup(restore)
	for _, args := range [][]string{
		{"plugins", "install", "http://plugins.example/a.py"},
		{"plugins", "bogus"},
		{"plugins", "enable"},
	} {
		root := newRootCmd()
		root.SetArgs(append([]string{"--url", "http://qbt.test", "--user", "admin", "--pass", "secret", "search"}, args...))
		err := root.ExecuteContext(t.Context())
		if err == nil {
			t.Fatalf("args %v unexpectedly succeeded", args)
		}
		var coded *qbt.CodedError
		if !errors.As(err, &coded) || coded.Code != exitBadArgs {
			t.Fatalf("args %v error = %v, want ExitBadArgs", args, err)
		}
	}
	if logins != 0 {
		t.Fatalf("invalid management arguments triggered %d logins", logins)
	}
}
