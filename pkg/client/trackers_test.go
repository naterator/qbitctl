package client

import (
	"errors"
	"testing"
)

func TestTrackerDisplayHelpers(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		want   string
	}{
		{name: "disabled", status: 0, want: "Disabled"},
		{name: "not contacted", status: 1, want: "Not contacted"},
		{name: "working", status: 2, want: "Working"},
		{name: "updating", status: 3, want: "Updating"},
		{name: "not working", status: 4, want: "Not working"},
		{name: "unknown", status: 9, want: "Unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := TrackerStatusLabel(test.status); got != test.want {
				t.Fatalf("TrackerStatusLabel(%d) = %q, want %q", test.status, got, test.want)
			}
		})
	}

	for _, test := range []struct {
		name string
		url  string
		want string
	}{
		{name: "host strips credentials and path", url: "https://user:pass@Tracker.Example:443/announce?passkey=secret", want: "Tracker.Example"},
		{name: "udp host", url: "udp://tracker.example:6969/announce", want: "tracker.example"},
		{name: "dht", url: "** [DHT] **", want: "DHT"},
		{name: "pex", url: "** [PeX] **", want: "PeX"},
		{name: "lsd", url: "** [LSD] **", want: "LSD"},
		{name: "malformed", url: "not a URL", want: "Unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := TrackerHost(test.url); got != test.want {
				t.Fatalf("TrackerHost(%q) = %q, want %q", test.url, got, test.want)
			}
		})
	}
}

func TestTrackerMutationsUseExactAPIParameters(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	app := server.newApp()

	if err := app.AddTrackers(hash, "https://one.example/announce\nudp://two.example:6969/announce"); err != nil {
		t.Fatalf("AddTrackers: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/addTrackers"][0]; got.Get("hash") != hash || got.Get("urls") != "https://one.example/announce\nudp://two.example:6969/announce" {
		t.Fatalf("addTrackers form = %#v", got)
	}

	if err := app.EditTracker(hash, "https://one.example/announce?token=a,b", "https://new.example/announce?token=x&part=2"); err != nil {
		t.Fatalf("EditTracker: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/editTracker"][0]; got.Get("hash") != hash || got.Get("origUrl") != "https://one.example/announce?token=a,b" || got.Get("url") != "https://one.example/announce?token=a,b" || got.Get("newUrl") != "https://new.example/announce?token=x&part=2" {
		t.Fatalf("editTracker form = %#v", got)
	}

	if err := app.RemoveTrackers(hash, "https://one.example/announce?token=a,b|udp://two.example:6969/announce"); err != nil {
		t.Fatalf("RemoveTrackers: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/removeTrackers"][0]; got.Get("hash") != hash || got.Get("urls") != "https://one.example/announce?token=a,b|udp://two.example:6969/announce" {
		t.Fatalf("removeTrackers form = %#v", got)
	}
}

func TestTrackerMutationsRejectInvalidURLsBeforeRequest(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	app := server.newApp()

	for _, test := range []struct {
		name string
		call func() error
	}{
		{name: "empty add", call: func() error { return app.AddTrackers(hash, "") }},
		{name: "malformed add", call: func() error { return app.AddTrackers(hash, "not-a-url") }},
		{name: "empty edit source", call: func() error { return app.EditTracker(hash, "", "https://new.example/announce") }},
		{name: "malformed edit target", call: func() error { return app.EditTracker(hash, "https://old.example/announce", "https://") }},
		{name: "empty remove", call: func() error { return app.RemoveTrackers(hash, "") }},
		{name: "newline remove", call: func() error {
			return app.RemoveTrackers(hash, "https://one.example/announce\nhttps://two.example/announce")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if err == nil {
				t.Fatal("mutation unexpectedly succeeded")
			}
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
				t.Fatalf("error = %v, want ExitBadArgs", err)
			}
		})
	}
	if len(server.Forms) != 0 {
		t.Fatalf("invalid mutations sent requests: %#v", server.Forms)
	}
}
