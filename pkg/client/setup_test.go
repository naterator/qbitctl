package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
)

func TestReadSetupReadsMetadataWithoutDecryptingOrRewriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"url":"http://embedded:do-not-show@example.test/qbt/","user":"admin","password":"enc:v2:not-a-real-password","ui":{"queue":{"sort":"progress-desc"},"search":{"sort":"seeders-desc"},"tui_default":true},"future":{"keep":true}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	setup, err := ReadSetup(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("ReadSetup returned error: %v", err)
	}
	if setup.Path != path {
		t.Fatalf("Path = %q, want %q", setup.Path, path)
	}
	if setup.URL != "http://example.test/qbt/" {
		t.Fatalf("URL = %q, want userinfo-redacted URL", setup.URL)
	}
	if setup.User != "admin" || !setup.HasPassword || !setup.TUIDefault {
		t.Fatalf("metadata = %#v", setup)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatalf("ReadSetup rewrote config: %s", after)
	}

	override, err := ReadSetup(Options{ConfigPath: path, URL: "http://override.example", User: "override", Pass: "provided"})
	if err != nil {
		t.Fatalf("ReadSetup override returned error: %v", err)
	}
	if override.URL != "http://override.example" || override.User != "override" || !override.HasPassword {
		t.Fatalf("overridden metadata = %#v", override)
	}
}

