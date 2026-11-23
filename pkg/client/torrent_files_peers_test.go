package client

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseFilePrioritySupportsWireValuesAndAliases(t *testing.T) {
	tests := []struct {
		input string
		want  FilePriority
	}{
		{"skip", FilePrioritySkip},
		{"none", FilePrioritySkip},
		{"do-not-download", FilePrioritySkip},
		{"0", FilePrioritySkip},
		{"normal", FilePriorityNormal},
		{"1", FilePriorityNormal},
		{"high", FilePriorityHigh},
		{"6", FilePriorityHigh},
		{"maximum", FilePriorityMaximum},
		{"max", FilePriorityMaximum},
		{"7", FilePriorityMaximum},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := ParseFilePriority(test.input)
			if err != nil || got != test.want {
				t.Fatalf("ParseFilePriority(%q) = %d, %v; want %d", test.input, got, err, test.want)
			}
			if got.String() == "" {
				t.Fatal("priority string is empty")
			}
		})
	}
	for _, input := range []string{"", "2", "3", "4", "5", "8", "bogus"} {
		t.Run("invalid-"+input, func(t *testing.T) {
			if _, err := ParseFilePriority(input); err == nil {
				t.Fatalf("ParseFilePriority(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestFetchTorrentFilesAndMutationsUseExactAPIValues(t *testing.T) {
	server := newQBTestServer(t)
	server.FilesRawBody = `[{"index":2,"name":"dir/file.iso","size":1234,"progress":0.5,"priority":6,"is_seed":false,"piece_range":[1,2],"availability":1.25}]`
	app := server.newApp()
	const hash = "0123456789abcdef0123456789abcdef01234567"

	files, err := app.FetchTorrentFilesContext(t.Context(), hash, []int{2, 4})
	if err != nil {
		t.Fatalf("FetchTorrentFilesContext: %v", err)
	}
	if len(files) != 1 || files[0].Index != 2 || files[0].Name != "dir/file.iso" || files[0].Priority != 6 {
		t.Fatalf("files = %#v", files)
	}
	server.StatusQueue["/api/v2/torrents/files"] = []int{500}
	if _, err := app.FetchTorrentFiles(hash); err == nil {
		t.Fatal("torrent files HTTP failure unexpectedly succeeded")
	}
	server.FilesRawBody = "{"
	if _, err := app.FetchTorrentFiles(hash); err == nil {
		t.Fatal("malformed torrent files response unexpectedly succeeded")
	}
	server.FilesRawBody = `[{"index":2,"name":"dir/file.iso","size":1234,"progress":0.5,"priority":6}]`

	if err := app.SetFilePriority(hash, []int{2, 4}, FilePriorityHigh); err != nil {
		t.Fatalf("SetFilePriority: %v", err)
	}
	form := server.Forms["/api/v2/torrents/filePrio"]
	if len(form) != 1 || form[0].Get("hash") != hash || form[0].Get("id") != "2|4" || form[0].Get("priority") != "6" {
		t.Fatalf("filePrio form = %#v", form)
	}

	oldPath := " dir/file.iso "
	newPath := " renamed/file.iso "
	if err := app.RenameTorrentFile(hash, oldPath, newPath); err != nil {
		t.Fatalf("RenameTorrentFile: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/renameFile"][0]; got.Get("hash") != hash || got.Get("oldPath") != oldPath || got.Get("newPath") != newPath {
		t.Fatalf("renameFile form = %#v", got)
	}
	if err := app.RenameTorrentFolder(hash, "dir", "renamed"); err != nil {
		t.Fatalf("RenameTorrentFolder: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/renameFolder"][0]; got.Get("oldPath") != "dir" || got.Get("newPath") != "renamed" {
		t.Fatalf("renameFolder form = %#v", got)
	}
}

func TestTorrentFileMutationsValidateBeforeRequest(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	const hash = "0123456789abcdef0123456789abcdef01234567"
	for name, call := range map[string]func() error{
		"empty hash": func() error { return app.SetFilePriority("", []int{0}, FilePriorityNormal) },
		"empty ids":  func() error { return app.SetFilePriority(hash, nil, FilePriorityNormal) },
		"negative id": func() error {
			return app.SetFilePriority(hash, []int{-1}, FilePriorityNormal)
		},
		"invalid priority": func() error { return app.SetFilePriority(hash, []int{0}, FilePriority(2)) },
		"blank old path":   func() error { return app.RenameTorrentFile(hash, " ", "new") },
		"blank new path":   func() error { return app.RenameTorrentFolder(hash, "old", "\t") },
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
				t.Fatalf("error = %v, want ExitBadArgs", err)
			}
		})
	}
	if len(server.Forms) != 0 {
		t.Fatalf("invalid file mutations sent requests: %#v", server.Forms)
	}
}

func TestTorrentPeerMergePreservesFieldsInPartialDeltas(t *testing.T) {
	var peers TorrentPeers
	if err := json.Unmarshal([]byte(`{
		"rid":1,"full_update":true,"show_flags":true,
		"peers":{"peer-a":{"client":"Client A","ip":"2001:db8::1","port":6881,"dl_speed":123,"files":"one.mkv","flags":"D"}},
		"peers_removed":[]
	}`), &peers); err != nil {
		t.Fatal(err)
	}
	peers.Merge(peers)

	var update TorrentPeers
	if err := json.Unmarshal([]byte(`{
		"rid":2,"full_update":false,
		"peers":{"peer-a":{"dl_speed":0,"files":"","flags":""}},
		"peers_removed":[]
	}`), &update); err != nil {
		t.Fatal(err)
	}
	peers.Merge(update)
	peer := peers.Peers["peer-a"]
	if peer.Client != "Client A" || peer.IP != "2001:db8::1" || peer.Port != 6881 {
		t.Fatalf("partial merge discarded unchanged fields: %#v", peer)
	}
	if peer.DLSpeed != 0 || peer.Files != "" || peer.Flags != "" {
		t.Fatalf("partial merge did not apply zero/empty updates: %#v", peer)
	}
	if !peers.ShowFlags {
		t.Fatal("omitted show_flags changed the prior value")
	}

	var removal TorrentPeers
	if err := json.Unmarshal([]byte(`{"rid":3,"full_update":false,"peers_removed":["peer-a"]}`), &removal); err != nil {
		t.Fatal(err)
	}
	peers.Merge(removal)
	if len(peers.Peers) != 0 {
		t.Fatalf("removed peer remained after delta: %#v", peers.Peers)
	}
}

func TestFetchTorrentPeersUsesRIDAndReportsFailures(t *testing.T) {
	server := newQBTestServer(t)
	server.PeersRawBody = `{"rid":9,"full_update":true,"show_flags":false,"peers":{"peer":{"client":"A"}}}`
	app := server.newApp()
	const hash = "0123456789abcdef0123456789abcdef01234567"
	peers, err := app.FetchTorrentPeersContext(t.Context(), hash, 8)
	if err != nil || peers.RID != 9 || peers.Peers["peer"].Client != "A" {
		t.Fatalf("peers = %#v, err = %v", peers, err)
	}
	server.StatusQueue["/api/v2/sync/torrentPeers"] = []int{500}
	if _, err := app.FetchTorrentPeers(hash, peers.RID); err == nil {
		t.Fatal("peer HTTP failure unexpectedly succeeded")
	}
	if _, err := app.FetchTorrentPeers(hash, -1); err == nil {
		t.Fatal("negative peer RID unexpectedly succeeded")
	}
}
