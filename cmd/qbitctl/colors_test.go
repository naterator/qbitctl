package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/internal/terminaltheme"
)

func TestColorsShowsCurrentPaletteAndCLISamples(t *testing.T) {
	withoutEnvironment(t, "NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("FORCE_COLOR", "1")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := terminaltheme.Save(path, "default"); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		root := newRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	got := run("--config", path, "--profile", "unused", "debug", "color-mapping")
	if !strings.HasPrefix(got, "Default\n") || strings.Count(got, "\x1b[48;2;") != 14 {
		t.Fatalf("preview did not render all fourteen Default swatches:\n%q", got)
	}
	for _, want := range []string{
		"\x1b[48;2;46;52;64m       \x1b[m",
		"\x1b[48;2;236;239;244m       \x1b[m",
		"\x1b[38;2;236;239;244;1mHeadings and table headers\x1b[m",
		"\x1b[38;2;143;188;187mCommands and field labels\x1b[m",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview missing %q:\n%q", want, got)
		}
	}
	rows := strings.Split(strings.TrimSpace(ansi.Strip(got)), "\n")[3:]
	if len(rows) != 14 {
		t.Fatalf("preview has %d rows", len(rows))
	}
	for i, row := range rows {
		if len(row) < 3 || row[0] != byte('A'+i) {
			t.Errorf("preview row %d = %q", i, row)
		}
	}

	if !strings.HasSuffix(rows[3], "Headings and table headers") || !strings.HasSuffix(rows[6], "  -") {
		t.Fatalf("heading mapping rows: D = %q, G = %q", rows[3], rows[6])
	}

	t.Setenv("NO_COLOR", "1")
	plain := run("--config", path, "debug", "color-mapping")
	if !strings.Contains(plain, "#2E3440") || strings.ContainsRune(plain, '\x1b') {
		t.Fatalf("no-color preview lost its hex values or contains escapes:\n%q", plain)
	}
	if strings.Contains(run("--help"), "\n  debug ") || strings.Contains(run("__complete", "de"), "debug\t") {
		t.Fatal("the internal debug command appeared in root help or shell completion")
	}
}

func TestDebugRejectsImplicitAndExplicitHelp(t *testing.T) {
	for _, args := range [][]string{
		{"debug"}, {"debug", "--help"}, {"debug", "color-mapping", "--help"},
		{"help", "debug"}, {"h", "debug", "color-mapping"},
	} {
		root := newRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		if err := root.Execute(); !errors.Is(err, errUnknownDebug) || out.Len() != 0 {
			t.Errorf("%v: error %v, output %q", args, err, &out)
		}
	}
}
