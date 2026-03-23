package client

import "testing"

func TestSetCategoryVariants(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"

	t.Run("clears category", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()

		if err := app.SetCategory(fullHash, ""); err != nil {
			t.Fatalf("setCategory err = %v", err)
		}
		if len(server.forms["/api/v2/torrents/categories"]) != 0 && server.categoriesRawBody != "" {
			t.Fatal("unexpected category discovery")
		}
		form := server.forms["/api/v2/torrents/setCategory"][0]
		if form.Get("category") != "" {
			t.Fatalf("clear category form = %#v", form)
		}
	})

	t.Run("creates missing category", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()

		if err := app.SetCategory(fullHash, "linux"); err != nil {
			t.Fatalf("setCategory err = %v", err)
		}
		if got := server.forms["/api/v2/torrents/createCategory"][0].Get("category"); got != "linux" {
			t.Fatalf("createCategory category = %q, want %q", got, "linux")
		}
		if got := server.forms["/api/v2/torrents/setCategory"][0].Get("category"); got != "linux" {
			t.Fatalf("setCategory category = %q, want %q", got, "linux")
		}
	})

	t.Run("skips create when category exists", func(t *testing.T) {
		server := newQBTestServer(t)
		server.categories["linux"] = map[string]any{}
		app := server.newApp()

		if err := app.SetCategory(fullHash, "linux"); err != nil {
			t.Fatalf("setCategory err = %v", err)
		}
		if len(server.forms["/api/v2/torrents/createCategory"]) != 0 {
			t.Fatal("createCategory unexpectedly called")
		}
	})
}

func TestSetSequentialDownloadBehaviors(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"

	t.Run("no-op when already desired", func(t *testing.T) {
		server := newQBTestServer(t)
		server.torrents = []TorrentInfo{{Hash: fullHash, SequentialDL: true}}
		app := server.newApp()

		if err := app.SetSequentialDownload(fullHash, "true"); err != nil {
			t.Fatalf("setSequentialDownload err = %v", err)
		}
		if len(server.forms["/api/v2/torrents/toggleSequentialDownload"]) != 0 {
			t.Fatal("toggleSequentialDownload unexpectedly called")
		}
	})

	t.Run("toggles and verifies state", func(t *testing.T) {
		server := newQBTestServer(t)
		server.torrents = []TorrentInfo{{Hash: fullHash, SequentialDL: false}}
		app := server.newApp()

		if err := app.SetSequentialDownload(fullHash, "true"); err != nil {
			t.Fatalf("setSequentialDownload err = %v", err)
		}
		if len(server.forms["/api/v2/torrents/toggleSequentialDownload"]) != 1 {
			t.Fatal("toggleSequentialDownload was not called exactly once")
		}
	})
}

func TestSetLimitAndShareRequests(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{{
		Hash:              fullHash,
		RatioLimit:        1.25,
		SeedingTimeLimit:  7200,
		InactiveSeedingTL: 14400,
	}}
	app := server.newApp()

	if err := app.SetUploadLimit(fullHash, "512"); err != nil {
		t.Fatalf("setUploadLimit err = %v", err)
	}
	if got := server.forms["/api/v2/torrents/setUploadLimit"][0].Get("limit"); got != "512" {
		t.Fatalf("upload limit = %q, want 512", got)
	}

	if err := app.SetDownloadLimit(fullHash, "1024"); err != nil {
		t.Fatalf("setDownloadLimit err = %v", err)
	}
	if got := server.forms["/api/v2/torrents/setDownloadLimit"][0].Get("limit"); got != "1024" {
		t.Fatalf("download limit = %q, want 1024", got)
	}

	if err := app.SetSeedtimeLimit(fullHash, "3600"); err != nil {
		t.Fatalf("setSeedtimeLimit err = %v", err)
	}
	if got := server.forms["/api/v2/torrents/setShareLimits"][0].Get("seedingTimeLimit"); got != "3600" {
		t.Fatalf("seedingTimeLimit = %q, want 3600", got)
	}
	if got := server.forms["/api/v2/torrents/setShareLimits"][0].Get("ratioLimit"); got != "1.250000" {
		t.Fatalf("ratioLimit preserved = %q, want 1.250000", got)
	}
	if got := server.forms["/api/v2/torrents/setShareLimits"][0].Get("inactiveSeedingTimeLimit"); got != "14400" {
		t.Fatalf("inactiveSeedingTimeLimit preserved = %q, want 14400", got)
	}

	if err := app.SetRatioLimit(fullHash, "2.5"); err != nil {
		t.Fatalf("setRatioLimit err = %v", err)
	}
	if got := server.forms["/api/v2/torrents/setShareLimits"][1].Get("ratioLimit"); got != "2.500000" {
		t.Fatalf("ratioLimit = %q, want 2.500000", got)
	}
	if got := server.forms["/api/v2/torrents/setShareLimits"][1].Get("seedingTimeLimit"); got != "7200" {
		t.Fatalf("seedingTimeLimit preserved = %q, want 7200", got)
	}
	if got := server.forms["/api/v2/torrents/setShareLimits"][1].Get("inactiveSeedingTimeLimit"); got != "14400" {
		t.Fatalf("inactiveSeedingTimeLimit preserved = %q, want 14400", got)
	}
}
