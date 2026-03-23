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
