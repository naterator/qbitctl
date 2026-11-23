package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/naterator/qbitctl/internal/qbtest"
)

func TestAddTorrentVariants(t *testing.T) {
	t.Run("magnet link", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()
		magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Ubuntu"

		if err := app.AddTorrent(magnet); err != nil {
			t.Fatalf("AddTorrent err = %v", err)
		}
		if got := server.Forms["/api/v2/torrents/add"][0].Get("urls"); got != magnet {
			t.Fatalf("multipart urls = %q, want %q", got, magnet)
		}
	})

	t.Run("torrent file", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()
		path := filepath.Join(t.TempDir(), "ubuntu.torrent")
		if err := os.WriteFile(path, []byte("dummy torrent payload"), 0o600); err != nil {
			t.Fatalf("write torrent file failed: %v", err)
		}

		if err := app.AddTorrent(path); err != nil {
			t.Fatalf("AddTorrent err = %v", err)
		}
		if len(server.AddFiles) != 1 {
			t.Fatalf("addFiles = %d, want 1", len(server.AddFiles))
		}
		if server.AddFiles[0].Filename != "ubuntu.torrent" {
			t.Fatalf("uploaded filename = %q", server.AddFiles[0].Filename)
		}
		if string(server.AddFiles[0].Content) != "dummy torrent payload" {
			t.Fatalf("uploaded content = %q", string(server.AddFiles[0].Content))
		}
	})

	t.Run("add options", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()
		magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
		options := AddTorrentOptions{Category: "linux", SavePath: "/downloads/linux", Tags: "iso,linux", Stopped: true}

		if err := app.AddTorrentWithOptions(magnet, options); err != nil {
			t.Fatalf("AddTorrentWithOptions err = %v", err)
		}
		form := server.Forms["/api/v2/torrents/add"][0]
		for field, want := range map[string]string{
			"urls":     magnet,
			"category": "linux",
			"savepath": "/downloads/linux",
			"autoTMM":  "false",
			"tags":     "iso,linux",
			"stopped":  "true",
		} {
			if got := form.Get(field); got != want {
				t.Fatalf("add form %s = %q, want %q", field, got, want)
			}
		}
	})

	t.Run("file add options", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()
		path := filepath.Join(t.TempDir(), "ubuntu.torrent")
		if err := os.WriteFile(path, []byte("dummy torrent payload"), 0o600); err != nil {
			t.Fatalf("write torrent file failed: %v", err)
		}
		options := AddTorrentOptions{Category: "linux", SavePath: "/downloads/linux", Tags: "iso,linux", Stopped: true}

		if err := app.AddTorrentWithOptions(path, options); err != nil {
			t.Fatalf("AddTorrentWithOptions err = %v", err)
		}
		if len(server.AddFiles) != 1 || string(server.AddFiles[0].Content) != "dummy torrent payload" {
			t.Fatalf("uploaded files = %#v", server.AddFiles)
		}
		form := server.Forms["/api/v2/torrents/add"][0]
		for field, want := range map[string]string{
			"category": "linux",
			"savepath": "/downloads/linux",
			"autoTMM":  "false",
			"tags":     "iso,linux",
			"stopped":  "true",
		} {
			if got := form.Get(field); got != want {
				t.Fatalf("file add form %s = %q, want %q", field, got, want)
			}
		}
	})

	t.Run("missing file", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()

		err := app.AddTorrent(filepath.Join(t.TempDir(), "missing.torrent"))
		if err == nil {
			t.Fatal("AddTorrent unexpectedly succeeded")
		} else {
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitFile {
				t.Fatalf("AddTorrent err code = %v, want %d", err, ExitFile)
			}
		}
	})
}

func TestAddTorrentRejectsOversizedFile(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	path := filepath.Join(t.TempDir(), "oversized.torrent")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("create torrent file failed: %v", err)
	}
	if err := os.Truncate(path, maxTorrentFileSize+1); err != nil {
		t.Fatalf("create oversized torrent file failed: %v", err)
	}

	err := app.AddTorrent(path)
	if err == nil {
		t.Fatal("AddTorrent unexpectedly accepted an oversized file")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitFile {
		t.Fatalf("AddTorrent err = %v, want ExitFile", err)
	}
	if len(server.AddFiles) != 0 {
		t.Fatalf("uploaded files = %d, want 0", len(server.AddFiles))
	}
}

func TestAddTorrentRejectsNonRegularPath(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	path := filepath.Join(t.TempDir(), "torrent-directory")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create torrent directory failed: %v", err)
	}

	err := app.AddTorrent(path)
	if err == nil {
		t.Fatal("AddTorrent unexpectedly accepted a non-regular path")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitFile {
		t.Fatalf("AddTorrent err = %v, want ExitFile", err)
	}
	if len(server.AddFiles) != 0 {
		t.Fatalf("uploaded files = %d, want 0", len(server.AddFiles))
	}
}

func TestAddTorrentEmptySource(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()

	err := app.AddTorrent("")
	if err == nil {
		t.Fatal("AddTorrent unexpectedly succeeded with empty source")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
		t.Fatalf("AddTorrent err = %v, want ExitBadArgs", err)
	}
}

