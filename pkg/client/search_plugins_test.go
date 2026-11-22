package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type searchPluginsFixture struct {
	status int
	body   string
	calls  int
}

func newSearchPluginsApp(t *testing.T, fixture *searchPluginsFixture) *App {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/search/plugins" {
			t.Fatalf("search plugins request = %s %s, want GET /api/v2/search/plugins", r.Method, r.URL)
		}
		status := fixture.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, fixture.body)
	}))
	t.Cleanup(server.Close)
	creds := Credentials{URL: server.URL, User: "admin", Pass: "secret"}
	app := &App{creds: creds}
	newAppDefaults(app)
	app.client = newClient(creds, io.Discard)
	return app
}

func TestSearchPluginsDecodesCatalog(t *testing.T) {
	fixture := &searchPluginsFixture{body: `[
        {"enabled":true,"fullName":"Jackett","name":"jackett"},
        {"enabled":false,"fullName":"Prowlarr","name":"prowlarr"}
    ]`}
	plugins, err := newSearchPluginsApp(t, fixture).SearchPlugins()
	if err != nil {
		t.Fatalf("SearchPlugins returned error: %v", err)
	}
	want := []SearchPlugin{
		{Name: "jackett", FullName: "Jackett", Enabled: true},
		{Name: "prowlarr", FullName: "Prowlarr", Enabled: false},
	}
	if len(plugins) != len(want) || plugins[0] != want[0] || plugins[1] != want[1] {
		t.Fatalf("SearchPlugins = %#v, want %#v", plugins, want)
	}
	if fixture.calls != 1 {
		t.Fatalf("SearchPlugins requests = %d, want 1", fixture.calls)
	}
}

func TestSearchPluginsRejectsInvalidResponses(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		code int
	}{
		{name: "malformed JSON", body: "not-json"},
		{name: "object instead of array", body: `{}`},
		{name: "null instead of array", body: "null"},
		{name: "HTTP error", code: http.StatusInternalServerError, body: `server failure`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &searchPluginsFixture{status: tt.code, body: tt.body}
			_, err := newSearchPluginsApp(t, fixture).SearchPlugins()
			if err == nil {
				t.Fatal("SearchPlugins unexpectedly succeeded")
			}
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitFetchFail {
				t.Fatalf("SearchPlugins error = %v, want ExitFetchFail", err)
			}
		})
	}
}

func TestResolveSearchPluginSelection(t *testing.T) {
	catalog := []SearchPlugin{
		{Name: "jackett", FullName: "Jackett", Enabled: true},
		{Name: "JACKETT", FullName: "Upper Jackett", Enabled: true},
		{Name: "display-id", FullName: "jackett", Enabled: true},
		{Name: "prowlarr", FullName: "Prowlarr", Enabled: true},
		{Name: "other", FullName: "Shared display name", Enabled: false},
		{Name: "another", FullName: "Shared display name", Enabled: false},
	}
	for _, tt := range []struct {
		selection string
		want      string
	}{
		{selection: "enabled", want: "enabled"},
		{selection: "ALL", want: "all"},
		{selection: "multi", want: "multi"},
		{selection: "jackett", want: "jackett"},
		{selection: "JACKETT", want: "JACKETT"},
		{selection: "Prowlarr", want: "prowlarr"},
		{selection: "Prowlarr | jackett", want: "prowlarr|jackett"},
		{selection: "jackett|jackett|JACKETT|JACKETT", want: "jackett|JACKETT"},
	} {
		t.Run(tt.selection, func(t *testing.T) {
			got, err := ResolveSearchPluginSelection(tt.selection, catalog)
			if err != nil || got != tt.want {
				t.Fatalf("ResolveSearchPluginSelection(%q) = %q, %v; want %q", tt.selection, got, err, tt.want)
			}
		})
	}

	for _, selection := range []string{"missing", "jackett||prowlarr", "Shared display name", "jAcKeTt"} {
		t.Run("reject "+selection, func(t *testing.T) {
			if _, err := ResolveSearchPluginSelection(selection, catalog); err == nil {
				t.Fatalf("ResolveSearchPluginSelection(%q) unexpectedly succeeded", selection)
			}
		})
	}
}

func TestResolveSearchPluginsSkipsDiscoveryForReservedSelectors(t *testing.T) {
	fixture := &searchPluginsFixture{body: `not needed`}
	app := newSearchPluginsApp(t, fixture)
	for _, selection := range []string{"enabled", "all", "multi", "ENABLED"} {
		got, err := app.ResolveSearchPlugins(selection)
		if err != nil {
			t.Fatalf("ResolveSearchPlugins(%q) returned error: %v", selection, err)
		}
		if want := strings.ToLower(selection); got != want {
			t.Fatalf("ResolveSearchPlugins(%q) = %q, want %q", selection, got, want)
		}
	}
	if fixture.calls != 0 {
		t.Fatalf("reserved selectors triggered %d discovery requests", fixture.calls)
	}
}

func TestResolveSearchPluginsUsesCatalogAndPreservesLegacyIDsWhenUnavailable(t *testing.T) {
	fixture := &searchPluginsFixture{body: `[{"enabled":true,"fullName":"Jackett","name":"jackett"}]`}
	app := newSearchPluginsApp(t, fixture)
	got, err := app.ResolveSearchPlugins("Jackett")
	if err != nil || got != "jackett" {
		t.Fatalf("ResolveSearchPlugins display name = %q, %v; want jackett", got, err)
	}
	if fixture.calls != 1 {
		t.Fatalf("catalog requests = %d, want 1", fixture.calls)
	}

	fixture = &searchPluginsFixture{status: http.StatusNotFound, body: `not supported`}
	app = newSearchPluginsApp(t, fixture)
	got, err = app.ResolveSearchPlugins("legacy-id|other-id|legacy-id|LEGACY-ID")
	if err != nil || got != "legacy-id|other-id|LEGACY-ID" {
		t.Fatalf("legacy selector fallback = %q, %v; want deduplicated IDs", got, err)
	}

	fixture = &searchPluginsFixture{status: http.StatusNotFound, body: `not supported`}
	app = newSearchPluginsApp(t, fixture)
	if _, err = app.ResolveSearchPlugins("Legacy Display Name"); err == nil {
		t.Fatal("display name unexpectedly bypassed unavailable catalog")
	}

	fixture = &searchPluginsFixture{status: http.StatusInternalServerError, body: `server failure`}
	app = newSearchPluginsApp(t, fixture)
	if _, err = app.ResolveSearchPlugins("legacy-id"); err == nil {
		t.Fatal("selector unexpectedly bypassed an internal server error")
	}

	fixture = &searchPluginsFixture{body: `not-json`}
	app = newSearchPluginsApp(t, fixture)
	if _, err = app.ResolveSearchPlugins("legacy-id"); err == nil {
		t.Fatal("selector unexpectedly bypassed a malformed catalog")
	}
}

func TestResolveSearchPluginSelectionSanitizesSelectorErrors(t *testing.T) {
	_, err := ResolveSearchPluginSelection("missing\n\x1b[31m", []SearchPlugin{
		{Name: "jackett", FullName: "Jackett"},
	})
	if err == nil {
		t.Fatal("missing selector unexpectedly succeeded")
	}
	if strings.ContainsAny(err.Error(), "\r\n\x1b") {
		t.Fatalf("selector error contains terminal/control characters: %q", err)
	}
}
