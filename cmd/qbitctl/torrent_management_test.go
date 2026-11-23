package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestTorrentManagementCommandsResolveBulkHashes(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Alpha", Hash: hash1}, {Name: "Beta", Hash: hash2}}
	withQBServerClientFactory(t, server)
	opts := &CLIOptions{URL: server.creds().URL, User: server.creds().User, Pass: server.creds().Pass}

	cases := []struct {
		name string
		cmd  *cobra.Command
		path string
	}{
		{name: "recheck", cmd: newRecheckCmd(opts), path: "/api/v2/torrents/recheck"},
		{name: "reannounce", cmd: newReannounceCmd(opts), path: "/api/v2/torrents/reannounce"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.cmd.SetArgs([]string{hash1[:8], hash2[:8]})
			if err := tt.cmd.Execute(); err != nil {
				t.Fatalf("%s returned %v", tt.name, err)
			}
			forms := server.Forms[tt.path]
			if len(forms) != 1 || forms[0].Get("hashes") != hash1+"|"+hash2 {
				t.Fatalf("%s forms = %#v, want joined resolved hashes", tt.name, forms)
			}
		})
	}
}

func TestQueueCommandSyntaxAndBulkResolution(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Alpha", Hash: hash1}, {Name: "Beta", Hash: hash2}}
	withQBServerClientFactory(t, server)
	opts := &CLIOptions{URL: server.creds().URL, User: server.creds().User, Pass: server.creds().Pass}
	for _, test := range []struct {
		name string
		path string
	}{
		{name: "up", path: "/api/v2/torrents/increasePrio"},
		{name: "down", path: "/api/v2/torrents/decreasePrio"},
		{name: "top", path: "/api/v2/torrents/topPrio"},
		{name: "bottom", path: "/api/v2/torrents/bottomPrio"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := newQueueCmd(opts)
			root.SetArgs([]string{test.name, hash1[:8], hash2[:8]})
			if err := root.Execute(); err != nil {
				t.Fatalf("queue %s returned %v", test.name, err)
			}
			forms := server.Forms[test.path]
			if len(forms) != 1 || forms[0].Get("hashes") != hash1+"|"+hash2 {
				t.Fatalf("queue %s forms = %#v, want one joined request", test.name, forms)
			}
		})
	}
}

func TestRenameCommandAcceptsNameThenOneHash(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Old name", Hash: hash}}
	withQBServerClientFactory(t, server)
	opts := &CLIOptions{URL: server.creds().URL, User: server.creds().User, Pass: server.creds().Pass}
	cmd := newRenameCmd(opts)
	cmd.SetArgs([]string{"New name", hash[:8]})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("rename returned %v", err)
	}
	forms := server.Forms["/api/v2/torrents/rename"]
	if len(forms) != 1 || forms[0].Get("hash") != hash || forms[0].Get("name") != "New name" {
		t.Fatalf("rename forms = %#v, want singular hash and name", forms)
	}
}

func TestRenameCommandRejectsMultipleHashesBeforeLogin(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	opts := &CLIOptions{URL: server.creds().URL, User: server.creds().User, Pass: server.creds().Pass}
	cmd := newRenameCmd(opts)
	cmd.SetArgs([]string{"New name", "hash1", "hash2"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "accepts 2 arg(s), received 3") {
		t.Fatalf("rename multiple hashes error = %v", err)
	}
	if server.LoginCalls != 0 {
		t.Fatalf("rename authenticated before argument validation: %d logins", server.LoginCalls)
	}
}

func TestRenameCommandPropagatesActionFailure(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Old name", Hash: hash}}
	server.StatusQueue["/api/v2/torrents/rename"] = []int{500}
	withQBServerClientFactory(t, server)
	opts := &CLIOptions{URL: server.creds().URL, User: server.creds().User, Pass: server.creds().Pass}
	cmd := newRenameCmd(opts)
	cmd.SetArgs([]string{"New name", hash})

	err := cmd.Execute()
	var exit exitError
	if !errors.As(err, &exit) || exit.code != exitActionFail {
		t.Fatalf("rename error = %v, want ExitActionFail", err)
	}
}
