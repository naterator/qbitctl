package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/naterator/qbitctl/internal/qbtest"
	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestCLISessionCleanup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const (
		serverURL = "http://qbt.test"
		version   = "/api/v2/app/version"
		transfer  = "/api/v2/transfer/info"
	)
	for _, tt := range []struct {
		name           string
		failPath       string
		cancelPath     string
		logoutStatus   int
		rejectLogin    bool
		wantCode       int
		wantErrMessage string
	}{
		{name: "success"},
		{name: "command failed", failPath: transfer, wantCode: exitFetchFail, wantErrMessage: "Failed to fetch transfer info: http error: 500 ({})"},
		{name: "setup failed", failPath: version, wantCode: exitFetchFail, wantErrMessage: "Failed to fetch app version: http error: 500 (v5.3.0)"},
		{name: "command canceled", cancelPath: transfer, wantCode: exitFetchFail, wantErrMessage: "context canceled"},
		{name: "setup canceled", cancelPath: version, wantCode: exitFetchFail, wantErrMessage: "context canceled"},
		{name: "logout failed", logoutStatus: http.StatusForbidden},
		{name: "command and logout failed", failPath: transfer, logoutStatus: http.StatusInternalServerError, wantCode: exitFetchFail, wantErrMessage: "Failed to fetch transfer info: http error: 500 ({})"},
		{name: "login rejected", rejectLogin: true, wantCode: exitLoginFail, wantErrMessage: "Login failed: login failed, http status: 401"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var paths []string
			restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
				return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
					path := req.URL.Path
					paths = append(paths, path)
					resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req}
					var body string
					switch path {
					case "/api/v2/auth/login":
						if tt.rejectLogin {
							resp.StatusCode = http.StatusUnauthorized
							resp.Header.Set("Set-Cookie", "proxy=route; Path=/")
						} else {
							resp.StatusCode = http.StatusNoContent
							resp.Header.Set("Set-Cookie", "QBT_SID_8080=cli-session; Path=/; HttpOnly")
						}
					case version:
						body = "v5.3.0"
					case "/api/v2/app/webapiVersion":
						body = "2.11.0"
					case transfer:
						body = "{}"
					case "/api/v2/auth/logout":
						cookie, err := req.Cookie("QBT_SID_8080")
						if req.Method != http.MethodPost || err != nil || cookie.Value != "cli-session" {
							t.Error("logout did not POST with the CLI session cookie")
						}
						deadline, ok := req.Context().Deadline()
						if !ok || time.Until(deadline) > 2*time.Second || req.Context().Err() != nil {
							t.Error("logout lacks an active, independent two-second timeout")
						}
						if tt.logoutStatus != 0 {
							resp.StatusCode = tt.logoutStatus
						}
					default:
						t.Errorf("unexpected request path %s", path)
					}
					if path == tt.cancelPath {
						cancel()
						return nil, req.Context().Err()
					}
					if path == tt.failPath {
						resp.StatusCode = http.StatusInternalServerError
					}
					resp.Body = io.NopCloser(strings.NewReader(body))
					return resp, nil
				})}
			})
			t.Cleanup(restore)

			var commandErr error
			stdout, stderr := captureOutput(t, func() {
				root := newRootCmd()
				root.SetArgs([]string{"--url", serverURL, "--user", "admin", "--pass", "secret", "info", "--server-json"})
				commandErr = root.ExecuteContext(ctx)
			})
			if tt.wantCode == exitOK {
				if commandErr != nil || stdout != "{}\n" || stderr != "" {
					t.Errorf("success returned %v, stdout %q, stderr %q", commandErr, stdout, stderr)
				}
			} else {
				var exit exitError
				if !errors.As(commandErr, &exit) || exit.code != tt.wantCode || stdout != "" ||
					!strings.HasPrefix(stderr, "[ERROR] ") || !strings.HasSuffix(stderr, tt.wantErrMessage+"\n") || strings.Count(stderr, "\n") != 1 {
					t.Errorf("failure returned %#v, stdout %q, stderr %q", commandErr, stdout, stderr)
				}
			}

			wantPaths := []string{"/api/v2/auth/login"}
			if !tt.rejectLogin {
				wantPaths = append(wantPaths, version)
				if tt.failPath != version && tt.cancelPath != version {
					wantPaths = append(wantPaths, "/api/v2/app/webapiVersion", transfer)
				}
				wantPaths = append(wantPaths, "/api/v2/auth/logout")
			}
			if !slices.Equal(paths, wantPaths) {
				t.Errorf("request paths = %v, want %v", paths, wantPaths)
			}
		})
	}
}

func TestCLISessionBurstPreservesBrowserSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const (
		limit      = 15
		burst      = 30
		cookieName = "QBT_SID_8080"
		loginPath  = "/api/v2/auth/login"
		logoutPath = "/api/v2/auth/logout"
		listPath   = "/api/v2/torrents/info"
	)
	var mu sync.Mutex
	var sessions []string // Least recently used to most recently used.
	var logins, logouts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path == loginPath {
			logins++
			sid := fmt.Sprintf("session-%d", logins)
			if len(sessions) == limit {
				sessions = sessions[1:]
			}
			sessions = append(sessions, sid)
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: sid, Path: "/", HttpOnly: true})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		cookie, err := req.Cookie(cookieName)
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		i := slices.Index(sessions, cookie.Value)
		if i < 0 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		sessions = slices.Delete(sessions, i, i+1)
		if req.URL.Path == logoutPath {
			logouts++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		sessions = append(sessions, cookie.Value)
		switch req.URL.Path {
		case "/api/v2/app/version":
			_, _ = io.WriteString(w, "v5.3.0")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.0")
		case listPath:
			_, _ = io.WriteString(w, "[]")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	browser := server.Client()
	browser.Jar = jar
	resp, err := browser.PostForm(server.URL+loginPath, url.Values{"username": {"admin"}, "password": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("browser login status = %d", resp.StatusCode)
	}

	stdout, stderr := captureOutput(t, func() {
		for i := range burst {
			root := newRootCmd()
			root.SetArgs([]string{"--url", server.URL, "--user", "admin", "--pass", "secret", "list", "--server-json"})
			if err := root.ExecuteContext(t.Context()); err != nil {
				t.Errorf("invocation %d: %v", i+1, err)
				return
			}
		}
	})
	if stdout != strings.Repeat("[]\n", burst) || stderr != "" {
		t.Errorf("burst stdout = %q, stderr = %q", stdout, stderr)
	}
	resp, err = browser.Get(server.URL + listPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("browser session status after burst = %d, want 200", resp.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	if logins != burst+1 || logouts != burst || !slices.Equal(sessions, []string{"session-1"}) {
		t.Errorf("after burst: logins=%d, logouts=%d, remaining sessions=%v", logins, logouts, sessions)
	}
}
