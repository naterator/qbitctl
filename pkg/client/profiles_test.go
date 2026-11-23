package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/configfile"
)

func TestAddProfileConfigCreatesExplicitDefaultAndEncryptsPassword(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := AddProfileConfig(path, " home ", Credentials{URL: "https://home.example/qbt", User: "admin", Pass: "secret"}, ""); err != nil {
		t.Fatal(err)
	}
	profiles, defaultName, err := ReadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != "home" || defaultName != "home" || !profiles[0].Default {
		t.Fatalf("profiles = %#v default=%q", profiles, defaultName)
	}
	object, err := configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := object["url"]; ok {
		t.Fatal("profile URL leaked to legacy root")
	}
	var nested configfile.Object
	if err := json.Unmarshal(object[profilesConfigKey], &nested); err != nil {
		t.Fatal(err)
	}
	var profile configfile.Object
	if err := json.Unmarshal(nested["home"], &profile); err != nil {
		t.Fatal(err)
	}
	var password string
	if err := json.Unmarshal(profile["password"], &password); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(password, "enc:v2:") {
		t.Fatalf("stored password = %q, want encrypted", password)
	}
	setup, err := ReadSetup(Options{ConfigPath: path, Profile: "home"})
	if err != nil {
		t.Fatal(err)
	}
	if setup.Profile != "home" || setup.URL != "https://home.example/qbt" || !setup.HasPassword {
		t.Fatalf("setup = %#v", setup)
	}
	creds, _, err := initAuthWithPath(Options{ConfigPath: path, Profile: "home"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Pass != "secret" || creds.URL != "https://home.example/qbt" {
		t.Fatalf("loaded credentials = %#v", creds)
	}
}

func TestAddProfileConfigMigratesLegacyPlaintextPassword(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"url":"https://legacy.example","user":"admin","password":"old-secret","default_search_plugin":"legacy"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileConfig(path, "new", Credentials{URL: "https://new.example", User: "admin", Pass: "new-secret"}, ""); err != nil {
		t.Fatal(err)
	}
	object, err := configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := object["url"]; ok {
		t.Fatal("legacy URL remained at root")
	}
	profiles, defaultName, err := ReadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if defaultName != DefaultProfileName || len(profiles) != 2 {
		t.Fatalf("profiles=%#v default=%q", profiles, defaultName)
	}
	legacy, _, _, err := selectProfileObject(object, DefaultProfileName)
	if err != nil || legacy != DefaultProfileName {
		t.Fatalf("legacy selection = %q, %v", legacy, err)
	}
	creds, _, err := initAuthWithPath(Options{ConfigPath: path, Profile: DefaultProfileName}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Pass != "old-secret" || creds.URL != "https://legacy.example" {
		t.Fatalf("migrated credentials = %#v", creds)
	}
	if got, err := ReadDefaultSearchPluginForProfile(path, DefaultProfileName); err != nil || got != "legacy" {
		t.Fatalf("migrated search preference = %q, %v", got, err)
	}
}

func TestRemoveProfileChoosesNewDefault(t *testing.T) {
	withMockMasterKey(t)
	path := filepath.Join(t.TempDir(), "config.json")
	for _, name := range []string{"zeta", "alpha"} {
		if err := AddProfileConfig(path, name, Credentials{URL: "https://" + name + ".example", User: "admin", Pass: name}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetDefaultProfile(path, "zeta"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfile(path, " zeta "); err != nil {
		t.Fatal(err)
	}
	profiles, defaultName, err := ReadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != "alpha" || defaultName != "alpha" {
		t.Fatalf("profiles=%#v default=%q", profiles, defaultName)
	}
}

func TestSearchPreferenceIsPerProfileAndPathScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	root := configfile.Object{}
	profiles := configfile.Object{}
	for _, profile := range []struct {
		name string
		url  string
	}{
		{"one", "https://qbt.example/one"},
		{"two", "https://qbt.example/two"},
	} {
		encoded, err := json.Marshal(configfile.Object{"url": json.RawMessage(`"` + profile.url + `"`), "user": json.RawMessage(`"admin"`)})
		if err != nil {
			t.Fatal(err)
		}
		profiles[profile.name] = encoded
	}
	root[profilesConfigKey], _ = json.Marshal(profiles)
	root[defaultProfileConfigKey] = json.RawMessage(`"one"`)
	if err := configfile.Update(path, func(object configfile.Object) (bool, error) {
		for k, v := range root {
			object[k] = v
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaultSearchPluginForProfile(path, "one", "jackett"); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaultSearchPluginForProfile(path, "two", "prowlarr"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadDefaultSearchPluginForProfile(path, "one"); got != "jackett" {
		t.Fatalf("one preference = %q", got)
	}
	if got, _ := ReadDefaultSearchPluginForProfile(path, "two"); got != "prowlarr" {
		t.Fatalf("two preference = %q", got)
	}
	pathMismatch := &App{configPath: path, profileName: "one", creds: Credentials{URL: "https://qbt.example/two", User: "admin"}}
	if err := pathMismatch.ResetDefaultSearchPluginIfMatches("jackett"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadDefaultSearchPluginForProfile(path, "one"); got != "jackett" {
		t.Fatalf("path mismatch cleared preference: %q", got)
	}
	matching := &App{configPath: path, profileName: "one", creds: Credentials{URL: "https://qbt.example/one/", User: "admin"}}
	if err := matching.ResetDefaultSearchPluginIfMatches("jackett"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadDefaultSearchPluginForProfile(path, "one"); got != DefaultSearchPlugins {
		t.Fatalf("matching path did not clear preference: %q", got)
	}
}
