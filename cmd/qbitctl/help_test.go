package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestQbitctlHelpIsGroupedAndPlainWhenRedirected(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	t.Setenv("FORCE_COLOR", "")

	root := testHelpCommandTree()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"--help"})
	installHelp(root)

	if err := root.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}
	got := output.String()
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("redirected help contains ANSI escapes: %q", got)
	}
	for _, want := range []string{"View:", "Setup:", "list", "config", "Flags:", "qbitctl [command] --help"} {
		if !strings.Contains(got, want) {
			t.Errorf("help output missing %q:\n%s", want, got)
		}
	}
}

func TestQbitctlHelpCanBeForcedIntoColor(t *testing.T) {
	withoutEnvironment(t, "NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("FORCE_COLOR", "1")

	root := testHelpCommandTree()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"--help"})
	installHelp(root)

	if err := root.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "\x1b[38;5;81m") || !strings.Contains(got, "\x1b[38;5;116m") {
		t.Fatalf("forced-color help is missing heading/command colors:\n%q", got)
	}
}

func testHelpCommandTree() *cobra.Command {
	root := &cobra.Command{Use: "qbitctl", Short: "qBittorrent Web API CLI"}
	root.AddGroup(&cobra.Group{ID: "view", Title: "View:"}, &cobra.Group{ID: "setup", Title: "Setup:"})
	root.PersistentFlags().StringP("url", "u", "", "qBittorrent WebUI URL")
	root.AddCommand(
		&cobra.Command{Use: "list", Short: "List all torrents", GroupID: "view", Run: func(*cobra.Command, []string) {}},
		&cobra.Command{Use: "config", Short: "Manage qBittorrent credentials", GroupID: "setup", Run: func(*cobra.Command, []string) {}},
	)
	return root
}

func withoutEnvironment(t *testing.T, name string) {
	t.Helper()
	original, wasSet := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(name, original)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}
