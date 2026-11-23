package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/configfile"
)

func TestValidateDefaultSearchPlugin(t *testing.T) {
	plugins := []SearchPlugin{
		{Name: "jackett", FullName: "Jackett", Enabled: true},
		{Name: "prowlarr", FullName: "Prowlarr", Enabled: false},
		{Name: "other", FullName: "Jackett", Enabled: true},
	}
	tests := []struct {
		name      string
		selection string
		want      string
	}{
		{name: "empty", selection: "", want: DefaultSearchPlugins},
		{name: "enabled group", selection: "enabled", want: DefaultSearchPlugins},
		{name: "enabled group case", selection: "ENABLED", want: DefaultSearchPlugins},
		{name: "all group", selection: "ALL", want: "all"},
		{name: "multi group rejected", selection: "multi", want: DefaultSearchPlugins},
		{name: "canonical enabled id", selection: "jackett", want: "jackett"},
		{name: "display name enabled id", selection: "Jackett", want: DefaultSearchPlugins},
		{name: "disabled id", selection: "prowlarr", want: DefaultSearchPlugins},
		{name: "unknown id", selection: "missing", want: DefaultSearchPlugins},
		{name: "multiple ids", selection: "jackett|other", want: DefaultSearchPlugins},
		{name: "control character", selection: "jack\nett", want: DefaultSearchPlugins},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateDefaultSearchPlugin(tt.selection, plugins); got != tt.want {
				t.Fatalf("ValidateDefaultSearchPlugin(%q) = %q, want %q", tt.selection, got, tt.want)
			}
		})
	}
}

func TestValidateDefaultSearchPluginRequiresExactID(t *testing.T) {
	plugins := []SearchPlugin{{Name: "Jackett", FullName: "Jackett provider", Enabled: true}}
	if got := ValidateDefaultSearchPlugin("jackett", plugins); got != DefaultSearchPlugins {
		t.Fatalf("case-folded ID = %q, want default %q", got, DefaultSearchPlugins)
	}
	if got := ValidateDefaultSearchPlugin("Jackett provider", plugins); got != DefaultSearchPlugins {
		t.Fatalf("display-name alias = %q, want default %q", got, DefaultSearchPlugins)
	}
}

func TestValidateDefaultSearchPluginDoesNotSwitchOnAliasCollision(t *testing.T) {
	plugins := []SearchPlugin{
		{Name: "jackett", FullName: "Jackett provider", Enabled: true},
		{Name: "other", FullName: "jackett", Enabled: true},
	}
	if got := ValidateDefaultSearchPlugin("jackett", plugins); got != "jackett" {
		t.Fatalf("exact ID = %q, want jackett", got)
	}
	if got := ValidateDefaultSearchPlugin("Jackett provider", plugins); got != DefaultSearchPlugins {
		t.Fatalf("display-name alias = %q, want default %q", got, DefaultSearchPlugins)
	}
	if got := ValidateDefaultSearchPlugin("Jackett", plugins); got != DefaultSearchPlugins {
		t.Fatalf("colliding display name = %q, want default %q", got, DefaultSearchPlugins)
	}
}

func TestReadDefaultSearchPluginDefaultsForMissingAndEmptyValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if got, err := ReadDefaultSearchPlugin(path); err != nil || got != DefaultSearchPlugins {
		t.Fatalf("missing config = %q, %v; want %q", got, err, DefaultSearchPlugins)
	}
	if err := os.WriteFile(path, []byte(`{"url":"http://qbt.test","default_search_plugin":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDefaultSearchPlugin(path); err != nil || got != DefaultSearchPlugins {
		t.Fatalf("empty preference = %q, %v; want %q", got, err, DefaultSearchPlugins)
	}
	if err := os.WriteFile(path, []byte(`{"url":"http://qbt.test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDefaultSearchPlugin(path); err != nil || got != DefaultSearchPlugins {
		t.Fatalf("older config = %q, %v; want %q", got, err, DefaultSearchPlugins)
	}
}

func TestReadDefaultSearchPluginRejectsWrongType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"default_search_plugin":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDefaultSearchPlugin(path); err == nil {
		t.Fatal("wrong preference type unexpectedly succeeded")
	}
}

