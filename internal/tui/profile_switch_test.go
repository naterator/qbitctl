//go:build !plan9

package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/naterator/qbitctl/pkg/client"
)

func TestLateRemoteResultCannotPopulateAfterGenerationSwitch(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.remoteGeneration = 2
	m.busy = "Connecting to second"
	m.work = connectWork
	m.queue = []client.TorrentInfo{{Hash: "new"}}
	done := workDone{kind: queueWork, remoteGeneration: 1, queue: []client.TorrentInfo{{Hash: "old"}}}
	if cmd := m.finishWork(done); cmd != nil {
		t.Fatalf("stale result returned command %T", cmd)
	}
	if len(m.queue) != 1 || m.queue[0].Hash != "new" {
		t.Fatalf("stale result replaced queue: %#v", m.queue)
	}
	if m.busy != "Connecting to second" {
		t.Fatalf("stale result cleared current work: %q", m.busy)
	}
}

func TestSwitchProfileIsSessionOnlyAndGuardsConnectionGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := `{
  "default_profile": "home",
  "profiles": {
    "home": {"url":"https://home.example", "user":"admin", "password":"saved"},
    "seedbox": {"url":"https://seed.example", "user":"admin", "password":"saved"}
  }
}`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	originalConnect := connectClient
	var got client.Options
	connectClient = func(_ context.Context, opts *client.Options, _, _ io.Writer) (*client.App, error) {
		got = *opts
		return nil, errors.New("test connection failure")
	}
	defer func() { connectClient = originalConnect }()

	m := newModel(t.Context(), nil)
	m.configPath = path
	m.setup = setupState{path: path, profile: "home", url: "https://home.example", user: "admin", hasPassword: true, ready: true}
	command := m.switchProfile(" seedbox ")
	// The command is intentionally lazy; opts are observed when the worker runs
	// below, not while the picker is being opened.
	if command == nil {
		t.Fatal("switchProfile returned no connection command")
	}
	done, ok := command().(workDone)
	if !ok {
		t.Fatalf("switch command returned %T", command())
	}
	if got.Profile != "seedbox" {
		t.Fatalf("connection profile = %q, want seedbox", got.Profile)
	}
	if done.remoteGeneration != m.remoteGeneration {
		t.Fatalf("connection generation = %d, want %d", done.remoteGeneration, m.remoteGeneration)
	}
	m.finishWork(done)
	m.workers.Wait()
	if m.setup.profile != "seedbox" {
		t.Fatalf("active setup profile = %q", m.setup.profile)
	}
	if m.page != configPage {
		t.Fatalf("failed switch page = %d, want config page", m.page)
	}
	// switchProfile never calls SetDefaultProfile; the startup selector remains
	// home in the on-disk JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || !contains(string(data), `"default_profile": "home"`) {
		t.Fatalf("startup default changed unexpectedly: %s", data)
	}
}

func contains(value, want string) bool {
	for i := 0; i+len(want) <= len(value); i++ {
		if value[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
