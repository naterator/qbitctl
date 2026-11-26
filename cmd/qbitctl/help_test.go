package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/internal/terminaltheme"
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

func TestQbitctlHelpUsesTheSharedTheme(t *testing.T) {
	withoutEnvironment(t, "NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	run := func(args ...string) string {
		t.Helper()
		root := newRootCmd()
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("execute help: %v", err)
		}
		return output.String()
	}

	def := run("help", "list")
	if !strings.Contains(def, "\x1b[38;2;154;200;255;1mUsage:") ||
		!strings.Contains(def, "\x1b[38;2;133;215;200m") {
		t.Fatalf("help is missing Default's key and accent colors: %q", def)
	}

	fallback := "config.json"
	if err := terminaltheme.Save(fallback, "nord"); err != nil {
		t.Fatal(err)
	}
	nord := run("help", "list")
	if !strings.Contains(nord, "\x1b[38;2;136;192;208;1mUsage:") {
		t.Fatalf("help did not use the fallback config's Nord theme: %q", nord)
	}

	explicit := filepath.Join(t.TempDir(), "theme.json")
	if err := terminaltheme.Save(explicit, "ocean"); err != nil {
		t.Fatal(err)
	}
	ocean := run("--config", explicit, "--profile", "another", "help", "list")
	if !strings.Contains(ocean, "\x1b[38;2;125;211;252;1mUsage:") ||
		!strings.Contains(ocean, "\x1b[38;2;103;232;249m") {
		t.Fatalf("help is missing Ocean's key and accent colors: %q", ocean)
	}

	t.Setenv("NO_COLOR", "1")
	plain := run("--config", explicit, "help", "list")
	if plain != ansi.Strip(ocean) || plain != ansi.Strip(def) || plain != ansi.Strip(nord) {
		t.Fatal("themed help changed the underlying text")
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
