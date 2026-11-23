package client

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestShareLimitPolicyParsers(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "mode default", input: "default", want: shareLimitsModeDefault},
		{name: "mode all alias", input: "match-all", want: shareLimitsModeAll},
		{name: "mode any wire", input: "MatchAny", want: shareLimitsModeAny},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseShareLimitsMode(test.input)
			if err != nil || got != test.want {
				t.Fatalf("parseShareLimitsMode(%q) = %q, %v; want %q", test.input, got, err, test.want)
			}
		})
	}
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "action default", input: "default", want: shareLimitActionDefault},
		{name: "action remove content alias", input: "remove-with-content", want: shareLimitActionRemoveWithContent},
		{name: "action super seeding wire", input: "EnableSuperSeeding", want: shareLimitActionEnableSuperseeding},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseShareLimitAction(test.input)
			if err != nil || got != test.want {
				t.Fatalf("parseShareLimitAction(%q) = %q, %v; want %q", test.input, got, err, test.want)
			}
		})
	}
	if !IsDestructiveShareLimitAction("remove") || !IsDestructiveShareLimitAction("remove-with-content") {
		t.Fatal("remove actions must be destructive")
	}
	if IsDestructiveShareLimitAction("stop") || IsDestructiveShareLimitAction("enable-super-seeding") {
		t.Fatal("non-removing actions must not be destructive")
	}
}

func TestSetInactiveSeedtimeLimitUsesDurationAndPreservesPolicy(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{
		Hash:              hash,
		RatioLimit:        1.5,
		SeedingTimeLimit:  60,
		InactiveSeedingTL: 90,
		ShareLimitsMode:   "MatchAll",
		ShareLimitAction:  "Stop",
	}}
	app := server.newApp()
	if err := app.SetInactiveSeedtimeLimit(hash, "2h"); err != nil {
		t.Fatalf("SetInactiveSeedtimeLimit: %v", err)
	}
	form := server.Forms["/api/v2/torrents/setShareLimits"][0]
	if got := form.Get("inactiveSeedingTimeLimit"); got != "120" {
		t.Fatalf("inactive limit = %q, want 120 minutes", got)
	}
	if form.Get("ratioLimit") != "1.500000" || form.Get("seedingTimeLimit") != "60" ||
		form.Get("shareLimitsMode") != "MatchAll" || form.Get("shareLimitAction") != "Stop" {
		t.Fatalf("policy was not preserved: %v", form)
	}
}

func TestSetShareLimitPolicyFieldsAndVersionGates(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.AppVersion = "v5.3.0"
	server.Torrents = []TorrentInfo{{Hash: hash, RatioLimit: 1}}
	app := server.newApp()
	if err := app.SetShareLimitsMode(hash, "match-any"); err != nil {
		t.Fatalf("SetShareLimitsMode: %v", err)
	}
	if err := app.SetShareLimitAction(hash, "remove-with-content"); err != nil {
		t.Fatalf("SetShareLimitAction: %v", err)
	}
	forms := server.Forms["/api/v2/torrents/setShareLimits"]
	if len(forms) != 2 || forms[0].Get("shareLimitsMode") != "MatchAny" || forms[1].Get("shareLimitAction") != "RemoveWithContent" {
		t.Fatalf("policy forms = %#v", forms)
	}

	legacy := newQBTestServer(t)
	legacy.AppVersion = "v5.0.0"
	legacy.Torrents = []TorrentInfo{{Hash: hash, RatioLimit: 1}}
	legacyApp := legacy.newApp()
	if err := legacyApp.SetShareLimitsMode(hash, "match-all"); err == nil || !strings.Contains(err.Error(), "requires qBittorrent 5.3.0") {
		t.Fatalf("5.0 mode error = %v, want version explanation", err)
	}
	if len(legacy.Forms["/api/v2/torrents/setShareLimits"]) != 0 {
		t.Fatal("unsupported mode sent a mutation")
	}

	v52 := newQBTestServer(t)
	v52.AppVersion = "v5.2.4"
	v52.Torrents = []TorrentInfo{{Hash: hash, RatioLimit: 1}}
	if err := v52.newApp().SetShareLimitAction(hash, "enable-super-seeding"); err != nil {
		t.Fatalf("5.2 share-limit action: %v", err)
	}
	forms = v52.Forms["/api/v2/torrents/setShareLimits"]
	if len(forms) != 1 || forms[0].Get("shareLimitAction") != "EnableSuperSeeding" {
		t.Fatalf("5.2 action forms = %#v", forms)
	}
}

