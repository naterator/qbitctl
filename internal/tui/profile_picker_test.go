//go:build !plan9

package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

func writeProfilePickerConfig(t *testing.T, path string) {
	t.Helper()
	const config = `{
  "default_profile": "seedbox",
  "profiles": {
    "home": {"url": "https://home.example", "user": "admin", "password": "saved"},
    "seedbox": {"url": "https://seed.example", "user": "admin", "password": "saved"}
  }
}`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProfilePickersSelectProfilesAndMarkCurrentAndStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeProfilePickerConfig(t, path)

	m := newModel(t.Context(), nil)
	m.configPath = path
	m.setup = setupState{path: path, profile: "home", url: "https://home.example", user: "admin", hasPassword: true}

	if cmd := m.openProfilePicker(); cmd != nil {
		t.Fatalf("open profile picker returned command %T", cmd)
	}
	if m.dialog == nil || m.dialog.kind != dialogProfilePicker {
		t.Fatalf("profile picker = %#v, want profile dialog", m.dialog)
	}
	if m.dialog.title != "Switch server profile" || m.dialog.profileStartup {
		t.Fatalf("active picker = %#v, want session picker", m.dialog)
	}
	if got := m.dialog.items[m.dialog.selected].action; got != "home" {
		t.Fatalf("active picker selection = %q, want home", got)
	}
	items := make(map[string]dialogItem, len(m.dialog.items))
	for _, item := range m.dialog.items {
		items[item.action] = item
	}
	if !strings.Contains(items["home"].description, "current") {
		t.Fatalf("active profile marker missing: %#v", items["home"])
	}
	if !strings.Contains(items["seedbox"].description, "startup default") {
		t.Fatalf("startup marker missing: %#v", items["seedbox"])
	}
	if frame := m.dialogView(100, 30); !containsAll(frame, "https://home.example · current", "https://seed.example · startup default") {
		t.Fatalf("profile picker did not render both profiles and their markers: %q", frame)
	}

	originalConnect := connectClient
	connectClient = func(context.Context, *client.Options, io.Writer, io.Writer) (*client.App, error) {
		return nil, errors.New("profile test connection failure")
	}
	defer func() { connectClient = originalConnect }()

	if cmd := m.update(tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatalf("moving in profile picker returned command %T", cmd)
	}
	if got := m.dialog.items[m.dialog.selected].action; got != "seedbox" {
		t.Fatalf("moved profile selection = %q, want seedbox", got)
	}
	cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || m.dialog != nil {
		t.Fatalf("active profile submit command=%v dialog=%#v", cmd != nil, m.dialog)
	}
	runTUIWork(t, m, cmd)
	m.workers.Wait()
	if m.setup.profile != "seedbox" {
		t.Fatalf("active profile after picker = %q, want seedbox", m.setup.profile)
	}
	if m.notice == "" || !strings.Contains(m.notice, "profile test connection failure") {
		t.Fatalf("connection failure notice = %q", m.notice)
	}

	// Startup selection is a separate operation: it persists the choice but
	// leaves the already-connected session on the selected active profile.
	m.dialog = nil
	if cmd := m.openStartupProfilePicker(); cmd != nil {
		t.Fatalf("open startup profile picker returned command %T", cmd)
	}
	if m.dialog == nil || m.dialog.kind != dialogProfilePicker {
		t.Fatalf("startup picker = %#v, want profile dialog", m.dialog)
	}
	if m.dialog.title != "Choose startup profile" || !m.dialog.profileStartup {
		t.Fatalf("startup picker = %#v, want startup picker", m.dialog)
	}
	if got := m.dialog.items[m.dialog.selected].action; got != "seedbox" {
		t.Fatalf("startup picker selection = %q, want seedbox", got)
	}
	if cmd := m.update(tea.KeyPressMsg{Code: tea.KeyUp}); cmd != nil {
		t.Fatalf("moving startup picker returned command %T", cmd)
	}
	if got := m.dialog.items[m.dialog.selected].action; got != "home" {
		t.Fatalf("startup picker moved selection = %q, want home", got)
	}
	if cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("startup profile submit returned command %T", cmd)
	}
	if m.dialog != nil {
		t.Fatalf("startup picker remained open: %#v", m.dialog)
	}
	if m.setup.profile != "seedbox" {
		t.Fatalf("startup selection changed active profile to %q", m.setup.profile)
	}
	profiles, defaultName, err := client.ReadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if defaultName != "home" {
		t.Fatalf("persisted startup profile = %q, want home", defaultName)
	}
	if len(profiles) != 2 || !strings.Contains(m.notice, "Startup profile saved: home") {
		t.Fatalf("persisted profiles=%#v notice=%q", profiles, m.notice)
	}
}