func TestReadSetupSelectsExplicitMissingPathAndDefault(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nested", "config.json")
	setup, err := ReadSetup(Options{ConfigPath: missing})
	if err != nil {
		t.Fatalf("ReadSetup missing explicit path returned error: %v", err)
	}
	if setup.Path != missing || setup.Ready() {
		t.Fatalf("missing setup = %#v", setup)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	defaultPath := filepath.Join(home, ".config", defaultAuthDir, defaultAuthFile)
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultPath, []byte(`{"url":"http://default.example","user":"admin","password":"saved"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	setup, err = ReadSetup(Options{})
	if err != nil {
		t.Fatalf("ReadSetup default returned error: %v", err)
	}
	if setup.Path != defaultPath || !setup.Ready() {
		t.Fatalf("default setup = %#v", setup)
	}
}

func TestSetupReady(t *testing.T) {
	if !(Setup{Path: "/tmp/config.json", URL: "http://example.test", User: "admin", HasPassword: true}).Ready() {
		t.Fatal("complete setup was not ready")
	}
	for _, setup := range []Setup{
		{Path: "/tmp/config.json", URL: "", User: "admin", HasPassword: true},
		{Path: "/tmp/config.json", URL: "http://example.test", User: "", HasPassword: true},
		{Path: "/tmp/config.json", URL: "http://example.test", User: "admin", HasPassword: false},
		{URL: "http://example.test", User: "admin", HasPassword: true},
	} {
		if setup.Ready() {
			t.Fatalf("incomplete setup was ready: %#v", setup)
		}
	}
}

func TestSetTUIDefaultPreservesConfigWithoutCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"url":"http://example.test","user":"admin","ui":{"queue":{"sort":"progress-desc"},"search":{"sort":"seeders-desc"}},"future":{"keep":true}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetTUIDefault(path, true); err != nil {
		t.Fatalf("SetTUIDefault returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	var ui map[string]json.RawMessage
	if err := json.Unmarshal(object["ui"], &ui); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	if err := json.Unmarshal(ui["tui_default"], &enabled); err != nil || !enabled {
		t.Fatalf("tui_default = %s, %v", ui["tui_default"], err)
	}
	for _, key := range []string{"queue", "search"} {
		if _, ok := ui[key]; !ok {
			t.Fatalf("SetTUIDefault dropped ui.%s: %s", key, data)
		}
	}
	if _, ok := object["future"]; !ok {
		t.Fatalf("SetTUIDefault dropped unknown fields: %s", data)
	}
}

func TestSetTUIDefaultCreatesConfigWithoutCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	if err := SetTUIDefault(path, false); err != nil {
		t.Fatalf("SetTUIDefault returned error: %v", err)
	}
	setup, err := ReadSetup(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if setup.Path != path || setup.TUIDefault {
		t.Fatalf("created setup = %#v", setup)
	}
}

func TestConnectAndSaveSetupAuthenticatesBeforeSavingAndPreservesFields(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"ui":{"queue":{"sort":"progress-desc"},"search":{"sort":"seeders-desc"}},"future":{"keep":true}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	app, err := ConnectAndSaveSetup(context.Background(), path, server.creds(), true)
	if err != nil {
		t.Fatalf("ConnectAndSaveSetup returned error: %v", err)
	}
	if app == nil || app.ConfigPath() != path {
		t.Fatalf("app/config path = %#v, %q", app, app.ConfigPath())
	}
	app.Logout()
	if server.LoginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", server.LoginCalls)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	var saved configFile
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.URL != server.creds().URL || saved.User != server.creds().User || !strings.HasPrefix(saved.Password, "enc:") {
		t.Fatalf("saved auth = %#v", saved)
	}
	var ui map[string]json.RawMessage
	if err := json.Unmarshal(object["ui"], &ui); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	if err := json.Unmarshal(ui["tui_default"], &enabled); err != nil || !enabled {
		t.Fatalf("saved tui_default = %s, %v", ui["tui_default"], err)
	}
	for _, key := range []string{"queue", "search"} {
		if _, ok := ui[key]; !ok {
			t.Fatalf("saved config dropped ui.%s: %s", key, data)
		}
	}
	if _, ok := object["future"]; !ok {
		t.Fatalf("saved config dropped unknown field: %s", data)
	}
}

func TestConnectAndSaveSetupRequiresPasswordForChangedOrigin(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveAuthFile(path, Credentials{URL: "http://old.example", User: "admin", Pass: "secret"}); err != nil {
		t.Fatal(err)
	}
	_, err := ConnectAndSaveSetup(context.Background(), path, Credentials{URL: "http://new.example", User: "admin"}, true)
	if !errors.Is(err, ErrSetupPasswordRequired) {
		t.Fatalf("changed-origin error = %v, want ErrSetupPasswordRequired", err)
	}
}

func TestConnectAndSaveSetupRejectsUnsupportedSavedPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"url":"http://old.example","user":"admin","password":"enc:v1:not-decrypted:payload"}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ConnectAndSaveSetup(context.Background(), path, Credentials{URL: "http://old.example", User: "admin"}, false)
	if !errors.Is(err, errUnsupportedPasswordFormat) {
		t.Fatalf("ConnectAndSaveSetup error = %v, want unsupported-password error", err)
	}
	if !strings.Contains(err.Error(), "qbitctl config") {
		t.Fatalf("ConnectAndSaveSetup error = %v, want reconfiguration guidance", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != original {
		t.Fatalf("unsupported config was rewritten: %s", after)
	}
}

func TestConnectAndSaveSetupDoesNotPersistSavedCredentialsAfterConcurrentChange(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveAuthFile(path, Credentials{URL: "http://example.test", User: "admin", Pass: "secret"}); err != nil {
		t.Fatal(err)
	}
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	// Use a custom HTTP factory so the concurrent rewrite occurs after login
	// but before compatibility validation completes.
	oldFactory := httpClientFactory
	httpClientFactory = func(jar http.CookieJar) *http.Client {
		return &http.Client{Jar: jar, Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/v2/auth/login" {
				if err := saveAuthFile(path, Credentials{URL: "http://example.test", User: "new-user", Pass: "new-password"}); err != nil {
					t.Fatalf("concurrent save failed: %v", err)
				}
			}
			return server.roundTrip(req), nil
		})}
	}
	t.Cleanup(func() { httpClientFactory = oldFactory })

	_, err := ConnectAndSaveSetup(context.Background(), path, Credentials{URL: "http://example.test", User: "admin"}, true)
	if !errors.Is(err, ErrSetupChanged) {
		t.Fatalf("concurrent-change error = %v, want ErrSetupChanged", err)
	}
	loaded, _, err := loadAuthFile(path, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.User != "new-user" || loaded.Pass != "new-password" {
		t.Fatalf("concurrent credentials were overwritten: %#v", loaded)
	}
}

func TestInteractiveConfigAtUsesSelectedPathAndRejectsEmbeddedCredentials(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	creds := server.creds()
	path := filepath.Join(t.TempDir(), "config.json")
	input := strings.NewReader(creds.URL + "\n" + creds.User + "\n" + creds.Pass + "\n")
	saved, err := InteractiveConfigAt(input, io.Discard, nil, path)
	if err != nil || saved != path {
		t.Fatalf("InteractiveConfigAt = %q, %v", saved, err)
	}
	loaded, ok, err := loadAuthFile(path, io.Discard)
	if err != nil || !ok || loaded != creds || server.LoginCalls != 1 {
		t.Fatalf("saved valid credentials = %t, lookup success = %t, login calls = %d, error = %v", loaded == creds, ok, server.LoginCalls, err)
	}

	_, err = InteractiveConfigAt(strings.NewReader("https://alice:secret@qb.example.test\nalice\nsecret\n"), io.Discard, nil, filepath.Join(t.TempDir(), "bad.json"))
	if err == nil {
		t.Fatal("InteractiveConfigAt accepted embedded URL credentials")
	}
}

func TestInteractiveConfigContextVerifiesAndPreservesProfile(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "selected", "config.json")
	original := `{"url":"http://old.example","user":"old-user","password":"enc:v2:legacy","ui":{"tui_default":true,"queue":{"sort":"progress-desc"}},"future":{"keep":true}}`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	server.StatusQueue["/api/v2/auth/login"] = []int{http.StatusUnauthorized}
	rejected := strings.NewReader(server.creds().URL + "\nadmin\nwrong-password\n")
	if _, err := InteractiveConfigContext(context.Background(), rejected, io.Discard, nil, path); err == nil {
		t.Fatal("setup accepted invalid credentials")
	}
	afterFailure, err := os.ReadFile(path)
	if err != nil || string(afterFailure) != original {
		t.Fatalf("setup changed the profile after rejected login, read error = %v", err)
	}

	input := strings.NewReader(server.creds().URL + "\nadmin\nsecret\n")
	saved, err := InteractiveConfigContext(context.Background(), input, io.Discard, nil, path)
	if err != nil {
		t.Fatalf("InteractiveConfigContext returned error: %v", err)
	}
	if saved != path {
		t.Fatalf("saved path = %q, want %q", saved, path)
	}
	if server.LoginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", server.LoginCalls)
	}

	loaded, ok, err := loadAuthFile(path, io.Discard)
	if err != nil || !ok {
		t.Fatalf("loadAuthFile = %#v, %v, %v", loaded, ok, err)
	}
	if loaded.URL != server.creds().URL || loaded.User != "admin" || loaded.Pass != "secret" {
		t.Fatalf("saved credentials = %#v", loaded)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	var ui map[string]json.RawMessage
	if err := json.Unmarshal(object["ui"], &ui); err != nil {
		t.Fatal(err)
	}
	var tuiDefault bool
	if err := json.Unmarshal(ui["tui_default"], &tuiDefault); err != nil || !tuiDefault {
		t.Fatalf("tui_default = %s, %v", ui["tui_default"], err)
	}
	if _, ok := ui["queue"]; !ok {
		t.Fatalf("InteractiveConfigContext dropped existing UI profile: %s", data)
	}
	if _, ok := object["future"]; !ok {
		t.Fatalf("InteractiveConfigContext dropped unknown profile fields: %s", data)
	}
}

func TestInteractiveConfigContextPromptsForPathOnlyWhenEmpty(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	tmp := t.TempDir()
	withWorkingDirectory(t, tmp)
	withHomeDirectory(t, filepath.Join(tmp, "home"))
	path := filepath.Join(tmp, "chosen", "config.json")

	input := strings.NewReader(server.creds().URL + "\nadmin\nsecret\n" + path + "\n")
	saved, err := InteractiveConfigContext(context.Background(), input, io.Discard, nil, "")
	if err != nil {
		t.Fatalf("empty-path InteractiveConfigContext returned error: %v", err)
	}
	if saved != path {
		t.Fatalf("empty-path saved path = %q, want %q", saved, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("selected config was not written: %v", err)
	}
}

func TestInteractiveConfigContextRejectsInvalidURLBeforeConnecting(t *testing.T) {
	withMockMasterKey(t)
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	input := strings.NewReader("http://qbt.test/?query=1\nhttp://qbt.test\nadmin\nsecret\n")
	_, err := InteractiveConfigContext(context.Background(), input, io.Discard, nil, path)
	if err != nil {
		t.Fatalf("InteractiveConfigContext returned error after correction: %v", err)
	}
	if server.LoginCalls != 1 {
		t.Fatalf("login calls = %d, want one after URL correction", server.LoginCalls)
	}
}