func TestMissingInactiveSeedtimeUsesGlobalDefault(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	var omitted TorrentInfo
	if err := json.Unmarshal([]byte(`{"hash":"a"}`), &omitted); err != nil {
		t.Fatal(err)
	}
	if got := torrentFieldValue(omitted, "inactive-seedtime-limit"); got != "-2" {
		t.Fatalf("omitted inactive field displays %q, want -2", got)
	}
	server := newQBTestServer(t)
	server.InfoRawBody = `[ {"hash":"0123456789abcdef0123456789abcdef01234567","ratio_limit":1.0} ]`
	app := server.newApp()
	if err := app.SetRatioLimit(hash, "2"); err != nil {
		t.Fatalf("SetRatioLimit: %v", err)
	}
	form := server.Forms["/api/v2/torrents/setShareLimits"][0]
	if got := form.Get("inactiveSeedingTimeLimit"); got != "-2" {
		t.Fatalf("missing inactive limit = %q, want -2 global", got)
	}

	server = newQBTestServer(t)
	server.InfoRawBody = `[ {"hash":"0123456789abcdef0123456789abcdef01234567","ratio_limit":1.0,"inactive_seeding_time_limit":0} ]`
	app = server.newApp()
	if err := app.SetRatioLimit(hash, "2"); err != nil {
		t.Fatalf("SetRatioLimit with explicit zero: %v", err)
	}
	if got := server.Forms["/api/v2/torrents/setShareLimits"][0].Get("inactiveSeedingTimeLimit"); got != "0" {
		t.Fatalf("explicit zero inactive limit = %q, want 0", got)
	}
}

func TestGroupedShareLimitErrorReportsCompletedFailedAndSkipped(t *testing.T) {
	hashA := "0123456789abcdef0123456789abcdef01234567"
	hashB := "1123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{
		{Hash: hashA, RatioLimit: 1, ShareLimitsMode: "MatchAll"},
		{Hash: hashB, RatioLimit: 2, ShareLimitsMode: "MatchAny"},
	}
	server.StatusQueue["/api/v2/torrents/setShareLimits"] = []int{200, 500}
	err := server.newApp().SetRatioLimit(hashA+"|"+hashB, "3")
	if err == nil || !strings.Contains(err.Error(), "confirmed: "+hashA) ||
		!strings.Contains(err.Error(), "failed group: "+hashB) {
		t.Fatalf("grouped error = %v", err)
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitSetFail {
		t.Fatalf("grouped error code = %#v, want ExitSetFail", coded)
	}
}

func TestGroupedLegacyTagErrorReportsStepAndSkipped(t *testing.T) {
	hashA := "0123456789abcdef0123456789abcdef01234567"
	hashB := "1123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Hash: hashA, Tags: "old"}, {Hash: hashB, Tags: "keep"}}
	server.StatusQueue["/api/v2/torrents/setTags"] = []int{404}
	server.StatusQueue["/api/v2/torrents/addTags"] = []int{200, 200}
	server.StatusQueue["/api/v2/torrents/removeTags"] = []int{500}
	err := server.newApp().SetTags(hashA+"|"+hashB, "new")
	if err == nil || !strings.Contains(err.Error(), "set tags (remove), add completed") ||
		!strings.Contains(err.Error(), "failed group: "+hashA) || !strings.Contains(err.Error(), "skipped: "+hashB) {
		t.Fatalf("legacy tag grouped error = %v", err)
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitSetFail {
		t.Fatalf("legacy tag error code = %#v, want ExitSetFail", coded)
	}
}

func TestTorrentInfoInactivePresence(t *testing.T) {
	var missing, explicit TorrentInfo
	if err := json.Unmarshal([]byte(`{"hash":"a"}`), &missing); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"hash":"a","inactive_seeding_time_limit":0}`), &explicit); err != nil {
		t.Fatal(err)
	}
	if missing.inactiveSeedingTLPresent || !explicit.inactiveSeedingTLPresent {
		t.Fatalf("inactive presence = missing %t explicit %t", missing.inactiveSeedingTLPresent, explicit.inactiveSeedingTLPresent)
	}
	var reused TorrentInfo
	if err := json.Unmarshal([]byte(`{"inactive_seeding_time_limit":15}`), &reused); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"inactive_seeding_time_limit":null}`), &reused); err != nil {
		t.Fatal(err)
	}
	if reused.inactiveSeedingTLPresent || reused.InactiveSeedingTL != 0 {
		t.Fatalf("null inactive limit = %d, present=%t; want zero and absent", reused.InactiveSeedingTL, reused.inactiveSeedingTLPresent)
	}
}
