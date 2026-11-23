package client

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/configfile"
)

func decodeSetupProfileObject(t *testing.T, raw json.RawMessage) configfile.Object {
	t.Helper()
	var object configfile.Object
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		if err == nil {
			t.Fatalf("decoded JSON value is not an object: %s", raw)
		}
		t.Fatalf("decode JSON object: %v", err)
	}
	return object
}

func setupJSONValue(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode JSON value: %v", err)
	}
	return value
}

func TestConnectAndSaveProfileSetupPreservesProfileBoundaries(t *testing.T) {
	withMockMasterKey(t)
	fixture := newQBTestServer(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := &http.Server{Handler: http.HandlerFunc(fixture.Handle)}
	go func() {
		_ = httpServer.Serve(listener)
	}()
	t.Cleanup(func() { _ = httpServer.Close() })
	serverURL := "http://" + listener.Addr().String()

	caFixture := newCATestServer(t, []string{"127.0.0.1"})
	caPath := writeTestCAPEM(t, caFixture, "profile-ca.pem")
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"profiles":{"sibling":{"url":"http://sibling.example","user":"sibling","password":"enc:v2:sibling","default_search_plugin":"prowlarr"},"work":{"url":"http://old.example","user":"old-user","password":"enc:v2:old","default_search_plugin":"jackett"}},"default_profile":"sibling","default_search_plugin":"root-provider","data_units":"bytes","ui":{"tui_default":true,"theme":"dark"},"future":{"keep":true}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	before, err := configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeProfiles := decodeSetupProfileObject(t, before[profilesConfigKey])
	beforeSibling := setupJSONValue(t, beforeProfiles["sibling"])
	beforeUI := setupJSONValue(t, before["ui"])

	app, err := ConnectAndSaveProfileSetup(context.Background(), path, "work", Credentials{
		URL:  serverURL,
		User: "updated-user",
		Pass: "updated-password",
	}, caPath)
	if err != nil {
		t.Fatalf("ConnectAndSaveProfileSetup returned error: %v", err)
	}
	if app == nil {
		t.Fatal("ConnectAndSaveProfileSetup returned a nil app")
	}
	if app.CAFile != caPath {
		t.Fatalf("effective CA file = %q, want %q", app.CAFile, caPath)
	}
	app.Logout()

	after, err := configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	afterProfiles := decodeSetupProfileObject(t, after[profilesConfigKey])
	updated := decodeSetupProfileObject(t, afterProfiles["work"])
	if got := string(updated["url"]); got != `"`+serverURL+`"` {
		t.Fatalf("updated profile URL = %s, want %q", got, serverURL)
	}
	if got := string(updated["user"]); got != `"updated-user"` {
		t.Fatalf("updated profile user = %s, want updated-user", got)
	}
	var password string
	if err := json.Unmarshal(updated["password"], &password); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(password, "enc:") {
		t.Fatalf("updated profile password = %q, want encrypted value", password)
	}
	var savedCA string
	if err := json.Unmarshal(updated[profileCAFileKey], &savedCA); err != nil {
		t.Fatal(err)
	}
	if savedCA != caPath {
		t.Fatalf("updated profile CA file = %q, want %q", savedCA, caPath)
	}
	for _, key := range []string{profilesConfigKey, "ui"} {
		if _, ok := updated[key]; ok {
			t.Fatalf("updated profile unexpectedly contains nested %q key: %s", key, updated[key])
		}
	}

	if got := setupJSONValue(t, afterProfiles["sibling"]); !reflect.DeepEqual(got, beforeSibling) {
		t.Fatalf("sibling profile changed: got %#v, want %#v", got, beforeSibling)
	}
	if got := setupJSONValue(t, after["ui"]); !reflect.DeepEqual(got, beforeUI) {
		t.Fatalf("root UI preferences changed: got %#v, want %#v", got, beforeUI)
	}
	for _, key := range []string{defaultProfileConfigKey, "default_search_plugin", "data_units", "future"} {
		if got, want := setupJSONValue(t, after[key]), setupJSONValue(t, before[key]); !reflect.DeepEqual(got, want) {
			t.Fatalf("root %s changed: got %#v, want %#v", key, got, want)
		}
	}
	for _, key := range []string{"url", "user", "password", profileCAFileKey} {
		if _, ok := after[key]; ok {
			t.Fatalf("named-profile save leaked %q into the root object", key)
		}
	}
}
