package client

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSetCategoryVariants(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"

	t.Run("clears category", func(t *testing.T) {
		server := newQBTestServer(t)
		app := server.newApp()

		if err := app.SetCategory(fullHash, ""); err != nil {
			t.Fatalf("setCategory err = %v", err)
		}
		if len(server.Forms["/api/v2/torrents/categories"]) != 0 && server.CategoriesRawBody != "" {
			t.Fatal("unexpected category discovery")
		}
		form := server.Forms["/api/v2/torrents/setCategory"][0]
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
		if got := server.Forms["/api/v2/torrents/createCategory"][0].Get("category"); got != "linux" {
			t.Fatalf("createCategory category = %q, want %q", got, "linux")
		}
		if got := server.Forms["/api/v2/torrents/setCategory"][0].Get("category"); got != "linux" {
			t.Fatalf("setCategory category = %q, want %q", got, "linux")
		}
	})

	t.Run("skips create when category exists", func(t *testing.T) {
		server := newQBTestServer(t)
		server.Categories["linux"] = map[string]any{}
		app := server.newApp()

		if err := app.SetCategory(fullHash, "linux"); err != nil {
			t.Fatalf("setCategory err = %v", err)
		}
		if len(server.Forms["/api/v2/torrents/createCategory"]) != 0 {
			t.Fatal("createCategory unexpectedly called")
		}
	})
}

func TestSetSequentialDownloadBehaviors(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"

	t.Run("no-op when already desired", func(t *testing.T) {
		server := newQBTestServer(t)
		server.Torrents = []TorrentInfo{{Hash: fullHash, SequentialDL: true}}
		app := server.newApp()

		if err := app.SetSequentialDownload(fullHash, "true"); err != nil {
			t.Fatalf("setSequentialDownload err = %v", err)
		}
		if len(server.Forms["/api/v2/torrents/toggleSequentialDownload"]) != 0 {
			t.Fatal("toggleSequentialDownload unexpectedly called")
		}
	})

	t.Run("toggles and verifies state", func(t *testing.T) {
		server := newQBTestServer(t)
		server.Torrents = []TorrentInfo{{Hash: fullHash, SequentialDL: false}}
		app := server.newApp()

		if err := app.SetSequentialDownload(fullHash, "true"); err != nil {
			t.Fatalf("setSequentialDownload err = %v", err)
		}
		if len(server.Forms["/api/v2/torrents/toggleSequentialDownload"]) != 1 {
			t.Fatal("toggleSequentialDownload was not called exactly once")
		}
	})
}

func TestSetSequentialDownloadAppliesPerTorrent(t *testing.T) {
	hashA := "0123456789abcdef0123456789abcdef01234567"
	hashB := "1123456789abcdef0123456789abcdef01234567"
	hashC := "2123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{
		{Hash: hashA, SequentialDL: true},
		{Hash: hashB, SequentialDL: false},
		{Hash: hashC, SequentialDL: false},
	}
	app := server.newApp()
	if err := app.SetSequentialDownload(strings.Join([]string{hashA, hashB, hashC}, "|"), "true"); err != nil {
		t.Fatalf("SetSequentialDownload err = %v", err)
	}
	forms := server.Forms["/api/v2/torrents/toggleSequentialDownload"]
	if len(forms) != 1 || forms[0].Get("hashes") != hashB+"|"+hashC {
		t.Fatalf("toggle forms = %#v, want b|c", forms)
	}
	if server.Torrents[0].SequentialDL != true || server.Torrents[1].SequentialDL != true || server.Torrents[2].SequentialDL != true {
		t.Fatalf("sequential states = %#v, want all true", server.Torrents)
	}
}

