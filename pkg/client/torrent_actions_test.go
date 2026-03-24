package client

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddTorrentVariants(t *testing.T) {
	t.Run("magnet link", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()
		magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Ubuntu"

		if err := app.AddTorrent(magnet); err != nil {
			t.Fatalf("AddTorrent err = %v", err)
		}
		if got := server.forms["/api/v2/torrents/add"][0].Get("urls"); got != magnet {
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
		if len(server.addFiles) != 1 {
			t.Fatalf("addFiles = %d, want 1", len(server.addFiles))
		}
		if server.addFiles[0].Filename != "ubuntu.torrent" {
			t.Fatalf("uploaded filename = %q", server.addFiles[0].Filename)
		}
		if string(server.addFiles[0].Content) != "dummy torrent payload" {
			t.Fatalf("uploaded content = %q", string(server.addFiles[0].Content))
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
	server.statusQueue["/api/v2/torrents/stop"] = []int{http.StatusInternalServerError}
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
	if got := server.forms["/api/v2/torrents/setForceStart"][0].Get("value"); got != "true" {
		t.Fatalf("force-start value = %q, want true", got)
	}
	if err := app.MoveTorrent(fullHash, "/downloads/linux"); err != nil {
		t.Fatalf("MoveTorrent err = %v", err)
	}
	if got := server.forms["/api/v2/torrents/setLocation"][0].Get("location"); got != "/downloads/linux" {
		t.Fatalf("move location = %q, want %q", got, "/downloads/linux")
	}
	if err := app.StopAndRemoveTorrent(fullHash, true); err != nil {
		t.Fatalf("StopAndRemoveTorrent err = %v", err)
	}
	wantOrder := []string{
		"/api/v2/torrents/start",
		"/api/v2/torrents/setForceStart",
		"/api/v2/torrents/setLocation",
		"/api/v2/torrents/stop",
		"/api/v2/torrents/delete",
	}
	if strings.Join(server.callOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("callOrder = %v, want %v", server.callOrder, wantOrder)
	}
	if got := server.forms["/api/v2/torrents/delete"][0].Get("deleteFiles"); got != "true" {
		t.Fatalf("deleteFiles = %q, want true", got)
	}
}
