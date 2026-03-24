package client

import (
	"errors"
	"strings"
	"testing"
)

func TestFetchAllTorrentsSupportsLargeResponses(t *testing.T) {
	server := newQBTestServer(t)
	for i := 0; i < 1500; i++ {
		server.torrents = append(server.torrents, TorrentInfo{
			Name: strings.Repeat("x", 1024),
			Hash: "0123456789abcdef0123456789abcdef01234567",
		})
	}

	app := server.newApp()
	torrents, _, err := app.FetchAllTorrents()
	if err != nil {
		t.Fatalf("FetchAllTorrents err = %v", err)
	}
	if len(torrents) != len(server.torrents) {
		t.Fatalf("torrent count = %d, want %d", len(torrents), len(server.torrents))
	}
}

func TestResolveHashScenarios(t *testing.T) {
	uniqueHash := "0123456789abcdef0123456789abcdef01234567"
	otherHash := "fedcba9876543210fedcba9876543210fedcba98"

	tests := []struct {
		name     string
		input    string
		torrents []TorrentInfo
		wantErr  bool
		wantCode int
		wantHash string
	}{
		{name: "full hash", input: strings.ToUpper(uniqueHash), wantHash: uniqueHash},
		{name: "unique short hash", input: uniqueHash[:8], torrents: []TorrentInfo{{Hash: uniqueHash}}, wantHash: uniqueHash},
		{name: "missing short hash", input: "abcdef", torrents: []TorrentInfo{{Hash: uniqueHash}}, wantErr: true, wantCode: ExitBadArgs},
		{name: "ambiguous short hash", input: uniqueHash[:6], torrents: []TorrentInfo{{Hash: uniqueHash}, {Hash: uniqueHash[:6] + otherHash[6:]}}, wantErr: true, wantCode: ExitBadArgs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newQBTestServer(t)
			server.torrents = tt.torrents
			app := server.newApp()

			gotHash, err := app.ResolveHash(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveHash err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				var coded *CodedError
				if !errors.As(err, &coded) || coded.Code != tt.wantCode {
					t.Fatalf("ResolveHash err code = %v, want %d", err, tt.wantCode)
				}
			}
			if !tt.wantErr && gotHash != tt.wantHash {
				t.Fatalf("resolved hash = %q, want %q", gotHash, tt.wantHash)
			}
		})
	}
}

func TestResolveHashes(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"

	t.Run("multiple full hashes", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()

		got, err := app.ResolveHashes([]string{hash1, hash2})
		if err != nil {
			t.Fatalf("ResolveHashes err = %v", err)
		}
		if len(got) != 2 || got[0] != hash1 || got[1] != hash2 {
			t.Fatalf("ResolveHashes = %v, want [%s %s]", got, hash1, hash2)
		}
	})

	t.Run("multiple short hashes", func(t *testing.T) {
		server := newQBTestServer(t)
		server.torrents = []TorrentInfo{{Hash: hash1}, {Hash: hash2}}
		app := server.newApp()

		got, err := app.ResolveHashes([]string{hash1[:8], hash2[:8]})
		if err != nil {
			t.Fatalf("ResolveHashes err = %v", err)
		}
		if len(got) != 2 || got[0] != hash1 || got[1] != hash2 {
			t.Fatalf("ResolveHashes = %v", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()

		_, err := app.ResolveHashes(nil)
		if err == nil {
			t.Fatal("ResolveHashes unexpectedly succeeded with nil input")
		}
		var coded *CodedError
		if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
			t.Fatalf("ResolveHashes err = %v, want ExitBadArgs", err)
		}
	})

	t.Run("one bad short hash fails all", func(t *testing.T) {
		server := newQBTestServer(t)
		server.torrents = []TorrentInfo{{Hash: hash1}}
		app := server.newApp()

		_, err := app.ResolveHashes([]string{hash1[:8], "deadbeef"})
		if err == nil {
			t.Fatal("ResolveHashes unexpectedly succeeded with bad hash")
		}
	})

	t.Run("mixed full and short hashes", func(t *testing.T) {
		server := newQBTestServer(t)
		server.torrents = []TorrentInfo{{Hash: hash1}, {Hash: hash2}}
		app := server.newApp()

		got, err := app.ResolveHashes([]string{hash1, hash2[:6]})
		if err != nil {
			t.Fatalf("ResolveHashes err = %v", err)
		}
		if len(got) != 2 || got[0] != hash1 || got[1] != hash2 {
			t.Fatalf("ResolveHashes = %v", got)
		}
	})
}

func TestResolveHashEmpty(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()

	_, err := app.ResolveHash("")
	if err == nil {
		t.Fatal("ResolveHash unexpectedly succeeded with empty input")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
		t.Fatalf("ResolveHash err = %v, want ExitBadArgs", err)
	}
}

func TestGetTorrentInfoErrors(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		server := newQBTestServer(t)
		server.infoRawBody = "{"
		app := server.newApp()
		hash := "0123456789abcdef0123456789abcdef01234567"

		if _, err := app.GetTorrentInfo(hash); err == nil {
			t.Fatal("GetTorrentInfo unexpectedly succeeded")
		} else {
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitFetchFail {
				t.Fatalf("GetTorrentInfo err code = %v, want %d", err, ExitFetchFail)
			}
		}
	})

	t.Run("empty array", func(t *testing.T) {
		server := newQBTestServer(t)
		server.infoRawBody = "[]"
		app := server.newApp()
		hash := "0123456789abcdef0123456789abcdef01234567"

		if _, err := app.GetTorrentInfo(hash); err == nil {
			t.Fatal("GetTorrentInfo unexpectedly succeeded")
		} else {
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitFetchFail {
				t.Fatalf("GetTorrentInfo err code = %v, want %d", err, ExitFetchFail)
			}
		}
	})
}