func TestSetLimitAndShareRequests(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{
		Hash:              fullHash,
		RatioLimit:        1.25,
		SeedingTimeLimit:  7200,
		InactiveSeedingTL: 14400,
		ShareLimitsMode:   "MatchAll",
		ShareLimitAction:  "Stop",
	}}
	app := server.newApp()

	if err := app.SetUploadLimit(fullHash, "512"); err != nil {
		t.Fatalf("setUploadLimit err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setUploadLimit"][0].Get("limit"); got != "512" {
		t.Fatalf("upload limit = %q, want 512", got)
	}

	if err := app.SetDownloadLimit(fullHash, "1024"); err != nil {
		t.Fatalf("setDownloadLimit err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setDownloadLimit"][0].Get("limit"); got != "1024" {
		t.Fatalf("download limit = %q, want 1024", got)
	}

	if err := app.SetSeedtimeLimit(fullHash, "3600"); err != nil {
		t.Fatalf("setSeedtimeLimit err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setShareLimits"][0].Get("seedingTimeLimit"); got != "60" {
		t.Fatalf("seedingTimeLimit = %q, want 60 API minutes", got)
	}
	if got := server.Forms["/api/v2/torrents/setShareLimits"][0].Get("ratioLimit"); got != "1.250000" {
		t.Fatalf("ratioLimit preserved = %q, want 1.250000", got)
	}
	if got := server.Forms["/api/v2/torrents/setShareLimits"][0].Get("inactiveSeedingTimeLimit"); got != "14400" {
		t.Fatalf("inactiveSeedingTimeLimit preserved = %q, want 14400", got)
	}

	if err := app.SetRatioLimit(fullHash, "2.5"); err != nil {
		t.Fatalf("setRatioLimit err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setShareLimits"][1].Get("ratioLimit"); got != "2.500000" {
		t.Fatalf("ratioLimit = %q, want 2.500000", got)
	}
	if got := server.Forms["/api/v2/torrents/setShareLimits"][1].Get("seedingTimeLimit"); got != "60" {
		t.Fatalf("seedingTimeLimit preserved = %q, want 60 after the preceding update", got)
	}
	if got := server.Forms["/api/v2/torrents/setShareLimits"][1].Get("inactiveSeedingTimeLimit"); got != "14400" {
		t.Fatalf("inactiveSeedingTimeLimit preserved = %q, want 14400", got)
	}
	for _, form := range server.Forms["/api/v2/torrents/setShareLimits"] {
		if form.Get("shareLimitsMode") != "MatchAll" || form.Get("shareLimitAction") != "Stop" {
			t.Fatalf("share limits request lost current policy: %v", form)
		}
	}
}

func TestSetSeedtimeLimitSentinelsAndPartialMinutes(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: fullHash, RatioLimit: 1.25}}
	app := server.newApp()

	for _, tt := range []struct {
		input string
		want  string
	}{
		{input: "-1", want: "-1"},
		{input: "-2", want: "-2"},
	} {
		if err := app.SetSeedtimeLimit(fullHash, tt.input); err != nil {
			t.Fatalf("SetSeedtimeLimit(%q) err = %v", tt.input, err)
		}
		forms := server.Forms["/api/v2/torrents/setShareLimits"]
		form := forms[len(forms)-1]
		got := form.Get("seedingTimeLimit")
		if got != tt.want {
			t.Fatalf("SetSeedtimeLimit(%q) sent %q, want %q", tt.input, got, tt.want)
		}
		if form.Get("shareLimitsMode") != "Default" || form.Get("shareLimitAction") != "Default" {
			t.Fatalf("share limits request did not supply defaults for missing policy: %v", form)
		}
	}

	before := len(server.Forms["/api/v2/torrents/setShareLimits"])
	err := app.SetSeedtimeLimit(fullHash, "61")
	if err == nil {
		t.Fatal("SetSeedtimeLimit accepted seconds that are not a whole minute")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
		t.Fatalf("SetSeedtimeLimit partial-minute err = %v, want ExitBadArgs", err)
	}
	if got := len(server.Forms["/api/v2/torrents/setShareLimits"]); got != before {
		t.Fatalf("partial-minute input sent request; count = %d, want %d", got, before)
	}
}

func TestStatefulSettersPreservePerTorrentShareLimits(t *testing.T) {
	hashA := "0123456789abcdef0123456789abcdef01234567"
	hashB := "1123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{
		{Hash: hashA, RatioLimit: 1, SeedingTimeLimit: 10, InactiveSeedingTL: 20, ShareLimitsMode: "MatchAll", ShareLimitAction: "Stop"},
		{Hash: hashB, RatioLimit: 1, SeedingTimeLimit: 30, InactiveSeedingTL: 40, ShareLimitsMode: "MatchAll", ShareLimitAction: "Stop"},
	}
	app := server.newApp()
	selector := hashA + "|" + hashB
	if err := app.SetRatioLimit(selector, "2"); err != nil {
		t.Fatalf("SetRatioLimit err = %v", err)
	}
	forms := server.Forms["/api/v2/torrents/setShareLimits"]
	if len(forms) != 2 || forms[0].Get("hashes") != hashA || forms[1].Get("hashes") != hashB {
		t.Fatalf("ratio forms = %#v, want separate hashes", forms)
	}
	if forms[0].Get("seedingTimeLimit") != "10" || forms[1].Get("seedingTimeLimit") != "30" {
		t.Fatalf("ratio setter overwrote seed limits: %#v", forms)
	}
	if err := app.SetSeedtimeLimit(selector, "3600"); err != nil {
		t.Fatalf("SetSeedtimeLimit err = %v", err)
	}
	forms = server.Forms["/api/v2/torrents/setShareLimits"]
	if forms[2].Get("ratioLimit") != "2.000000" || forms[3].Get("ratioLimit") != "2.000000" {
		t.Fatalf("seed setter overwrote ratios: %#v", forms)
	}
}

