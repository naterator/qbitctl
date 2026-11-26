package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/naterator/qbitctl/pkg/client"
)

func TestCLIProfileLifecycleWithAlternateConfig(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	t.Setenv("QBITCTL_MASTER_KEY", "profile-cli-master")
	t.Setenv("QBITCTL_PASSWORD", "")
	configPath := filepath.Join(t.TempDir(), "connections.json")
	creds := server.creds()
	connection := []string{"--url", creds.URL, "--user", creds.User, "--pass", creds.Pass}
	run := func(want, wantError string, args ...string) {
		t.Helper()
		cmd := newRootCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs(append([]string{"--config", configPath}, args...))
		err := cmd.ExecuteContext(t.Context())
		if (err != nil) != (wantError != "") || !strings.Contains(stderr.String(), wantError) {
			t.Fatalf("profile command returned %v, stderr %q; want error %q", err, &stderr, wantError)
		}
		if got := stdout.String(); !strings.Contains(got, want) || strings.Contains(got, creds.Pass) {
			t.Fatalf("profile command output did not contain %q or exposed credentials", want)
		}
	}

	run("", "", append([]string{"config", "write"}, connection...)...)
	run("* default\t", "", "profile", "list")
	run("Startup profile: default", "", "profile", "use", "default")
	setup, err := qbt.ReadSetup(qbt.Options{ConfigPath: configPath})
	if err != nil || setup.Profile != "default" || setup.URL != creds.URL || setup.User != creds.User || !setup.HasPassword {
		t.Fatalf("migrated setup = %+v, %v", setup, err)
	}
	run("", "", "--profile", "default", "info", "--json")
	run("Saved profile lab", "", append([]string{"profile", "add", "lab"}, connection...)...)
	run("lab\t"+creds.URL, "", "__complete", "add", "--profile", "la")
	run("", "already exists", append([]string{"profile", "add", "lab"}, connection...)...)
	run("", "", "--profile", "lab", "info", "--json")
	newURL := strings.TrimRight(creds.URL, "/") + "/other-webui"
	run("", "", "--profile", "lab", "config", "write", "--url", newURL, "--user", creds.User, "--pass", creds.Pass)
	run("* default\t"+creds.URL, "", "profile", "list")
	run("Startup profile: lab", "", "profile", "use", "lab")
	run("* lab\t"+newURL, "", "profile", "list")
	startupURL := newURL + "/startup"
	run("", "", "config", "write", "--url", startupURL, "--user", creds.User, "--pass", creds.Pass)
	run("* lab\t"+startupURL, "", "profile", "list")
	run("Removed profile lab", "", "profile", "remove", "lab")
	run("* default\t"+creds.URL, "", "profile", "list")
	run("", "not configured", "--profile", "lab", "info")
	run("", "not configured", append([]string{"--profile", "lab", "config", "write"}, connection...)...)
}

func TestAuthenticatedCLISetupHint(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("QBITCTL_PASSWORD", "")

	run := func(code int, wantError string, wantHint bool, args ...string) {
		t.Helper()
		cmd := newRootCmd()
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(t.Context())
		if err == nil {
			t.Fatalf("%v: expected a CLI error", args)
		}
		var exit exitError
		if !errors.As(err, &exit) {
			printCLIError(cmd, err)
			exit.code = exitBadArgs
		}
		if exit.code != code {
			t.Fatalf("%v: got %v, want exit code %d", args, err, code)
		}
		got := stderr.String()
		if !strings.Contains(got, "[ERROR] "+wantError) ||
			strings.Contains(got, "\nRun qbitctl profile setup") != wantHint {
			t.Fatalf("%v: unexpected diagnostics %q", args, got)
		}
	}

	for _, args := range [][]string{
		{"add", "https://example.test/download.torrent"},
		{"files", "abcd"},
		{"get", "name", "abcd"},
		{"info"},
	} {
		run(qbt.ExitFile, "No credentials supplied", true, args...)
	}
	run(qbt.ExitBadArgs, "accepts 1 arg(s), received 0", true, "add")
	run(qbt.ExitBadArgs, "accepts 1 arg(s), received 0", true, "files")
	run(qbt.ExitBadArgs, "accepts 2 arg(s), received 0", true, "get")
	run(qbt.ExitBadArgs, "unknown command", true, "info", "extra")
	run(qbt.ExitBadArgs, "Config file 'missing.json' does not exist or is not readable", true, "--config", "missing.json", "info")

	if err := os.WriteFile("config.json", []byte(`{"profiles":{},"ui":{"tui_default":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	run(qbt.ExitFile, "No credentials supplied", true, "info")
	run(qbt.ExitFile, "No credentials supplied", false, "--url", "https://example.test", "info")
	run(qbt.ExitBadArgs, "accepts 1 arg(s), received 0", false, "--url", "https://example.test", "add")

	for _, config := range []string{
		`{"url":"https://example.test"}`,
		`{"profiles":{"lab":{"url":"https://example.test"}},"default_profile":"lab"}`,
	} {
		if err := os.WriteFile("config.json", []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		run(qbt.ExitBadArgs, "accepts 1 arg(s), received 0", false, "add")
	}
}
