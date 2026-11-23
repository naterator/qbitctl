package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
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