func TestStatefulSettersGroupEqualShareLimits(t *testing.T) {
	hashA := "0123456789abcdef0123456789abcdef01234567"
	hashB := "1123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{
		{Hash: hashA, RatioLimit: 1, SeedingTimeLimit: 10, InactiveSeedingTL: 20},
		{Hash: hashB, RatioLimit: 1, SeedingTimeLimit: 10, InactiveSeedingTL: 20},
	}
	app := server.newApp()
	if err := app.SetRatioLimit(hashA+"|"+hashB, "2"); err != nil {
		t.Fatalf("SetRatioLimit err = %v", err)
	}
	forms := server.Forms["/api/v2/torrents/setShareLimits"]
	if len(forms) != 1 || forms[0].Get("hashes") != hashA+"|"+hashB {
		t.Fatalf("equal-limit forms = %#v, want one grouped request", forms)
	}
}

func TestStatefulSetterFetchesTargetedAtFiftyAndAllOverCap(t *testing.T) {
	makeHash := func(index int) string {
		return fmt.Sprintf("%040x", index)
	}

	for _, test := range []struct {
		name      string
		count     int
		wantQuery string
	}{
		{name: "fifty targeted", count: 50},
		{name: "fifty one bulk", count: 51, wantQuery: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newQBTestServer(t)
			hashes := make([]string, test.count)
			for index := range hashes {
				hashes[index] = makeHash(index)
			}
			server.Torrents = make([]TorrentInfo, test.count)
			for index, hash := range hashes {
				server.Torrents[index] = TorrentInfo{Hash: hash, RatioLimit: 1}
			}

			if err := server.newApp().SetRatioLimit(strings.Join(hashes, "|"), "2"); err != nil {
				t.Fatalf("SetRatioLimit err = %v", err)
			}
			if len(server.InfoHashQueries) != 1 {
				t.Fatalf("info queries = %#v, want one query", server.InfoHashQueries)
			}
			wantQuery := test.wantQuery
			if wantQuery == "" && test.count == 50 {
				wantQuery = strings.Join(hashes, "|")
			}
			if got := server.InfoHashQueries[0]; got != wantQuery {
				t.Fatalf("info query = %q, want %q", got, wantQuery)
			}
			if got := len(server.Forms["/api/v2/torrents/setShareLimits"]); got != 1 {
				t.Fatalf("share-limit forms = %d, want one grouped mutation", got)
			}
		})
	}
}

