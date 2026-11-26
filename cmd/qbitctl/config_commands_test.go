package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/internal/terminaltheme"
	qbt "github.com/naterator/qbitctl/pkg/client"
)

func executeConfigCLI(path string, input io.Reader, args ...string) (string, string, error) {
	cmd := newRootCmd()
	var out, diag bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&diag)
	if input == nil {
		input = strings.NewReader("")
	}
	cmd.SetIn(input)
	cmd.SetArgs(append([]string{"--config", path}, args...))
	err := cmd.Execute()
	return out.String(), diag.String(), err
}

func TestConfigSettingsRoundTripWithoutCredentials(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	path := filepath.Join(t.TempDir(), "config.json")
	run := func(args ...string) string {
		t.Helper()
		out, diag, err := executeConfigCLI(path, nil, append([]string{"config"}, args...)...)
		if err != nil {
			t.Fatalf("%v: %v (%s)", args, err, diag)
		}
		return out
	}
	for _, tc := range []struct {
		setting, initial, input, result string
	}{
		{"tui-default", "false", "true", "true"},
		{"color-scheme", "default", "Tookie", "tookie"},
		{"data-units", "human", "bytes", "bytes"},
		{"refresh-interval", "adaptive", "23s", "23s"},
		{"http-timeout", "default", "40s", "40s"},
		{"queue-sort", "active-first", "name-asc", "name-asc"},
		{"queue-columns", "compact", "wide", "wide"},
		{"search-sort", "original", "seeders-desc", "seeders-desc"},
		{"search-min-seeders", "none", "5", "5"},
		{"search-min-size", "none", "5 MiB", "5 MiB"},
		{"search-max-size", "none", "6 MiB", "6 MiB"},
		{"default-search-plugin", "enabled", "all", "all"},
	} {
		if got := run(tc.setting, "get"); got != tc.initial+"\n" {
			t.Errorf("%s initial = %q", tc.setting, got)
		}
		run(tc.setting, "set", tc.input)
		if got := run(tc.setting, "get"); got != tc.result+"\n" {
			t.Errorf("%s saved = %q", tc.setting, got)
		}
	}
	help := run("color-scheme")
	if !strings.Contains(help, "get") || !strings.Contains(help, "set") || !strings.Contains(help, "list") {
		t.Fatalf("color scheme help = %q", help)
	}
	for _, args := range [][]string{
		{"color-scheme", "set", "poop"},
		{"search-max-size", "set", "4 MiB"},
		{"refresh-interval", "set", "800ms"},
	} {
		if _, _, err := executeConfigCLI(path, nil, append([]string{"config"}, args...)...); err == nil {
			t.Errorf("config accepted %v", args)
		}
	}
	if got := run("search-max-size", "get"); got != "6 MiB\n" {
		t.Fatalf("invalid update changed the saved bound to %q", got)
	}
}

