package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
)

func TestHTTPErrorSanitizesAndBoundsServerBody(t *testing.T) {
	long := strings.Repeat("x", 201)
	for _, test := range []struct {
		name        string
		contentType string
		body        string
		wantMessage string
	}{
		{name: "plain text", contentType: "text/plain; charset=utf-8", body: "Cannot\nmake\t save path", wantMessage: "Cannot make save path"},
		{name: "html content type", contentType: "text/html", body: "<html><body>secret proxy page</body></html>", wantMessage: ""},
		{name: "html body", contentType: "text/plain", body: "<html>secret proxy page</html>", wantMessage: ""},
		{name: "control characters", contentType: "text/plain", body: "\x1b[31mBad\x1b[0m\nvalue", wantMessage: "[31mBad [0m value"},
		{name: "bounded", contentType: "text/plain", body: long, wantMessage: strings.Repeat("x", 200) + "…"},
	} {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: http.StatusConflict, Header: http.Header{"Content-Type": []string{test.contentType}}}
			err := newHTTPError(resp, []byte(test.body))
			if err.StatusCode != http.StatusConflict || err.Message != test.wantMessage {
				t.Fatalf("HTTPError = %#v, want status %d and message %q", err, http.StatusConflict, test.wantMessage)
			}
			if strings.ContainsAny(err.Error(), "\x00\x1b\n\r\t") {
				t.Fatalf("HTTPError contains unsafe control characters: %q", err.Error())
			}
		})
	}
}

func TestSanitizeServerTextOnlyReplacesControls(t *testing.T) {
	input := "display  name\twith  spacing"
	if got, want := SanitizeServerText(input), "display  name with  spacing"; got != want {
		t.Fatalf("SanitizeServerText = %q, want %q", got, want)
	}
}

func TestHTTPErrorIsInspectableThroughErrorsAs(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header)}
	err := newHTTPError(response, []byte("bad parameter"))
	var typed *HTTPError
	if !errors.As(error(err), &typed) || typed.StatusCode != http.StatusBadRequest {
		t.Fatalf("errors.As did not recover HTTPError: %#v", err)
	}
	if !isHTTPStatus(err, http.StatusBadRequest) || isHTTPStatus(err, http.StatusNotFound) {
		t.Fatal("HTTP status matcher returned the wrong result")
	}
}

func TestHTTPErrorWithEmptyBodyKeepsStatusOnlyMessage(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header)}
	if got := newHTTPError(response, nil).Error(); got != "http error: 500" {
		t.Fatalf("empty-body HTTPError = %q, want status-only message", got)
	}
}

func TestRequestContextIncludesHTTPErrorDetails(t *testing.T) {
	c := newClient(Credentials{URL: "http://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusConflict,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("Cannot make save path")),
			Request:    req,
		}, nil
	})
	_, err := c.requestContext(context.Background(), http.MethodPost, "/api/v2/torrents/setLocation", url.Values{"hashes": {"abc"}})
	if err == nil || !strings.Contains(err.Error(), "http error: 409 (Cannot make save path)") {
		t.Fatalf("request error = %v, want server detail", err)
	}
}

func TestPublicActionRetainsHTTPErrorAndExitCode(t *testing.T) {
	c := newClient(Credentials{URL: "http://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusConflict,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("Cannot make save path")),
			Request:    req,
		}, nil
	})
	app := &App{creds: c.creds, client: c, Ctx: context.Background()}
	err := app.MoveTorrent("abc", "/downloads")
	var coded *CodedError
	var httpErr *HTTPError
	if !errors.As(err, &coded) || coded.Code != ExitActionFail {
		t.Fatalf("MoveTorrent error = %v, want ExitActionFail CodedError", err)
	}
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict {
		t.Fatalf("MoveTorrent error = %v, want underlying HTTPError", err)
	}
}

func TestRequestBodyContextDropsHTMLDetails(t *testing.T) {
	c := newClient(Credentials{URL: "http://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader("<html>upstream details</html>")),
			Request:    req,
		}, nil
	})
	_, err := c.requestBodyContext(context.Background(), http.MethodPost, "/api/v2/torrents/add", []byte("hashes=abc"), "application/x-www-form-urlencoded")
	if err == nil || err.Error() != "http error: 502" {
		t.Fatalf("request body error = %v, want empty HTML detail", err)
	}
}

func TestMultipartUploadIncludesHTTPErrorDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.torrent")
	if err := os.WriteFile(path, []byte("torrent fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newQBTestServer(t)
	app := server.newApp()
	app.client.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnsupportedMediaType,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("torrent upload rejected")),
			Request:    req,
		}, nil
	})
	err := app.AddTorrent(path)
	if err == nil || !strings.Contains(err.Error(), "http error: 415 (torrent upload rejected)") {
		t.Fatalf("multipart upload error = %v, want server detail", err)
	}
}

func TestLoginIncludesSanitizedBanMessage(t *testing.T) {
	c := newClient(Credentials{URL: "http://qbt.test", User: "admin", Pass: "secret"}, io.Discard)
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("Your IP address is\n banned\x1b[31m")),
			Request:    req,
		}, nil
	})
	err := c.loginContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "login failed, http status: 403 (Your IP address is banned [31m)") {
		t.Fatalf("login error = %v, want sanitized ban message", err)
	}
	if strings.ContainsAny(err.Error(), "\x1b\n\r\t") {
		t.Fatalf("login error contains unsafe control characters: %q", err.Error())
	}
}

func TestLoginRedactsPasswordEchoes(t *testing.T) {
	password := "s3cret!"
	c := newClient(Credentials{URL: "http://qbt.test", User: "admin", Pass: password}, io.Discard)
	c.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"message":"banned","password":"s3cret!","encoded":"s3cret%21"}`)),
			Request:    req,
		}, nil
	})
	err := c.loginContext(context.Background())
	if err == nil || strings.Contains(err.Error(), password) || strings.Contains(err.Error(), "s3cret%21") {
		t.Fatalf("login error leaked a password echo: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("login error = %v, want redaction marker", err)
	}
}

func TestSetTagsLegacyFallbackStillRecognizes404Body(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hash, Tags: "old"}}
	server.StatusQueue["/api/v2/torrents/setTags"] = []int{http.StatusNotFound}
	app := server.newApp()
	original := app.client.httpClient.Transport
	app.client.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := original.RoundTrip(req)
		if err == nil && req.URL.Path == "/api/v2/torrents/setTags" {
			resp.Body = io.NopCloser(strings.NewReader("Not Found"))
		}
		return resp, err
	})
	if err := app.SetTags(hash, "new"); err != nil {
		t.Fatalf("legacy setTags fallback returned error: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/addTags"][0].Get("tags"); got != "new" {
		t.Fatalf("legacy addTags = %q, want new", got)
	}
}

func TestDeleteSearch404DoesNotWarn(t *testing.T) {
	server := newQBTestServer(t)
	server.StatusQueue["/api/v2/search/delete"] = []int{http.StatusNotFound}
	app := server.newApp()
	var stderr bytes.Buffer
	app.Stderr = &stderr
	app.client.stderr = &stderr
	app.deleteSearch(url.Values{"id": {"42"}})
	if stderr.Len() != 0 {
		t.Fatalf("deleteSearch warning = %q, want none for 404", stderr.String())
	}
}
