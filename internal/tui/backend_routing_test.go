//go:build !plan9

package tui

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
	"github.com/naterator/qbitctl/pkg/client"
)

func newBackendFixture(t *testing.T) (*tuiClient, *qbtest.Server) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := qbtest.New(t)
	server.Torrents = []client.TorrentInfo{{Hash: "a", Name: "Alpha", State: "downloading"}}
	restore := client.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{
			Jar: jar,
			Transport: qbtest.RoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return server.RoundTrip(request), nil
			}),
		}
	})
	t.Cleanup(restore)
	app, err := client.NewClientContext(context.Background(), &client.Options{
		URL:  server.Creds().URL,
		User: server.Creds().User,
		Pass: server.Creds().Pass,
	})
	if err != nil {
		t.Fatalf("NewClientContext: %v", err)
	}
	t.Cleanup(app.Logout)
	return &tuiClient{app: app}, server
}

func TestTUIClientRoutesTorrentActions(t *testing.T) {
	backend, server := newBackendFixture(t)
	ctx := context.Background()
	tests := []struct {
		name, kind, path string
		request          actionRequest
		want             map[string]string
	}{
		{name: "start", kind: "start", path: "/api/v2/torrents/start", request: actionRequest{kind: "start", hashes: "a|b"}, want: map[string]string{"hashes": "a|b"}},
		{name: "pause", kind: "pause", path: "/api/v2/torrents/stop", request: actionRequest{kind: "pause", hashes: "a"}, want: map[string]string{"hashes": "a"}},
		{name: "force start", kind: "force-start", path: "/api/v2/torrents/setForceStart", request: actionRequest{kind: "force-start", hashes: "a", value: "ignored"}, want: map[string]string{"hashes": "a", "value": "true"}},
		{name: "move", kind: "move", path: "/api/v2/torrents/setLocation", request: actionRequest{kind: "move", hashes: "a", value: "/data/x"}, want: map[string]string{"hashes": "a", "location": "/data/x"}},
		{name: "remove", kind: "remove", path: "/api/v2/torrents/delete", request: actionRequest{kind: "remove", hashes: "a"}, want: map[string]string{"hashes": "a", "deleteFiles": "false"}},
		{name: "delete", kind: "delete", path: "/api/v2/torrents/delete", request: actionRequest{kind: "delete", hashes: "a"}, want: map[string]string{"hashes": "a", "deleteFiles": "true"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := backend.action(ctx, test.request); err != nil {
				t.Fatalf("action returned error: %v", err)
			}
			forms := server.Forms[test.path]
			if len(forms) == 0 {
				t.Fatalf("no form recorded for %s", test.path)
			}
			form := forms[len(forms)-1]
			for key, want := range test.want {
				if got := form.Get(key); got != want {
					t.Fatalf("%s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

func TestTUIClientAddExpandsHomeAndRejectsUnknownAction(t *testing.T) {
	backend, server := newBackendFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "demo.torrent"), []byte("torrent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := backend.action(context.Background(), actionRequest{
		kind: "add", value: "~/demo.torrent",
		addOptions: client.AddTorrentOptions{Category: "linux", SavePath: "/downloads", Stopped: true},
	}); err != nil {
		t.Fatalf("add action returned error: %v", err)
	}
	if len(server.AddFiles) != 1 || server.AddFiles[0].Filename != "demo.torrent" || string(server.AddFiles[0].Content) != "torrent" {
		t.Fatalf("uploaded files = %#v", server.AddFiles)
	}
	form := server.Forms["/api/v2/torrents/add"]
	if len(form) != 1 || form[0].Get("category") != "linux" || form[0].Get("savepath") != "/downloads" || form[0].Get("stopped") != "true" {
		t.Fatalf("add form = %#v", form)
	}
	if err := backend.action(context.Background(), actionRequest{kind: "unknown"}); err == nil {
		t.Fatal("unknown action unexpectedly succeeded")
	}
}

func TestTUIClientQueueAndServer(t *testing.T) {
	backend, server := newBackendFixture(t)
	server.Torrents = []client.TorrentInfo{
		{Hash: "b", Name: "Beta", State: "seeding"},
		{Hash: "a", Name: "Alpha", State: "downloading"},
	}
	server.TransferInfo = &qbtest.TransferInfo{ConnectionStatus: "connected", DHTNodes: 7}
	queue, err := backend.queue(context.Background())
	if err != nil || len(queue) != 2 || queue[0].Hash != "b" || queue[1].Hash != "a" {
		t.Fatalf("queue = %#v, error = %v", queue, err)
	}
	rows, err := backend.server(context.Background())
	if err != nil {
		t.Fatalf("server = %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("server returned no rows")
	}
	if rows[0].label != "qBittorrent" || rows[1].label != "WebAPI" || rows[2].label != "Connection" {
		t.Fatalf("server rows = %#v", rows[:min(3, len(rows))])
	}
}

func TestTUIClientDetailsWithAndWithoutTrackers(t *testing.T) {
	backend, server := newBackendFixture(t)
	server.InfoRawBody = `[{"name":"Alpha","hash":"a","category":"linux","state":"downloading"}]`
	rows, err := backend.details(context.Background(), "a", false)
	if err != nil {
		t.Fatalf("torrent details = %v", err)
	}
	if len(rows) == 0 || rows[0].label != "Name" || rows[0].value != "Alpha" {
		t.Fatalf("torrent detail rows = %#v", rows)
	}

	server.Trackers = []client.TrackerEntry{
		{URL: "https://tracker.example/announce?token=a,b"},
		{URL: "** [DHT] **"},
		{URL: "udp://tracker.example:6969"},
	}
	rows, err = backend.details(context.Background(), "a", true)
	if err != nil {
		t.Fatalf("tracker details = %v", err)
	}
	if len(rows) != 2 || rows[0].value != "https://tracker.example/announce?token=a,b" || rows[1].value != "udp://tracker.example:6969" {
		t.Fatalf("tracker detail rows = %#v", rows)
	}
}

func TestTUIClientRejectsMissingConfiguration(t *testing.T) {
	var backend *tuiClient
	if _, err := backend.queue(context.Background()); err == nil {
		t.Fatal("nil queue client unexpectedly succeeded")
	}
	if _, _, err := backend.search(context.Background(), "query", "jackett", 0); err == nil {
		t.Fatal("nil search client unexpectedly succeeded")
	}
	if _, err := backend.details(context.Background(), "a", false); err == nil {
		t.Fatal("nil details client unexpectedly succeeded")
	}
	if _, err := backend.server(context.Background()); err == nil {
		t.Fatal("nil server client unexpectedly succeeded")
	}
	if err := backend.action(context.Background(), actionRequest{kind: "start", hashes: "a"}); err == nil {
		t.Fatal("nil action client unexpectedly succeeded")
	}
}