func TestColorSchemeListShowsPaletteSwatches(t *testing.T) {
	withoutEnvironment(t, "NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("FORCE_COLOR", "1")
	path := filepath.Join(t.TempDir(), "config.json")

	out, diag, err := executeConfigCLI(path, nil, "config", "color-scheme", "list")
	if err != nil {
		t.Fatalf("list color schemes: %v (%s)", err, diag)
	}
	themes := terminaltheme.All()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != len(themes)+1 || !strings.Contains(ansi.Strip(lines[0]), "SWATCHES") {
		t.Fatalf("color scheme table has %d lines or a missing heading: %q", len(lines), lines[0])
	}
	for i, theme := range themes {
		line := lines[i+1]
		if strings.Count(line, "\x1b[48;2;") != len(paletteColors) ||
			!strings.HasPrefix(ansi.Strip(line), string(theme.ID)+" ") ||
			!strings.Contains(ansi.Strip(line), theme.Label) {
			t.Errorf("theme %q has an incorrect row: %q", theme.ID, line)
		}
	}
	if !strings.Contains(lines[1], "\x1b[48;2;46;52;64m \x1b[m") {
		t.Fatalf("Default is missing its one-cell canvas swatch: %q", lines[1])
	}

	t.Setenv("NO_COLOR", "1")
	plain, _, err := executeConfigCLI(path, nil, "config", "color-scheme", "list")
	if err != nil || strings.ContainsRune(plain, '\x1b') || !strings.Contains(plain, "#2E3440 #3B4252") {
		t.Fatalf("plain color scheme table = %q, %v", plain, err)
	}
}

func TestProfileAddPasswordStdinCreatesAnEncryptedNamedProfile(t *testing.T) {
	t.Setenv("QBITCTL_MASTER_KEY", "test-master-key")
	t.Setenv("QBITCTL_PASSWORD", "")
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	out, diag, err := executeConfigCLI(path, nil, "profile", "add")
	if err != nil || !strings.Contains(out, "--password") || server.LoginCalls != 0 {
		t.Fatalf("profile add help: %v, output %q, diagnostics %q", err, out, diag)
	}
	password := strings.NewReader("secret\r\n")
	out, diag, err = executeConfigCLI(path, password, "profile", "add", "nate", "--url", server.creds().URL, "--user", "admin", "--password", "-")
	if err != nil || server.LoginCalls != 1 || !strings.Contains(out, "Saved profile nate") || strings.Contains(out+diag, "secret") {
		t.Fatalf("profile add failed or emitted undesired output: %v, output %q, diagnostics %q", err, out, diag)
	}
	profiles, def, err := qbt.ReadProfiles(path)
	if err != nil || def != "nate" || len(profiles) != 1 || !profiles[0].HasPassword {
		t.Fatalf("saved profiles = %v, default %q, error %v", profiles, def, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("enc:v2:")) || bytes.Contains(data, []byte("secret")) {
		t.Fatal("profile password was not saved encrypted")
	}

	for _, args := range [][]string{
		{"duplicate", "--url", "bad://url", "--user", "admin"},
		{"nate", "--url", server.creds().URL, "--user", "admin"},
		{"new", "--url", server.creds().URL, "--user", "admin", "--pass-stdin"},
	} {
		in := strings.NewReader("secret\n")
		argv := append(append([]string{"profile", "add"}, args...), "--password", "-")
		if _, _, err := executeConfigCLI(path, in, argv...); err == nil || in.Len() != len("secret\n") || server.LoginCalls != 1 {
			t.Fatalf("invalid profile request %v consumed credentials or attempted login", args)
		}
	}
}

func TestDefaultSearchProviderListAndSaveUseServerCatalog(t *testing.T) {
	t.Setenv("QBITCTL_MASTER_KEY", "test-master-key")
	t.Setenv("QBITCTL_PASSWORD", "")
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	const catalog = `[{"name":"down","fullName":"A disabled","enabled":false},
		{"name":"zed","fullName":"Zulu","enabled":true},
		{"name":"alpha","fullName":"Alpha","enabled":true}]`
	server.BodyQueue["/api/v2/search/plugins"] = []string{catalog, catalog, catalog}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := qbt.ExecuteConfigWrite(qbt.Options{URL: server.creds().URL, User: "admin", Pass: "secret"}, path, nil); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		t.Helper()
		out, _, err := executeConfigCLI(path, nil, append([]string{"config", "default-search-plugin"}, args...)...)
		return out, err
	}
	var want strings.Builder
	for _, row := range [][3]string{
		{"ID", "NAME", "STATUS"},
		{"enabled", "Enabled search plugins", "group"},
		{"all", "All search plugins", "group"},
		{"alpha", "Alpha", "enabled"},
		{"zed", "Zulu", "enabled"},
		{"down", "A disabled", "disabled"},
	} {
		fmt.Fprintf(&want, "%-7s  %-22s  %s\n", row[0], row[1], row[2])
	}
	if out, err := run("list"); err != nil || out != want.String() {
		t.Fatalf("plugin list = %q, %v", out, err)
	}
	if _, err := run("set", "Zulu"); err != nil {
		t.Fatalf("set plugin by display name: %v", err)
	}
	if _, err := run("set", "down"); err == nil {
		t.Fatal("accepted a disabled plugin")
	}
	if got, err := run("get"); err != nil || got != "zed\n" {
		t.Fatalf("saved default = %q, %v", got, err)
	}
}

func TestDefaultSearchProviderSetRequiresAnArgumentBeforeIO(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	input := strings.NewReader("fixture-input\n")
	out, diag, err := executeConfigCLI(path, input, "config", "default-search-plugin", "set", "--pass-stdin")
	var coded *qbt.CodedError
	if !errors.As(err, &coded) || coded.Code != exitBadArgs || !strings.Contains(err.Error(), "missing required parameter PROVIDER") {
		t.Fatalf("set returned %v, stdout %q, diagnostics %q", err, out, diag)
	}
	if server.LoginCalls != 0 || input.Len() != len("fixture-input\n") {
		t.Fatal("missing parameter caused login or consumed stdin")
	}
}

func TestBatchAcceptsForceAndForceStartAlias(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Example", Hash: hash}}
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("force " + hash + "\nforce-start " + hash + "\n")
	_, diag, err := executeConfigCLI(path, input, "--url", server.creds().URL, "--user", "admin", "--pass", "secret", "batch")
	if err != nil || server.LoginCalls != 1 || len(server.Forms["/api/v2/torrents/setForceStart"]) != 2 {
		t.Fatalf("batch did not force both torrents under one login: %v, %s", err, diag)
	}
}
