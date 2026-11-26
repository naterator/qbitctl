package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestFullHashFieldRoundTripsCollidingPrefixes(t *testing.T) {
	const first = "0123456701234567012345670123456701234567"
	const second = "01234567fedcba98fedcba98fedcba98fedcba98"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Alpha", Hash: first}, {Name: "Beta", Hash: second}}
	withQBServerClientFactory(t, server)
	run := func(args ...string) string {
		t.Helper()
		cmd := newRootCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs(append([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret"}, args...))
		if err := cmd.Execute(); err != nil || stderr.Len() != 0 {
			t.Fatalf("%q: %v; stderr = %q", args, err, &stderr)
		}
		return stdout.String()
	}

	legacy := run("list", "--fields", "hash", "--no-header")
	if legacy != "01234567\n01234567\n" {
		t.Fatalf("legacy hash output = %q", legacy)
	}
	var rows []struct{ Hash, Done string }
	if err := json.Unmarshal([]byte(run("list", "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Hash != "01234567" || rows[1].Hash != "01234567" || rows[0].Done != "0%" {
		t.Fatalf("legacy JSON rows = %+v", rows)
	}

	hashes := strings.Fields(run("list", "--fields", "hash-full", "--no-header"))
	slices.Sort(hashes)
	if !slices.Equal(hashes, []string{first, second}) {
		t.Fatalf("full hashes = %v", hashes)
	}
	for i, name := range []string{"Alpha", "Beta"} {
		if got, want := run("show", hashes[i], "--fields", "hash-full,name"), "hash-full\tname\n"+hashes[i]+"\t"+name+"\n"; got != want {
			t.Fatalf("show output = %q, want %q", got, want)
		}
	}
}