func TestProfilePickerUsesSetupPathAndReportsUnavailableStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeProfilePickerConfig(t, path)

	// A resolved setup path is sufficient when the model has not copied it to
	// configPath yet (for example while entering the config page).
	m := newModel(t.Context(), nil)
	m.setup = setupState{path: path, profile: "home"}
	if cmd := m.openProfilePicker(); cmd != nil || m.dialog == nil {
		t.Fatalf("setup-path picker command=%v dialog=%#v", cmd != nil, m.dialog)
	}
	if got := m.dialog.items[m.dialog.selected].action; got != "home" {
		t.Fatalf("setup-path picker selected %q, want home", got)
	}

	tests := []struct {
		name       string
		configPath string
		contents   string
		wantNotice string
		wantFailed bool
	}{
		{
			name:       "no path",
			wantNotice: "No configuration path is available",
			wantFailed: true,
		},
		{
			name:       "missing file",
			configPath: filepath.Join(t.TempDir(), "missing.json"),
			wantNotice: "Profiles unavailable:",
			wantFailed: true,
		},
		{
			name:       "empty config",
			configPath: filepath.Join(t.TempDir(), "empty.json"),
			contents:   `{}`,
			wantNotice: "No saved server profiles",
		},
		{
			name:       "legacy default only",
			configPath: filepath.Join(t.TempDir(), "legacy.json"),
			contents:   `{"url":"https://legacy.example","user":"admin","password":"saved"}`,
			wantNotice: "No named profiles yet; use qbitctl profile add NAME",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.contents != "" {
				if err := os.WriteFile(tt.configPath, []byte(tt.contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			m := newModel(t.Context(), nil)
			m.configPath = tt.configPath
			if cmd := m.openProfilePicker(); cmd != nil {
				t.Fatalf("picker returned command %T", cmd)
			}
			if m.dialog != nil {
				t.Fatalf("picker opened unexpectedly: %#v", m.dialog)
			}
			if m.notice != tt.wantNotice && !strings.HasPrefix(m.notice, tt.wantNotice) {
				t.Fatalf("notice = %q, want %q", m.notice, tt.wantNotice)
			}
			if m.failed != tt.wantFailed {
				t.Fatalf("failed = %t, want %t", m.failed, tt.wantFailed)
			}
		})
	}
}

func TestSetStartupProfileValidatesAndPersistsOnlyConfiguredProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeProfilePickerConfig(t, path)
	m := newModel(t.Context(), nil)
	m.configPath = path

	if err := m.setStartupProfile("not a valid name"); err == nil {
		t.Fatal("invalid startup profile name unexpectedly succeeded")
	}
	if err := m.setStartupProfile("missing"); err == nil {
		t.Fatal("unconfigured startup profile unexpectedly succeeded")
	}
	if err := m.setStartupProfile(" home "); err != nil {
		t.Fatalf("set configured startup profile: %v", err)
	}
	if m.notice != "Startup profile saved: home" {
		t.Fatalf("startup save notice = %q", m.notice)
	}
	_, defaultName, err := client.ReadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if defaultName != "home" {
		t.Fatalf("startup profile on disk = %q, want home", defaultName)
	}

	withoutPath := newModel(t.Context(), nil)
	if err := withoutPath.setStartupProfile("home"); err == nil || !strings.Contains(err.Error(), "no configuration path") {
		t.Fatalf("missing path error = %v", err)
	}
}
