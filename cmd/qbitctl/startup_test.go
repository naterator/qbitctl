package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/configfile"
)

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
