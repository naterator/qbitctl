package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
)

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) { return 0, errors.New("body read failed") }
func (failingReadCloser) Close() error             { return nil }

func TestClientLoginResponses(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		wantOK bool
	}{
		{"legacy success", http.StatusOK, "Ok.", true},
		{"modern success", http.StatusNoContent, "", true},
		{"legacy invalid credentials", http.StatusOK, "Fails.", false},
		{"legacy failure whitespace", http.StatusOK, " fails.\n", false},
		{"legacy unexpected body", http.StatusOK, "welcome", false},
		{"modern invalid credentials", http.StatusUnauthorized, "", false},
		{"banned", http.StatusForbidden, "", false},
		{"server error", http.StatusInternalServerError, "", false},
		{"unexpected success status", http.StatusAccepted, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newClient(Credentials{URL: "https://qbt.test", User: "nate", Pass: "a&b+c= d"}, io.Discard)
			c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.Path != "/api/v2/auth/login" {
					t.Fatalf("unexpected login request: %s %s", req.Method, req.URL)
				}
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if req.Form.Get("username") != c.creds.User || req.Form.Get("password") != c.creds.Pass {
					t.Fatal("login form did not preserve credentials")
				}
				resp := &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header)}
				if tt.wantOK {
					resp.Header.Set("Set-Cookie", "SID=test-session; Path=/")
				}
				return resp, nil
			})
			if err := c.loginContext(context.Background()); (err == nil) != tt.wantOK {
				t.Fatalf("login error = %v, want success %t", err, tt.wantOK)
			}
		})
	}
}

func TestClientSessionCookiesAndRenewal(t *testing.T) {
	for _, tt := range []struct {
		cookie string
		status int
		body   string
	}{
		{"SID", http.StatusOK, "Ok."},
		{"QBT_SID_8080", http.StatusNoContent, ""},
		{"custom-session", http.StatusOK, "Ok."},
	} {
		t.Run(tt.cookie, func(t *testing.T) {
			c := newClient(Credentials{URL: "https://qbt.test:8443/qbt/", User: "nate", Pass: "secret"}, io.Discard)
			logins := 0
			loggedOut := false
			attempts := map[string]int{}
			c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Origin") != "https://qbt.test:8443" || req.Header.Get("Referer") != "https://qbt.test:8443/" {
					t.Fatalf("request missing matching origin headers: %v", req.Header)
				}
				resp := &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}
				if req.URL.Path == "/qbt/api/v2/auth/login" {
					logins++
					resp.StatusCode = tt.status
					resp.Body = io.NopCloser(strings.NewReader(tt.body))
					resp.Header.Add("Set-Cookie", fmt.Sprintf("%s=session-%d; Path=/; Secure; HttpOnly", tt.cookie, logins))
					return resp, nil
				}
				cookie, err := req.Cookie(tt.cookie)
				if err != nil || cookie.Value != fmt.Sprintf("session-%d", logins) {
					t.Fatalf("request did not carry current %s cookie", tt.cookie)
				}
				if req.URL.Path == "/qbt/api/v2/auth/logout" {
					if req.Method != http.MethodPost {
						t.Fatalf("logout method = %s, want POST", req.Method)
					}
					loggedOut = true
					return resp, nil
				}
				attempts[req.URL.Path]++
				// Each endpoint expires the session once. Both request paths must
				// renew the cookie and replay the original form or upload body.
				if attempts[req.URL.Path] == 1 {
					resp.StatusCode = http.StatusForbidden
				}
				body, err := io.ReadAll(req.Body)
				if err != nil || string(body) != "hashes=abc" {
					t.Fatalf("request body = %q, error = %v", body, err)
				}
				return resp, nil
			})
			if err := c.loginContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := c.requestContext(context.Background(), http.MethodPost, "/api/v2/torrents/setTags", url.Values{"hashes": {"abc"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.requestBodyContext(context.Background(), http.MethodPost, "/api/v2/torrents/add", []byte("hashes=abc"), "application/x-www-form-urlencoded"); err != nil {
				t.Fatal(err)
			}
			c.logout()
			if !loggedOut {
				t.Fatal("client skipped logout after renewal")
			}
			if logins != 3 {
				t.Fatalf("login calls = %d, want initial login and two renewals", logins)
			}
		})
	}
}

func TestClientLoginFailureOnNon200(t *testing.T) {
	server := newQBTestServer(t)
	server.StatusQueue["/api/v2/auth/login"] = []int{http.StatusUnauthorized}
	withQBServerClientFactory(t, server)

	err := newClient(server.creds(), os.Stderr).loginContext(context.Background())
	if err == nil {
		t.Fatal("login unexpectedly succeeded")
	}
}

func TestClientLoginRejectsMissingSessionCookie(t *testing.T) {
	c := newClient(Credentials{URL: "https://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req, Body: http.NoBody}
		switch req.URL.Path {
		case loginEndpoint:
			resp.Body = io.NopCloser(strings.NewReader("Ok."))
		case "/api/v2/app/version":
			resp.StatusCode = http.StatusForbidden
		default:
			t.Fatalf("unexpected request path %s", req.URL.Path)
		}
		return resp, nil
	})

	err := c.loginContext(t.Context())
	if err == nil || !strings.Contains(err.Error(), "did not issue a session cookie") {
		t.Fatalf("login error = %v, want missing-cookie error", err)
	}
}

