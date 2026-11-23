package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/naterator/qbitctl/internal/qbtest"
)

func TestFetchAllTorrentsSupportsLargeResponses(t *testing.T) {
	server := newQBTestServer(t)
	for i := 0; i < 1500; i++ {
		server.Torrents = append(server.Torrents, TorrentInfo{
			Name: strings.Repeat("x", 1024),
			Hash: "0123456789abcdef0123456789abcdef01234567",
		})
	}

	app := server.newApp()
	torrents, _, err := app.FetchAllTorrents()
	if err != nil {
		t.Fatalf("FetchAllTorrents err = %v", err)
	}
	if len(torrents) != len(server.Torrents) {
		t.Fatalf("torrent count = %d, want %d", len(torrents), len(server.Torrents))
	}
}

func TestFetchTorrentsContextPushesNativeFilters(t *testing.T) {
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Linux ISO", Hash: "0123456789abcdef0123456789abcdef01234567"}}
	app := server.newApp()

	var got url.Values
	app.client.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/v2/torrents/info" {
			got = req.URL.Query()
		}
		return server.roundTrip(req), nil
	})

	filter := TorrentListFilter{State: "stopped", Category: "linux", Tag: "release"}
	if _, _, err := app.FetchTorrentsContext(context.Background(), filter); err != nil {
		t.Fatalf("FetchTorrentsContext err = %v", err)
	}
	if got.Get("filter") != "stopped" || got.Get("category") != "linux" || got.Get("tag") != "release" {
		t.Fatalf("torrent info query = %v, want filter=stopped category=linux tag=release", got)
	}
	if server.InfoCalls != 1 {
		t.Fatalf("torrent info calls = %d, want 1", server.InfoCalls)
	}
}

func TestFetchTorrentsContextLocallyFiltersPortableStates(t *testing.T) {
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{
		{Name: "Queued", Hash: "0123456789abcdef0123456789abcdef01234567", State: "queuedDL"},
		{Name: "Checking", Hash: "1123456789abcdef0123456789abcdef01234567", State: "checkingUP"},
		{Name: "Downloading", Hash: "2123456789abcdef0123456789abcdef01234567", State: "downloading"},
	}
	app := server.newApp()
	var queries []url.Values
	app.client.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/v2/torrents/info" {
			queries = append(queries, req.URL.Query())
		}
		return server.roundTrip(req), nil
	})

	queued, _, err := app.FetchTorrentsContext(context.Background(), TorrentListFilter{State: "queued"})
	if err != nil {
		t.Fatalf("queued FetchTorrentsContext err = %v", err)
	}
	if len(queued) != 1 || queued[0].Name != "Queued" {
		t.Fatalf("queued torrents = %#v, want only queued torrent", queued)
	}
	if len(queries) != 1 {
		t.Fatalf("queued torrent queries = %d, want 1", len(queries))
	}
	if _, ok := queries[0]["filter"]; ok {
		t.Fatalf("queued query unexpectedly sent unsupported filter: %v", queries[0])
	}

	checking, _, err := app.FetchTorrentsContext(context.Background(), TorrentListFilter{State: "checking"})
	if err != nil {
		t.Fatalf("checking FetchTorrentsContext err = %v", err)
	}
	if len(checking) != 1 || checking[0].Name != "Checking" {
		t.Fatalf("checking torrents = %#v, want only checking torrent", checking)
	}
	if len(queries) != 2 {
		t.Fatalf("checking torrent queries = %d, want 2 total", len(queries))
	}
	if _, ok := queries[1]["filter"]; ok {
		t.Fatalf("checking query unexpectedly sent unsupported filter: %v", queries[1])
	}
}

func TestFetchMainDataPassesRIDAndReturnsRawBody(t *testing.T) {
	server := newQBTestServer(t)
	wantBody := `{"rid":42,"full_update":true,"torrents":{}}`
	server.MainDataQueue = []string{wantBody}
	app := server.newApp()

	body, err := app.FetchMainData(context.Background(), 41)
	if err != nil {
		t.Fatalf("FetchMainData err = %v", err)
	}
	if string(body) != wantBody {
		t.Fatalf("FetchMainData body = %q", body)
	}
	if got := server.MainDataRIDs; len(got) != 1 || got[0] != "41" {
		t.Fatalf("FetchMainData rid = %v, want [41]", got)
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
			server.Torrents = tt.torrents
			if tt.name == "full hash" {
				server.Torrents = []TorrentInfo{{Hash: uniqueHash}}
			}
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

func TestFetchAllTorrentsPropagatesContextErrors(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Hour))
	defer stop()
	app := newQBTestServer(t).newApp()
	app.client.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})

	for _, ctx := range []context.Context{canceled, expired} {
		if torrents, body, err := app.FetchAllTorrentsContext(ctx); !errors.Is(err, ctx.Err()) || torrents != nil || body != nil {
			t.Fatalf("FetchAllTorrentsContext = %v, %q, %v, want %v with no data", torrents, body, err, ctx.Err())
		}
	}
}

