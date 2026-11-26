//go:build !plan9

package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
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

func TestProfileSwitchKeepsWorkingSessionOnFailureOrCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		name := "rejected login"
		if cancel {
			name = "canceled login returning a late session"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeProfilePickerConfig(t, path)
			active, activeLogouts := newMockApp(t)
			t.Cleanup(active.Logout)
			candidate, candidateLogouts := newMockApp(t)

			connecting := make(chan struct{})
			originalConnect := connectClient
			connectClient = func(ctx context.Context, _ *client.Options, _, _ io.Writer) (*client.App, error) {
				if cancel {
					close(connecting)
					<-ctx.Done()
					return candidate, nil
				}
				return nil, errors.New("bad credentials")
			}
			t.Cleanup(func() { connectClient = originalConnect })
			if !cancel {
				t.Cleanup(candidate.Logout)
			}

			service := &recordingService{}
			m := newModel(t.Context(), service)
			m.app, m.ownsApp = active, true
			m.configPath = path
			m.setup = setupState{path: path, profile: "home", ready: true}
			seedRecordingQueue(m, service, client.TorrentInfo{Hash: "active-torrent"})
			m.toggleMark()

			cmd := m.switchProfile("seedbox")
			if cmd == nil || m.app != active || m.setup.profile != "home" || len(m.queue) != 1 {
				t.Fatal("starting a profile switch displaced the active workspace")
			}
			if cancel {
				<-connecting
				m.handleKey("esc")
			}
			runTUIWork(t, m, cmd)
			m.workers.Wait()

			if m.backend != service || m.app != active || m.page != queuePage || m.dialog != nil ||
				m.setup.profile != "home" || len(m.queue) != 1 || !m.marked["active-torrent"] {
				t.Fatal("failed or canceled switch changed the active workspace")
			}
			if got := activeLogouts.Load(); got != 0 {
				t.Fatalf("active session logout count = %d", got)
			}
			if cancel && candidateLogouts.Load() != 1 {
				t.Fatalf("late candidate logout count = %d, want 1", candidateLogouts.Load())
			}
			runTUIWork(t, m, m.refresh())
			if service.queueCallCount() != 1 {
				t.Fatal("active queue could not refresh after the attempt")
			}
		})
	}
}

func TestSuccessfulProfileSwitchRetiresOldSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeProfilePickerConfig(t, path)
	active, activeLogouts := newMockApp(t)
	candidate, _ := newMockApp(t)
	t.Cleanup(candidate.Logout)
	originalConnect := connectClient
	connectClient = func(context.Context, *client.Options, io.Writer, io.Writer) (*client.App, error) {
		return candidate, nil
	}
	t.Cleanup(func() { connectClient = originalConnect })

	service := &recordingService{}
	m := newModel(t.Context(), service)
	m.app, m.ownsApp = active, true
	m.configPath = path
	m.setup = setupState{path: path, profile: "home", ready: true}
	seedRecordingQueue(m, service, client.TorrentInfo{Hash: "active-torrent"})
	m.toggleMark()

	stopping := make(chan struct{})
	release := make(chan struct{})
	releaseWorker := sync.OnceFunc(func() { close(release) })
	defer releaseWorker()
	old := m.startWork(queueWork, "Reading old queue", func(ctx context.Context) workDone {
		<-ctx.Done()
		close(stopping)
		<-release
		return workDone{queue: []client.TorrentInfo{{Hash: "stale-torrent"}}}
	})
	switchTo := m.switchProfile("seedbox")
	<-stopping
	if m.app != active || len(m.queue) != 1 {
		t.Fatal("candidate displaced the previous session before adoption")
	}
	if got := activeLogouts.Load(); got != 0 {
		t.Fatalf("old session logged out with a pending worker: %d", got)
	}
	releaseWorker()
	runTUIWork(t, m, switchTo)
	if m.app != candidate || m.setup.profile != "seedbox" || len(m.marked) != 0 {
		t.Fatal("successful switch did not adopt the candidate and reset old marks")
	}
	runTUIWork(t, m, old)
	m.workers.Wait()
	if got := activeLogouts.Load(); got != 1 {
		t.Fatalf("old session logout count = %d, want 1", got)
	}
	for _, torrent := range m.queue {
		if torrent.Hash == "stale-torrent" {
			t.Fatal("late old queue response changed the new workspace")
		}
	}
}