func TestStatefulSetterBulkFetchErrorPreventsMutation(t *testing.T) {
	server := newQBTestServer(t)
	hashes := make([]string, 51)
	server.Torrents = make([]TorrentInfo, len(hashes))
	for index := range hashes {
		hashes[index] = fmt.Sprintf("%040x", index)
		server.Torrents[index] = TorrentInfo{Hash: hashes[index], RatioLimit: 1}
	}
	server.StatusQueue["/api/v2/torrents/info"] = []int{http.StatusInternalServerError}

	err := server.newApp().SetRatioLimit(strings.Join(hashes, "|"), "2")
	if err == nil {
		t.Fatal("SetRatioLimit unexpectedly succeeded after bulk fetch error")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitFetchFail {
		t.Fatalf("SetRatioLimit error = %v, want ExitFetchFail", err)
	}
	if got := len(server.Forms["/api/v2/torrents/setShareLimits"]); got != 0 {
		t.Fatalf("share-limit forms = %d after fetch error, want none", got)
	}
}

func TestLegacyTagsApplyPerTorrent(t *testing.T) {
	hashA := "0123456789abcdef0123456789abcdef01234567"
	hashB := "1123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hashA, Tags: "old"}, {Hash: hashB, Tags: "other"}}
	server.StatusQueue["/api/v2/torrents/setTags"] = []int{http.StatusNotFound}
	app := server.newApp()
	if err := app.SetTags(hashA+"|"+hashB, "new"); err != nil {
		t.Fatalf("SetTags err = %v", err)
	}
	adds := server.Forms["/api/v2/torrents/addTags"]
	removes := server.Forms["/api/v2/torrents/removeTags"]
	if len(adds) != 2 || len(removes) != 2 {
		t.Fatalf("legacy tag forms = add %#v remove %#v, want two groups", adds, removes)
	}
	if adds[0].Get("hashes") != hashA || adds[1].Get("hashes") != hashB || removes[0].Get("hashes") != hashA || removes[1].Get("hashes") != hashB {
		t.Fatalf("legacy tag selectors = add %#v remove %#v", adds, removes)
	}
}

func TestStatefulSettersRejectMissingAndInvalidBeforeMutation(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hash}}
	app := server.newApp()
	if err := app.SetRatioLimit(hash+"|aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "2"); err == nil {
		t.Fatal("SetRatioLimit accepted a missing hash")
	}
	if len(server.Forms["/api/v2/torrents/setShareLimits"]) != 0 {
		t.Fatal("missing hash sent a share-limits request")
	}
	infoCalls := server.InfoCalls
	if err := app.SetRatioLimit(hash, "abc"); err == nil {
		t.Fatal("SetRatioLimit accepted invalid value")
	}
	if server.InfoCalls != infoCalls {
		t.Fatal("invalid ratio fetched torrent state")
	}
	for _, selector := range []string{hash + "|all", "all|" + hash, hash + "||", "ALL"} {
		if err := app.SetRatioLimit(selector, "2"); err == nil {
			t.Fatalf("SetRatioLimit accepted malformed selector %q", selector)
		}
	}
	if len(server.Forms["/api/v2/torrents/setShareLimits"]) != 0 {
		t.Fatal("malformed selector sent a share-limits request")
	}
}