func TestClientLoginAcceptsVerifiedAuthBypassWithoutCleanup(t *testing.T) {
	c := newClient(Credentials{URL: "https://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	var paths []string
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req, Body: http.NoBody}
		switch req.URL.Path {
		case loginEndpoint:
			resp.Body = io.NopCloser(strings.NewReader("Ok."))
		case "/api/v2/app/version":
			resp.Body = io.NopCloser(strings.NewReader("v5.3.0"))
		default:
			t.Fatalf("unexpected request path %s", req.URL.Path)
		}
		return resp, nil
	})

	if err := c.loginContext(t.Context()); err != nil {
		t.Fatalf("login returned error: %v", err)
	}
	c.logout()
	if !slices.Equal(paths, []string{loginEndpoint, "/api/v2/app/version"}) {
		t.Fatalf("request paths = %v, want verified login without logout", paths)
	}
}

func TestClientLoginRejectsUnparseableAuthBypassVersion(t *testing.T) {
	c := newClient(Credentials{URL: "https://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req, Body: http.NoBody}
		switch req.URL.Path {
		case loginEndpoint:
			resp.Body = io.NopCloser(strings.NewReader("Ok."))
		case "/api/v2/app/version":
			resp.Body = io.NopCloser(strings.NewReader("Welcome to qBittorrent"))
		default:
			t.Fatalf("unexpected request path %s", req.URL.Path)
		}
		return resp, nil
	})

	err := c.loginContext(t.Context())
	if err == nil || !strings.Contains(err.Error(), "invalid qBittorrent version") {
		t.Fatalf("login error = %v, want invalid-version error", err)
	}
}

func TestClientLoginBodyReadFailureStillCleansUpIssuedSession(t *testing.T) {
	c := newClient(Credentials{URL: "https://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	loggedOut := false
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Request: req, Body: http.NoBody}
		switch req.URL.Path {
		case loginEndpoint:
			resp.Header.Set("Set-Cookie", "custom-session=issued; Path=/")
			resp.Body = failingReadCloser{}
		case logoutEndpoint:
			cookie, err := req.Cookie("custom-session")
			if err != nil || cookie.Value != "issued" {
				t.Fatalf("logout cookie = %v, want issued custom session", err)
			}
			loggedOut = true
		default:
			t.Fatalf("unexpected request path %s", req.URL.Path)
		}
		return resp, nil
	})

	if err := c.loginContext(t.Context()); err == nil || !strings.Contains(err.Error(), "body read failed") {
		t.Fatalf("login error = %v, want body-read failure", err)
	}
	c.logout()
	if !loggedOut {
		t.Fatal("client did not clean up session after body read failure")
	}
}

func TestClientLogoutAfterLoginRedirect(t *testing.T) {
	c := newClient(Credentials{URL: "https://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	var loggedOut bool
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}
		switch req.URL.String() {
		case "https://qbt.test" + loginEndpoint:
			resp.StatusCode = http.StatusTemporaryRedirect
			resp.Header.Set("Location", "https://qbt.test/qbt"+loginEndpoint)
		case "https://qbt.test/qbt" + loginEndpoint:
			resp.Header.Set("Set-Cookie", "QBT_SID_8080=redirected-session; Path=/qbt; Secure; HttpOnly")
		case "https://qbt.test/qbt" + logoutEndpoint:
			cookie, err := req.Cookie("QBT_SID_8080")
			if err != nil || cookie.Value != "redirected-session" {
				t.Fatal("logout did not carry the secure session cookie")
			}
			loggedOut = true
		default:
			t.Fatalf("unexpected request to %s", req.URL)
		}
		return resp, nil
	})
	if err := c.loginContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.logout()
	if !loggedOut {
		t.Fatal("client skipped logout after redirect")
	}
}

func TestClientLoginRejectsCrossOrigin307Redirect(t *testing.T) {
	c := newClient(Credentials{URL: "https://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	var paths []string
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.String())
		if req.URL.String() == "https://qbt.test"+loginEndpoint {
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header:     http.Header{"Location": []string{"https://attacker.test" + loginEndpoint}},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		t.Fatalf("cross-origin redirect received login request: %s", req.URL)
		return nil, nil
	})

	if err := c.loginContext(t.Context()); err == nil || !strings.Contains(err.Error(), "cross-origin login redirect") {
		t.Fatalf("login error = %v, want cross-origin redirect error", err)
	}
	if !slices.Equal(paths, []string{"https://qbt.test" + loginEndpoint}) {
		t.Fatalf("request paths = %v, want only original login request", paths)
	}
}

func TestClientRequestReloginOnForbidden(t *testing.T) {
	server := newQBTestServer(t)
	server.StatusQueue["/api/v2/torrents/info"] = []int{http.StatusForbidden}
	server.Torrents = []TorrentInfo{{Name: "Ubuntu", Hash: "0123456789abcdef0123456789abcdef01234567"}}
	withQBServerClientFactory(t, server)

	client := newClient(server.creds(), os.Stderr)
	body, err := client.requestContext(context.Background(), http.MethodGet, "/api/v2/torrents/info", nil)
	if err != nil {
		t.Fatalf("request returned error: %v", err)
	}
	if server.LoginCalls != 1 {
		t.Fatalf("loginCalls = %d, want 1", server.LoginCalls)
	}
	if !strings.Contains(string(body), "Ubuntu") {
		t.Fatalf("request body = %q, want torrent JSON", string(body))
	}
}
