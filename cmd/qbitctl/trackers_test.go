package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTrackersListHidesFullURLsByDefault(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hash, Name: "Example"}}
	server.Trackers = []TrackerEntry{
		{URL: "https://user:secret@tracker.example/announce?passkey=hidden", Status: 2, Tier: 0, NumPeers: 8, NumSeeds: 3, NumLeeches: 5},
		{URL: "** [DHT] **", Status: 0, Tier: -1},
	}
	withQBServerClientFactory(t, server)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "trackers", hash[:8]})
	stdout, stderr := captureOutput(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("trackers list: %v", err)
		}
	})
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if !strings.Contains(stdout, "Host\tStatus\tTier\tPeers\tSeeds\tLeeches") || !strings.Contains(stdout, "tracker.example\tWorking\t0\t8\t3\t5") || !strings.Contains(stdout, "DHT\tDisabled") {
		t.Fatalf("tracker table = %q", stdout)
	}
	if strings.Contains(stdout, "secret") || strings.Contains(stdout, "passkey") || strings.Contains(stdout, "announce?") {
		t.Fatalf("default tracker table exposed a full URL: %q", stdout)
	}

	full := newRootCmd()
	full.SetArgs([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "trackers", hash[:8], "--full-url"})
	stdout, stderr = captureOutput(t, func() {
		if err := full.Execute(); err != nil {
			t.Fatalf("trackers --full-url: %v", err)
		}
	})
	if strings.TrimSpace(stderr) != "" || !strings.Contains(stdout, "URL\tStatus") || !strings.Contains(stdout, "passkey=hidden") {
		t.Fatalf("full tracker table = %q, stderr %q", stdout, stderr)
	}
}

func TestTrackersJSONIncludesStructuredTrackerRecords(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hash, Name: "Example"}}
	server.Trackers = []TrackerEntry{{URL: "https://tracker.example/announce?passkey=secret", Status: 4, Tier: 1, NumPeers: 2, NumSeeds: 1, NumLeeches: 1, NumDownloaded: 12, Message: "timeout"}}
	withQBServerClientFactory(t, server)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "trackers", hash, "--json"})
	stdout, stderr := captureOutput(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("trackers --json: %v", err)
		}
	})
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	var got []TrackerEntry
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode tracker JSON %q: %v", stdout, err)
	}
	if len(got) != 1 || got[0].URL == "" || got[0].Status != 4 || got[0].NumDownloaded != 12 || got[0].Message != "timeout" {
		t.Fatalf("tracker JSON = %#v", got)
	}
}

func TestTrackerMutationCommandsUseResolvedHashAndExactURLs(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hash, Name: "Example"}}
	withQBServerClientFactory(t, server)

	tests := []struct {
		name string
		args []string
		path string
		want map[string]string
	}{
		{name: "add", args: []string{"trackers", "add", hash[:8], "https://one.example/announce?token=a&part=1", "udp://two.example:6969/announce"}, path: "/api/v2/torrents/addTrackers", want: map[string]string{"hash": hash, "urls": "https://one.example/announce?token=a&part=1\nudp://two.example:6969/announce"}},
		{name: "edit", args: []string{"trackers", "edit", hash[:8], "https://one.example/announce?token=a&part=1", "https://new.example/announce?token=b"}, path: "/api/v2/torrents/editTracker", want: map[string]string{"hash": hash, "origUrl": "https://one.example/announce?token=a&part=1", "url": "https://one.example/announce?token=a&part=1", "newUrl": "https://new.example/announce?token=b"}},
		{name: "remove", args: []string{"trackers", "remove", hash[:8], "https://one.example/announce?token=a&part=1", "udp://two.example:6969/announce"}, path: "/api/v2/torrents/removeTrackers", want: map[string]string{"hash": hash, "urls": "https://one.example/announce?token=a&part=1|udp://two.example:6969/announce"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs(append([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret"}, test.args...))
			stdout, stderr := captureOutput(t, func() {
				if err := cmd.Execute(); err != nil {
					t.Fatalf("%s: %v", test.name, err)
				}
			})
			if stdout != "" || stderr != "" {
				t.Fatalf("%s output stdout=%q stderr=%q", test.name, stdout, stderr)
			}
			forms := server.Forms[test.path]
			if len(forms) == 0 {
				t.Fatalf("%s did not call %s; forms = %#v", test.name, test.path, server.Forms)
			}
			for key, want := range test.want {
				if got := forms[len(forms)-1].Get(key); got != want {
					t.Fatalf("%s %s = %q, want %q", test.name, key, got, want)
				}
			}
		})
	}
}