func TestResolveHashes(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"

	t.Run("multiple full hashes", func(t *testing.T) {
		server := newQBTestServer(t)
		server.Torrents = []TorrentInfo{{Hash: hash1}, {Hash: hash2}}
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
		server.Torrents = []TorrentInfo{{Hash: hash1}, {Hash: hash2}}
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
		server.Torrents = []TorrentInfo{{Hash: hash1}}
		app := server.newApp()

		_, err := app.ResolveHashes([]string{hash1[:8], "deadbeef"})
		if err == nil {
			t.Fatal("ResolveHashes unexpectedly succeeded with bad hash")
		}
	})

	t.Run("blank selectors never match the only torrent", func(t *testing.T) {
		server := newQBTestServer(t)
		server.Torrents = []TorrentInfo{{Hash: hash1}}
		app := server.newApp()

		for _, inputs := range [][]string{{""}, {" \t"}, {hash1[:8], ""}, {hash1, " \n"}} {
			got, err := app.ResolveHashes(inputs)
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitBadArgs || got != nil {
				t.Fatalf("ResolveHashes(%q) = %v, %v, want ExitBadArgs with no hashes", inputs, got, err)
			}
		}
		if server.InfoCalls != 0 {
			t.Fatalf("queried torrent list %d times for invalid selectors", server.InfoCalls)
		}
	})

	t.Run("mixed full and short hashes", func(t *testing.T) {
		server := newQBTestServer(t)
		server.Torrents = []TorrentInfo{{Hash: hash1}, {Hash: hash2}}
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

func TestResolveHashesChecksFullHashesAndDeduplicates(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hash1}, {Hash: hash2}}
	app := server.newApp()

	got, err := app.ResolveHashes([]string{strings.ToUpper(hash1), hash2, hash1})
	if err != nil || !reflect.DeepEqual(got, []string{hash1, hash2}) {
		t.Fatalf("ResolveHashes = %v, %v, want [%s %s]", got, err, hash1, hash2)
	}
	if server.InfoCalls != 1 || server.InfoHashQueries[0] != strings.ToLower(hash1)+"|"+hash2 {
		t.Fatalf("filtered info queries = %#v, want one joined query", server.InfoHashQueries)
	}

	_, err = app.ResolveHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err == nil || !strings.Contains(err.Error(), "No torrent matches hash") {
		t.Fatalf("unknown full hash error = %v", err)
	}
	if server.InfoCalls != 2 {
		t.Fatalf("unknown full hash did not make one filtered request: %d", server.InfoCalls)
	}
}

func TestResolveHashesValidatesOfflineAndUsesFullListOverCap(t *testing.T) {
	server := newQBTestServer(t)
	for i := 0; i < 51; i++ {
		server.Torrents = append(server.Torrents, TorrentInfo{Hash: fmt.Sprintf("%040x", i)})
	}
	app := server.newApp()
	inputs := make([]string, 51)
	for i := range inputs {
		inputs[i] = server.Torrents[i].Hash
	}
	got, err := app.ResolveHashes(inputs)
	if err != nil || len(got) != 51 || server.InfoHashQueries[0] != "" {
		t.Fatalf("51 full hashes = %d, %v, queries=%#v", len(got), err, server.InfoHashQueries)
	}
	before := server.InfoCalls
	_, err = app.ResolveHashes([]string{"zz12"})
	if err == nil || !strings.Contains(err.Error(), "Invalid hash or prefix") || server.InfoCalls != before {
		t.Fatalf("invalid prefix = %v, info calls %d -> %d", err, before, server.InfoCalls)
	}

	filteredServer := newQBTestServer(t)
	for i := 0; i < 50; i++ {
		filteredServer.Torrents = append(filteredServer.Torrents, TorrentInfo{Hash: fmt.Sprintf("%040x", i)})
	}
	filteredApp := filteredServer.newApp()
	filteredInputs := make([]string, 50)
	for i := range filteredInputs {
		filteredInputs[i] = filteredServer.Torrents[i].Hash
	}
	if _, err := filteredApp.ResolveHashes(filteredInputs); err != nil {
		t.Fatalf("50 full hashes = %v", err)
	}
	if len(filteredServer.InfoHashQueries) != 1 || filteredServer.InfoHashQueries[0] == "" {
		t.Fatalf("50 full hashes used unfiltered query: %#v", filteredServer.InfoHashQueries)
	}
}

func TestGetTorrentInfoErrors(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		server := newQBTestServer(t)
		server.InfoRawBody = "{"
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
		server.InfoRawBody = "[]"
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