func TestSettersSupportAllAndGetTorrentInfoRejectsLists(t *testing.T) {
	hashA := "0123456789abcdef0123456789abcdef01234567"
	hashB := "1123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hashA, SequentialDL: false}, {Hash: hashB, SequentialDL: false}}
	app := server.newApp()
	if err := app.SetSequentialDownload("all", "true"); err != nil {
		t.Fatalf("SetSequentialDownload(all) err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/toggleSequentialDownload"][0].Get("hashes"); got != hashA+"|"+hashB {
		t.Fatalf("all selector = %q, want both hashes", got)
	}
	if _, err := app.GetTorrentInfo(hashA + "|" + hashB); err == nil || !strings.Contains(err.Error(), "single hash") {
		t.Fatalf("GetTorrentInfo list error = %v", err)
	}
}

func TestSetFieldUnknown(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	app := server.newApp()

	err := app.SetField(fullHash, "nonexistent", "value")
	if err == nil {
		t.Fatal("SetField unexpectedly succeeded for unknown field")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
		t.Fatalf("SetField err = %v, want CodedError with ExitBadArgs", err)
	}
}

func TestSetTagsAndSuperseed(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	app := server.newApp()

	if err := app.SetTags(fullHash, "linux,iso"); err != nil {
		t.Fatalf("SetTags err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setTags"][0].Get("tags"); got != "linux,iso" {
		t.Fatalf("tags = %q, want linux,iso", got)
	}

	if err := app.SetSuperseed(fullHash, "true"); err != nil {
		t.Fatalf("SetSuperseed err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setSuperSeeding"][0].Get("value"); got != "true" {
		t.Fatalf("superseed value = %q, want true", got)
	}
}

func TestSetTagsFallsBackToAddAndRemoveOnLegacyServer(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: fullHash, Tags: "linux,old"}}
	server.StatusQueue["/api/v2/torrents/setTags"] = []int{404}
	app := server.newApp()

	if err := app.SetTags(fullHash, "linux,new"); err != nil {
		t.Fatalf("SetTags legacy fallback err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/removeTags"][0].Get("tags"); got != "old" {
		t.Fatalf("removeTags tags = %q, want old", got)
	}
	if got := server.Forms["/api/v2/torrents/addTags"][0].Get("tags"); got != "new" {
		t.Fatalf("addTags tags = %q, want new", got)
	}
}

func TestLegacyTagReplacementSkipsEmptyOperations(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	for _, test := range []struct {
		name        string
		current     string
		desired     string
		wantAdds    int
		wantRemoves int
	}{
		{name: "add only", desired: "new", wantAdds: 1},
		{name: "remove only", current: "old", wantRemoves: 1},
		{name: "no-op", current: "old,new", desired: "new,old"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newQBTestServer(t)
			server.Torrents = []TorrentInfo{{Hash: fullHash, Tags: test.current}}
			server.StatusQueue["/api/v2/torrents/setTags"] = []int{http.StatusNotFound}

			if err := server.newApp().SetTags(fullHash, test.desired); err != nil {
				t.Fatalf("SetTags err = %v", err)
			}
			if got := len(server.Forms["/api/v2/torrents/addTags"]); got != test.wantAdds {
				t.Fatalf("addTags forms = %d, want %d", got, test.wantAdds)
			}
			if got := len(server.Forms["/api/v2/torrents/removeTags"]); got != test.wantRemoves {
				t.Fatalf("removeTags forms = %d, want %d", got, test.wantRemoves)
			}
		})
	}
}

func TestLegacyTagReplacementPropagatesMutationErrors(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	for _, test := range []struct {
		name       string
		current    string
		desired    string
		failedPath string
	}{
		{name: "add error", desired: "new", failedPath: "/api/v2/torrents/addTags"},
		{name: "remove error", current: "old", failedPath: "/api/v2/torrents/removeTags"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newQBTestServer(t)
			server.Torrents = []TorrentInfo{{Hash: fullHash, Tags: test.current}}
			server.StatusQueue["/api/v2/torrents/setTags"] = []int{http.StatusNotFound}
			server.StatusQueue[test.failedPath] = []int{http.StatusInternalServerError}

			err := server.newApp().SetTags(fullHash, test.desired)
			if err == nil {
				t.Fatal("SetTags unexpectedly succeeded after legacy mutation error")
			}
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitSetFail {
				t.Fatalf("SetTags error = %v, want ExitSetFail", err)
			}
			if got := len(server.Forms[test.failedPath]); got != 0 {
				t.Fatalf("failed %s recorded %d successful forms", test.failedPath, got)
			}
		})
	}
}

func TestSetTagsDoesNotFallbackForOtherErrors(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: fullHash, Tags: "linux"}}
	server.StatusQueue["/api/v2/torrents/setTags"] = []int{http.StatusInternalServerError}
	app := server.newApp()

	err := app.SetTags(fullHash, "new")
	if err == nil {
		t.Fatal("SetTags unexpectedly succeeded after server error")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitSetFail {
		t.Fatalf("SetTags error = %v, want ExitSetFail", err)
	}
	if server.InfoCalls != 0 {
		t.Fatalf("SetTags fetched torrent info after non-compatibility error: %d calls", server.InfoCalls)
	}
}

func TestSetAutoTMM(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	app := server.newApp()

	if err := app.SetAutoTMM(fullHash, "true"); err != nil {
		t.Fatalf("SetAutoTMM err = %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setAutoManagement"][0].Get("enable"); got != "true" {
		t.Fatalf("autotmm enable = %q, want true", got)
	}
}

func TestSetFieldInvalidToggleValue(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	app := server.newApp()

	err := app.SetSuperseed(fullHash, "maybe")
	if err == nil {
		t.Fatal("SetSuperseed unexpectedly accepted invalid value")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
		t.Fatalf("SetSuperseed err = %v, want ExitBadArgs", err)
	}
}
