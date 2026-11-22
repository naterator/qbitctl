package main

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/configfile"
	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

func TestRunDefaultStartupPolicy(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		config      string
		wantHelp    bool
		wantTUI     bool
	}{
		{name: "noninteractive ignores startup config", config: `{"ui":{"tui_default":true}}`, wantHelp: true},
		{name: "interactive tui default", interactive: true, config: `{"ui":{"tui_default":true}}`, wantTUI: true},
		{name: "interactive configured cli default", interactive: true, config: `{"url":"http://qbt.test","user":"admin","password":"saved"}`, wantHelp: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			oldInteractive, oldRunner := isInteractive, runTUIFunc
			t.Cleanup(func() { isInteractive, runTUIFunc = oldInteractive, oldRunner })
			isInteractive = func(*cobra.Command) bool { return tt.interactive }
			called := 0
			runTUIFunc = func(*cobra.Command, CLIOptions) error {
				called++
				return nil
			}

			cmd := newRootCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			err := runDefault(cmd, CLIOptions{ConfigPath: path})
			if err != nil {
				t.Fatalf("runDefault returned error: %v", err)
			}
			if got := called; (got == 1) != tt.wantTUI {
				t.Fatalf("TUI calls = %d, want TUI = %v", got, tt.wantTUI)
			}
			if got := strings.Contains(out.String(), "Usage:"); got != tt.wantHelp {
				t.Fatalf("help = %v, want %v, output %q", got, tt.wantHelp, out.String())
			}
		})
	}
}

func TestFirstRunSetupChoices(t *testing.T) {
	oldRunner := runTUIFunc
	t.Cleanup(func() { runTUIFunc = oldRunner })

	t.Run("enter and one select tui", func(t *testing.T) {
		for _, input := range []string{"\n", "1\n"} {
			called := 0
			runTUIFunc = func(*cobra.Command, CLIOptions) error { called++; return nil }
			cmd := newRootCmd()
			cmd.SetIn(strings.NewReader(input))
			var out bytes.Buffer
			cmd.SetErr(&out)
			finished, err := firstRunSetup(cmd, &CLIOptions{}, filepath.Join(t.TempDir(), "config.json"))
			if err != nil || !finished || called != 1 {
				t.Fatalf("input %q: finished=%v calls=%d err=%v", input, finished, called, err)
			}
		}
	})

	t.Run("quit and eof", func(t *testing.T) {
		for _, input := range []string{"q\n", ""} {
			called := 0
			runTUIFunc = func(*cobra.Command, CLIOptions) error { called++; return nil }
			cmd := newRootCmd()
			cmd.SetIn(strings.NewReader(input))
			var out bytes.Buffer
			cmd.SetErr(&out)
			finished, err := firstRunSetup(cmd, &CLIOptions{}, filepath.Join(t.TempDir(), "config.json"))
			if err != nil || !finished || called != 0 {
				t.Fatalf("input %q: finished=%v calls=%d err=%v", input, finished, called, err)
			}
		}
	})

	t.Run("invalid choice then eof retries once", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetIn(strings.NewReader("x\n"))
		var out bytes.Buffer
		cmd.SetErr(&out)
		finished, err := firstRunSetup(cmd, &CLIOptions{}, filepath.Join(t.TempDir(), "config.json"))
		if err != nil || !finished {
			t.Fatalf("finished=%v err=%v", finished, err)
		}
		if got := strings.Count(out.String(), "Choose setup"); got != 2 {
			t.Fatalf("setup prompts = %d, want one retry: %q", got, out.String())
		}
	})
}

func TestFirstRunSetupTextModeSavesAndResumes(t *testing.T) {
	t.Setenv("QBITCTL_MASTER_KEY", "startup-test-master-key")
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	opts := CLIOptions{ConfigPath: path}
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader("2\nhttp://qbt.test\nadmin\nsecret\n"))
	var out bytes.Buffer
	cmd.SetErr(&out)
	finished, err := firstRunSetup(cmd, &opts, path)
	if err != nil || finished {
		t.Fatalf("text setup finished=%v err=%v output=%q", finished, err, out.String())
	}
	if opts.ConfigPath != path || opts.URL != "" || opts.User != "" || opts.Pass != "" {
		t.Fatalf("text setup options = %#v", opts)
	}
	setup, err := qbt.ReadSetup(qbt.Options{ConfigPath: path})
	if err != nil || !setup.Ready() {
		t.Fatalf("saved setup = %#v, err=%v", setup, err)
	}
}

func TestFirstRunSetupTextModeDoesNotSaveOnLoginFailure(t *testing.T) {
	server := newQBTestServer(t)
	server.StatusQueue["/api/v2/auth/login"] = []int{http.StatusForbidden}
	withQBServerClientFactory(t, server)
	path := filepath.Join(t.TempDir(), "config.json")
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader("2\nhttp://qbt.test\nadmin\nwrong\n"))
	var out bytes.Buffer
	cmd.SetErr(&out)
	finished, err := firstRunSetup(cmd, &CLIOptions{ConfigPath: path}, path)
	if !finished {
		t.Fatal("failed text setup should finish the invocation")
	}
	var exit exitError
	if !errors.As(err, &exit) || exit.code != qbt.ExitLoginFail {
		t.Fatalf("error = %v, want login failure exit", err)
	}
	if _, readErr := os.Stat(path); !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("failed setup left config behind, stat error = %v", readErr)
	}
}

func TestRunTUIRejectsNonInteractiveStreams(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	var err error
	for _, child := range cmd.Commands() {
		if child.Name() == "tui" {
			err = runTUI(child, CLIOptions{})
			break
		}
	}
	var exit exitError
	if !errors.As(err, &exit) || exit.code != qbt.ExitBadArgs {
		t.Fatalf("runTUI error = %v, want ExitBadArgs", err)
	}
}

func TestTUIDefaultCommandUsesSelectedConfigWithoutConnecting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alternate.json")
	if err := os.WriteFile(path, []byte(`{"url":"https://example.invalid","ui":{"queue":{"sort":"progress-desc"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		action []string
		want   string
	}{
		{nil, "off"},
		{[]string{"on"}, "on"},
		{[]string{"status"}, "on"},
		{[]string{"off"}, "off"},
	} {
		root := newRootCmd()
		root.SetArgs(append([]string{"--config", path, "tui-default"}, tc.action...))
		var out bytes.Buffer
		root.SetOut(&out)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(out.String()); got != tc.want {
			t.Errorf("tui-default %v reported %q, want %q", tc.action, got, tc.want)
		}
	}

	config, err := configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	ui, err := config.Child("ui")
	if err != nil {
		t.Fatal(err)
	}
	queue, err := ui.Child("queue")
	if err != nil {
		t.Fatal(err)
	}
	if string(config["url"]) != `"https://example.invalid"` || string(queue["sort"]) != `"progress-desc"` {
		t.Fatal("changing the default startup mode replaced unrelated configuration")
	}
}

func TestNonInteractiveBareCommandShowsHelpEvenWithTUIDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alternate.json")
	if err := os.WriteFile(path, []byte(`{"ui":{"tui_default":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := newRootCmd()
	root.SetArgs([]string{"--config", path})
	root.SetIn(strings.NewReader(""))
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("expected help on noninteractive invocation, got %q", out.String())
	}
}
