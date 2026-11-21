//go:build !plan9

package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestConfigDialogNeverPrefillsOrRendersSavedPassword(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = configPage
	m.setup = setupState{
		path:        "/tmp/qbitctl-config.json",
		url:         "http://qbit.example",
		user:        "admin",
		hasPassword: true,
		tuiDefault:  true,
	}
	m.openConfigDialog()
	if got := m.dialog.value(2); got != "" {
		t.Fatalf("password field was prefilled with %q", got)
	}
	m.dialog.fields[2].input.SetValue("super-secret")
	frame := ansi.Strip(m.dialogView(80, 24))
	if strings.Contains(frame, "super-secret") {
		t.Fatal("config dialog rendered the typed password")
	}
	if !strings.Contains(frame, "••••") {
		t.Fatalf("config dialog did not render a password mask: %q", frame)
	}
	if got := m.dialog.value(3); got != "yes" {
		t.Fatalf("TUI default field = %q, want yes", got)
	}
}

func TestConfigChoiceParsing(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "yes", want: true},
		{value: "on", want: true},
		{value: "no", want: false},
		{value: "off", want: false},
	} {
		got, err := parseTUIChoice(test.value)
		if err != nil || got != test.want {
			t.Errorf("parseTUIChoice(%q) = %v, %v; want %v, nil", test.value, got, err, test.want)
		}
	}
	if _, err := parseTUIChoice("maybe"); err == nil {
		t.Fatal("parseTUIChoice accepted an ambiguous value")
	}
}

func TestConfigPageShowsOnlyPasswordMetadata(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.width, m.height = 80, 24
	m.page = configPage
	m.setup = setupState{url: "http://qbit.example", user: "admin", hasPassword: true}
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "saved (hidden)") {
		t.Fatal("config page did not show saved-password metadata")
	}
	if strings.Contains(frame, "password") && strings.Contains(frame, "secret") {
		t.Fatal("config page exposed a password-like value")
	}
}

func TestFooterNoticeExpiry(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = configPage
	enabled := true
	saved := workDone{kind: configWork, setDefault: &enabled}
	const confirmation = "TUI startup preference saved"
	_, cmd := m.Update(saved)
	if cmd == nil || m.notice != confirmation {
		t.Fatal("startup preference save did not show a timed confirmation")
	}
	firstExpiry := noticeExpired(m.noticeID)

	m.Update(tea.KeyPressMsg{Code: '2'})
	if m.page != searchPage || m.notice != confirmation {
		t.Fatal("switching tabs should preserve the active confirmation")
	}
	m.Update(saved)
	secondExpiry := noticeExpired(m.noticeID)
	m.Update(firstExpiry)
	if m.notice != confirmation {
		t.Fatal("an older timer cleared a newer confirmation")
	}
	m.Update(secondExpiry)
	if m.notice != "" {
		t.Fatal("confirmation did not clear when its timer expired")
	}

	const action = "Torrent submitted"
	_, cmd = m.Update(workDone{kind: actionWork, notice: action})
	if cmd == nil || m.notice != action {
		t.Fatal("action did not show a timed confirmation")
	}
	actionExpiry := noticeExpired(m.noticeID)
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'f'})
	const search = "Run a search to filter its results"
	if cmd == nil || m.notice != search {
		t.Fatal("a keyboard action did not show a timed status")
	}
	m.Update(actionExpiry)
	if m.notice != search {
		t.Fatal("confirmation timer cleared a newer status message")
	}
	m.Update(noticeExpired(m.noticeID))
	if m.notice != "" {
		t.Fatal("keyboard status did not expire")
	}
}

func TestFooterErrorExpiresWhileWorkContinues(t *testing.T) {
	m := newModel(t.Context(), nil)
	_, cmd := m.Update(workDone{kind: configWork, err: errors.New("Could not save preferences")})
	if cmd == nil || !m.failed {
		t.Fatal("failure did not show a timed error")
	}
	m.busy = "Refreshing queue"
	m.Update(noticeExpired(m.noticeID))
	if m.notice != "" || m.failed || m.busy != "Refreshing queue" {
		t.Fatal("expiring an error should clear the error and preserve live progress")
	}
}

func TestSetupReadinessKeepsEphemeralPasswordUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"url":"http://qbit.example","user":"admin"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := readSetupState(client.Options{ConfigPath: path, Pass: "ephemeral"})
	if err != nil {
		t.Fatal(err)
	}
	if !state.ready {
		t.Fatal("ephemeral password did not keep setup ready")
	}
	if state.hasPassword {
		t.Fatal("ephemeral password was presented as saved password")
	}
}
