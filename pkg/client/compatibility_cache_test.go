package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
)

func TestCompatibilityPairIsReusedByServerDisplay(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	app := server.newApp()
	versionCalls, apiCalls := 0, 0
	app.client.httpClient.Transport = countVersionRoundTrips(app.client.httpClient.Transport, &versionCalls, &apiCalls)

	if _, err := app.ServerDisplayFields(); err != nil {
		t.Fatalf("first ServerDisplayFields err = %v", err)
	}
	if _, err := app.ServerDisplayFields(); err != nil {
		t.Fatalf("second ServerDisplayFields err = %v", err)
	}
	if versionCalls != 1 || apiCalls != 1 {
		t.Fatalf("version calls = (%d, %d), want one app and one API probe", versionCalls, apiCalls)
	}
}

func TestCompatibilityCacheReusesPairAcrossAppsAndScopesByUser(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	configPath := filepath.Join(t.TempDir(), "config.json")
	app1 := &App{creds: server.creds(), configPath: configPath, compatibilityCacheEnabled: true}
	newAppDefaults(app1)
	app1.client = newClient(app1.creds, app1.Stderr)
	versionCalls, apiCalls := 0, 0
	app1.client.httpClient.Transport = countVersionRoundTrips(app1.client.httpClient.Transport, &versionCalls, &apiCalls)
	if err := app1.CheckCompatibility(); err != nil {
		t.Fatalf("first CheckCompatibility err = %v", err)
	}
	if versionCalls != 1 || apiCalls != 1 {
		t.Fatalf("first probes = (%d, %d), want one each", versionCalls, apiCalls)
	}

	app2 := &App{creds: server.creds(), configPath: configPath, compatibilityCacheEnabled: true}
	newAppDefaults(app2)
	app2.client = newClient(app2.creds, app2.Stderr)
	app2.client.httpClient.Transport = app1.client.httpClient.Transport
	if err := app2.CheckCompatibility(); err != nil {
		t.Fatalf("cached CheckCompatibility err = %v", err)
	}
	if versionCalls != 1 || apiCalls != 1 || app2.compatibilitySource != compatibilitySourceDisk {
		t.Fatalf("cached probes = (%d, %d), disk=%v; want no additional probes and disk source", versionCalls, apiCalls, app2.compatibilitySource == compatibilitySourceDisk)
	}

	app3 := &App{creds: server.creds(), configPath: configPath, compatibilityCacheEnabled: true}
	app3.creds.User = "other-user"
	newAppDefaults(app3)
	app3.client = newClient(app3.creds, app3.Stderr)
	app3.client.httpClient.Transport = app1.client.httpClient.Transport
	if err := app3.CheckCompatibility(); err != nil {
		t.Fatalf("different-user CheckCompatibility err = %v", err)
	}
	if versionCalls != 2 || apiCalls != 2 {
		t.Fatalf("different-user probes = (%d, %d), want a fresh pair", versionCalls, apiCalls)
	}
}

func TestCompatibilityCacheIsBounded(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "config.json")
	server := newQBTestServer(t)
	app := &App{creds: server.creds(), configPath: configPath, compatibilityCacheEnabled: true}
	for i := 0; i < compatibilityCacheMaxEntries+8; i++ {
		app.creds.User = fmt.Sprintf("user-%d", i)
		app.saveCompatibilityCache("v5.0.0", "2.11.0")
	}
	data, err := os.ReadFile(app.compatibilityCachePath())
	if err != nil {
		t.Fatalf("read compatibility cache: %v", err)
	}
	var cache compatibilityCacheData
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("decode compatibility cache: %v", err)
	}
	if len(cache.Entries) != compatibilityCacheMaxEntries {
		t.Fatalf("cache entries = %d, want %d", len(cache.Entries), compatibilityCacheMaxEntries)
	}
}

func TestUnsupportedAPIResponseInvalidatesCachedCompatibility(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	creds := server.creds()
	opts := Options{
		ConfigPath: filepath.Join(t.TempDir(), "config.json"), URL: creds.URL,
		User: creds.User, Pass: creds.Pass, UseCompatibilityCache: true,
	}
	if err := os.WriteFile(opts.ConfigPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := NewClientContext(t.Context(), &opts)
	if err != nil {
		t.Fatal(err)
	}
	first.Logout()
	second, err := NewClientContext(t.Context(), &opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Logout)
	if second.compatibilitySource != compatibilitySourceDisk {
		t.Fatal("expected the second app to reuse the version cache")
	}
	server.StatusQueue["/api/v2/transfer/info"] = []int{http.StatusMethodNotAllowed}
	if _, err := second.ServerDisplayFields(); err == nil {
		t.Fatal("unsupported transfer endpoint did not return an error")
	}
	if _, _, ok := second.loadCompatibilityCache(); ok {
		t.Fatal("unsupported API response left the stale version pair on disk")
	}
}

func countVersionRoundTrips(delegate http.RoundTripper, appCalls, apiCalls *int) http.RoundTripper {
	return qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v2/app/version":
			*appCalls++
		case "/api/v2/app/webapiVersion":
			*apiCalls++
		}
		return delegate.RoundTrip(req)
	})
}