func TestAddTorrentReportsLegacyRejectedResponse(t *testing.T) {
	tests := []struct {
		name   string
		source string
		file   bool
	}{
		{name: "magnet link", source: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"},
		{name: "URL", source: "https://example.test/ubuntu.torrent"},
		{name: "torrent file", source: "ubuntu.torrent", file: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newQBTestServer(t)
			app := server.newApp()
			transport := app.client.httpClient.Transport
			app.client.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				response, err := transport.RoundTrip(req)
				if err == nil && req.URL.Path == "/api/v2/torrents/add" {
					response.Body = io.NopCloser(strings.NewReader("  fails.\n"))
				}
				return response, err
			})

			source := tt.source
			if tt.file {
				source = filepath.Join(t.TempDir(), tt.source)
				if err := os.WriteFile(source, []byte("dummy torrent payload"), 0o600); err != nil {
					t.Fatalf("write torrent file failed: %v", err)
				}
			}

			err := app.AddTorrent(source)
			if err == nil {
				t.Fatal("AddTorrent unexpectedly succeeded for a rejected response")
			}
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitActionFail {
				t.Fatalf("AddTorrent err = %v, want ExitActionFail", err)
			}
			if !strings.Contains(err.Error(), "rejected") {
				t.Fatalf("AddTorrent err = %v, want rejection detail", err)
			}
		})
	}
}

func TestAddTorrentFileRetriesAfterForbidden(t *testing.T) {
	server := newQBTestServer(t)
	server.StatusQueue["/api/v2/torrents/add"] = []int{http.StatusForbidden, http.StatusOK}
	app := server.newApp()
	path := filepath.Join(t.TempDir(), "ubuntu.torrent")
	if err := os.WriteFile(path, []byte("dummy torrent payload"), 0o600); err != nil {
		t.Fatalf("write torrent file failed: %v", err)
	}

	if err := app.AddTorrent(path); err != nil {
		t.Fatalf("AddTorrent err = %v", err)
	}
	if server.LoginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", server.LoginCalls)
	}
	if len(server.AddFiles) != 1 || string(server.AddFiles[0].Content) != "dummy torrent payload" {
		t.Fatalf("uploaded files = %#v, want one retried upload", server.AddFiles)
	}
}

func TestAddTorrentFileCancelsBeforeServerReadsBody(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.Ctx = ctx
	started := make(chan struct{})
	app.client.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	path := filepath.Join(t.TempDir(), "ubuntu.torrent")
	if err := os.WriteFile(path, []byte("dummy torrent payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- app.AddTorrent(path) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not reach the transport")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("canceled upload error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled upload remained blocked")
	}
}

func TestMoveTorrentEmptyPath(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	fullHash := "0123456789abcdef0123456789abcdef01234567"

	err := app.MoveTorrent(fullHash, "")
	if err == nil {
		t.Fatal("MoveTorrent unexpectedly succeeded with empty path")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
		t.Fatalf("MoveTorrent err = %v, want ExitBadArgs", err)
	}
}

func TestPauseTorrentHTTPError(t *testing.T) {
	server := newQBTestServer(t)
	server.StatusQueue["/api/v2/torrents/stop"] = []int{http.StatusInternalServerError}
	app := server.newApp()
	fullHash := "0123456789abcdef0123456789abcdef01234567"

	err := app.PauseTorrent(fullHash)
	if err == nil {
		t.Fatal("PauseTorrent unexpectedly succeeded on HTTP error")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitActionFail {
		t.Fatalf("PauseTorrent err = %v, want ExitActionFail", err)
	}
}

func TestActionSequencingAndForms(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	app := server.newApp()

	if err := app.StartTorrent(fullHash); err != nil {
		t.Fatalf("StartTorrent err = %v", err)
	}
	if err := app.ForceStartTorrent(fullHash); err != nil {
		t.Fatalf("ForceStartTorrent err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setForceStart"][0].Get("value"); got != "true" {
		t.Fatalf("force-start value = %q, want true", got)
	}
	if err := app.MoveTorrent(fullHash, "/downloads/linux"); err != nil {
		t.Fatalf("MoveTorrent err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setLocation"][0].Get("location"); got != "/downloads/linux" {
		t.Fatalf("move location = %q, want %q", got, "/downloads/linux")
	}
	if err := app.StopAndRemoveTorrent(fullHash, true); err != nil {
		t.Fatalf("StopAndRemoveTorrent err = %v", err)
	}
	wantOrder := []string{
		"/api/v2/torrents/start",
		"/api/v2/torrents/setForceStart",
		"/api/v2/torrents/setLocation",
		"/api/v2/torrents/delete",
	}
	if strings.Join(server.CallOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("callOrder = %v, want %v", server.CallOrder, wantOrder)
	}
	if got := server.Forms["/api/v2/torrents/delete"][0].Get("deleteFiles"); got != "true" {
		t.Fatalf("deleteFiles = %q, want true", got)
	}
}