func TestSetDefaultSearchPluginPreservesConfigAndSupportsClearing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := configfile.Update(path, func(object configfile.Object) (bool, error) {
		object["url"] = json.RawMessage(`"http://qbt.test"`)
		object["owner"] = json.RawMessage(`{"name":"nate"}`)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaultSearchPlugin(path, " Jackett "); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDefaultSearchPlugin(path)
	if err != nil || got != "Jackett" {
		t.Fatalf("saved preference = %q, %v; want Jackett", got, err)
	}
	object, err := configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := object["url"]; !ok {
		t.Fatal("SetDefaultSearchPlugin removed unrelated url")
	}
	if _, ok := object["owner"]; !ok {
		t.Fatal("SetDefaultSearchPlugin removed unrelated owner")
	}

	if err := SetDefaultSearchPlugin(path, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDefaultSearchPlugin(path); err != nil || got != DefaultSearchPlugins {
		t.Fatalf("cleared preference = %q, %v; want %q", got, err, DefaultSearchPlugins)
	}
	object, err = configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := object[defaultSearchPluginConfigKey]; ok {
		t.Fatal("clearing preference left config key")
	}
}

func TestSetDefaultSearchPluginRejectsMultiProviderValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, selection := range []string{"multi", "jackett|prowlarr", "jack\nett"} {
		if err := SetDefaultSearchPlugin(path, selection); err == nil {
			t.Fatalf("SetDefaultSearchPlugin(%q) unexpectedly succeeded", selection)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid preference created config: %v", err)
	}
}

func TestSetDefaultSearchPluginCanonicalizesGroupSelectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SetDefaultSearchPlugin(path, "ALL"); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDefaultSearchPlugin(path); err != nil || got != "all" {
		t.Fatalf("group preference = %q, %v; want all", got, err)
	}
	if err := SetDefaultSearchPlugin(path, "ENABLED"); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDefaultSearchPlugin(path); err != nil || got != DefaultSearchPlugins {
		t.Fatalf("enabled preference = %q, %v; want %q", got, err, DefaultSearchPlugins)
	}
	if strings.TrimSpace(DefaultSearchPlugins) == "" {
		t.Fatal("DefaultSearchPlugins must not be empty")
	}
}

func TestResetDefaultSearchPluginIfMatchesIsAtomicAndServerScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := configfile.Update(path, func(object configfile.Object) (bool, error) {
		object["url"] = json.RawMessage(`"https://qbt.example:443/webui"`)
		object["user"] = json.RawMessage(`"admin"`)
		object["owner"] = json.RawMessage(`{"name":"nate"}`)
		object[defaultSearchPluginConfigKey] = json.RawMessage(`"jackett"`)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	other := &App{configPath: path, creds: Credentials{URL: "https://other.example", User: "admin"}}
	if err := other.ResetDefaultSearchPluginIfMatches("jackett"); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDefaultSearchPlugin(path); err != nil || got != "jackett" {
		t.Fatalf("other-server reset = %q, %v; want jackett", got, err)
	}

	app := &App{configPath: path, creds: Credentials{URL: "https://qbt.example/webui/", User: "admin"}}
	if err := app.ResetDefaultSearchPluginIfMatches("missing"); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDefaultSearchPlugin(path); err != nil || got != "jackett" {
		t.Fatalf("stale reset = %q, %v; want jackett", got, err)
	}
	if err := app.ResetDefaultSearchPluginIfMatches("jackett"); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadDefaultSearchPlugin(path); err != nil || got != DefaultSearchPlugins {
		t.Fatalf("matching reset = %q, %v; want %q", got, err, DefaultSearchPlugins)
	}
	object, err := configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := object["owner"]; !ok {
		t.Fatal("reset removed unrelated config")
	}
}
