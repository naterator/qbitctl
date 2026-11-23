package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestFilesCommandsListJSONAndMutateResolvedHash(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hash, Name: "Example"}}
	server.FilesRawBody = `[{"index":0,"name":"folder/file.iso","size":1024,"progress":1,"priority":1}]`
	withQBServerClientFactory(t, server)
	opts := &CLIOptions{URL: server.creds().URL, User: server.creds().User, Pass: server.creds().Pass}

	list := newFilesCmd(opts)
	var out bytes.Buffer
	list.SetOut(&out)
	list.SetErr(&out)
	list.SetArgs([]string{hash[:8], "--json"})
	if err := list.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("files list: %v", err)
	}
	var files []qbt.TorrentFile
	if err := json.Unmarshal(out.Bytes(), &files); err != nil || len(files) != 1 || files[0].Index != 0 {
		t.Fatalf("files JSON = %q, err=%v", out.String(), err)
	}

	priority := newFilesCmd(opts)
	priority.SetOut(&out)
	priority.SetErr(&out)
	priority.SetArgs([]string{"priority", hash[:8], "high", "0"})
	if err := priority.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("files priority: %v", err)
	}
	form := server.Forms["/api/v2/torrents/filePrio"]
	if len(form) != 1 || form[0].Get("hash") != hash || form[0].Get("id") != "0" || form[0].Get("priority") != "6" {
		t.Fatalf("filePrio form = %#v", form)
	}

	rename := newFilesCmd(opts)
	rename.SetOut(&out)
	rename.SetErr(&out)
	rename.SetArgs([]string{"rename-file", hash[:8], "old path", "new path"})
	if err := rename.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("files rename-file: %v", err)
	}
	form = server.Forms["/api/v2/torrents/renameFile"]
	if len(form) != 1 || form[0].Get("hash") != hash || form[0].Get("oldPath") != "old path" || form[0].Get("newPath") != "new path" {
		t.Fatalf("renameFile form = %#v", form)
	}
}

func TestPeersCommandJSONAndHumanOutputSanitizeServerText(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hash, Name: "Example"}}
	server.PeersRawBody = `{"rid":4,"full_update":true,"show_flags":true,"peers":{"peer":{"ip":"2001:db8::1","port":6881,"client":"Client\nName","connection":"TCP\tconnected","progress":0.5,"dl_speed":1024,"up_speed":512}}}`
	withQBServerClientFactory(t, server)
	opts := &CLIOptions{URL: server.creds().URL, User: server.creds().User, Pass: server.creds().Pass}

	jsonCmd := newPeersCmd(opts)
	var out bytes.Buffer
	jsonCmd.SetOut(&out)
	jsonCmd.SetErr(&out)
	jsonCmd.SetArgs([]string{hash[:8], "--json"})
	if err := jsonCmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("peers JSON: %v", err)
	}
	var peers qbt.TorrentPeers
	if err := json.Unmarshal(out.Bytes(), &peers); err != nil || peers.RID != 4 {
		t.Fatalf("peers JSON = %q, err=%v", out.String(), err)
	}

	human := newPeersCmd(opts)
	out.Reset()
	human.SetOut(&out)
	human.SetErr(&out)
	human.SetArgs([]string{hash[:8]})
	if err := human.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("peers table: %v", err)
	}
	if !strings.Contains(out.String(), "[2001:db8::1]:6881") || !strings.Contains(out.String(), "Client Name") || !strings.Contains(out.String(), "TCP connected") {
		t.Fatalf("peer table did not sanitize/render IPv6 peer: %q", out.String())
	}
	if strings.ContainsAny(out.String(), "\n\r\t") {
		// Headers and table separators intentionally contain newlines/tabs; the
		// assertion below focuses on raw control characters embedded in fields.
		if strings.Contains(out.String(), "Client\nName") || strings.Contains(out.String(), "TCP\tconnected") {
			t.Fatalf("peer table retained control characters: %q", out.String())
		}
	}
}

func TestFilesCommandRejectsInvalidPriorityAndIndexes(t *testing.T) {
	if _, err := parseFileIndexes([]string{"0", "0"}); err == nil {
		t.Fatal("duplicate file index unexpectedly accepted")
	}
	if _, err := parseFileIndexes([]string{"-1"}); err == nil {
		t.Fatal("negative file index unexpectedly accepted")
	}
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: "0123456789abcdef0123456789abcdef01234567"}}
	withQBServerClientFactory(t, server)
	opts := &CLIOptions{URL: server.creds().URL, User: server.creds().User, Pass: server.creds().Pass}
	cmd := newFilesCmd(opts)
	cmd.SetArgs([]string{"priority", "0123456789abcdef0123456789abcdef01234567", "bogus", "0"})
	err := cmd.ExecuteContext(t.Context())
	var coded *qbt.CodedError
	if !errors.As(err, &coded) || coded.Code != qbt.ExitBadArgs {
		t.Fatalf("invalid priority error = %v, want ExitBadArgs", err)
	}
}
