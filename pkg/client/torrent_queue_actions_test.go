package client

import (
	"errors"
	"testing"
)

func TestTorrentQueueActionsUseBulkWebAPIEndpoints(t *testing.T) {
	hashes := "0123456789abcdef0123456789abcdef01234567|fedcba9876543210fedcba9876543210fedcba98"
	server := newQBTestServer(t)
	app := server.newApp()

	cases := []struct {
		name   string
		path   string
		action func(string) error
	}{
		{name: "recheck", path: "/api/v2/torrents/recheck", action: app.RecheckTorrent},
		{name: "reannounce", path: "/api/v2/torrents/reannounce", action: app.ReannounceTorrent},
		{name: "queue up", path: "/api/v2/torrents/increasePrio", action: app.QueueUpTorrent},
		{name: "queue down", path: "/api/v2/torrents/decreasePrio", action: app.QueueDownTorrent},
		{name: "queue top", path: "/api/v2/torrents/topPrio", action: app.QueueTopTorrent},
		{name: "queue bottom", path: "/api/v2/torrents/bottomPrio", action: app.QueueBottomTorrent},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.action(hashes); err != nil {
				t.Fatalf("%s err = %v", tt.name, err)
			}
			forms := server.Forms[tt.path]
			if len(forms) != 1 || forms[0].Get("hashes") != hashes {
				t.Fatalf("%s forms = %#v, want one hashes=%q request", tt.name, forms, hashes)
			}
		})
	}
}

func TestRenameTorrentUsesSingularHashAndName(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	hash := "0123456789abcdef0123456789abcdef01234567"

	if err := app.RenameTorrent(hash, "Ubuntu Desktop"); err != nil {
		t.Fatalf("RenameTorrent err = %v", err)
	}
	forms := server.Forms["/api/v2/torrents/rename"]
	if len(forms) != 1 || forms[0].Get("hash") != hash || forms[0].Get("name") != "Ubuntu Desktop" {
		t.Fatalf("rename forms = %#v, want hash and name", forms)
	}
}

func TestRenameTorrentValidatesRequiredValues(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	for _, values := range [][2]string{{"", "name"}, {"hash", ""}, {" ", "  "}} {
		if err := app.RenameTorrent(values[0], values[1]); err == nil {
			t.Fatalf("RenameTorrent(%q, %q) unexpectedly succeeded", values[0], values[1])
		} else {
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
				t.Fatalf("RenameTorrent(%q, %q) err = %v, want ExitBadArgs", values[0], values[1], err)
			}
		}
	}
	if len(server.Forms["/api/v2/torrents/rename"]) != 0 {
		t.Fatal("invalid rename sent a request")
	}
}

func TestTorrentQueueActionsReturnActionFailure(t *testing.T) {
	server := newQBTestServer(t)
	server.StatusQueue["/api/v2/torrents/recheck"] = []int{500}
	app := server.newApp()

	err := app.RecheckTorrent("0123456789abcdef0123456789abcdef01234567")
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitActionFail {
		t.Fatalf("RecheckTorrent err = %v, want ExitActionFail", err)
	}
}
