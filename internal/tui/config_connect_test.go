//go:build !plan9

package tui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/internal/qbtest"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestConnectionProgressIsVisibleBeforeLogin(t *testing.T) {
	originalConnect := connectClient
	connectClient = func(context.Context, *client.Options, io.Writer, io.Writer) (*client.App, error) {
		return nil, errors.New("login failed")
	}
	defer func() { connectClient = originalConnect }()

	m := newModel(t.Context(), nil)
	m.page = queuePage
	m.connectTarget = "qbt.test"
	m.connect = &client.Options{URL: "https://qbt.test"}
	initCommand := m.Init()
	if m.busy != "Connecting to qbt.test" {
		t.Fatalf("busy label = %q", m.busy)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Connecting to qbt.test…") {
		t.Fatalf("queue view did not show connection progress:\n%s", ansi.Strip(m.View().Content))
	}
	batch, ok := initCommand().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("Init command = %T, want a batch with connection work", initCommand())
	}
	message := batch[len(batch)-1]()
	done, ok := message.(workDone)
	if !ok {
		t.Fatalf("connection command returned %T, want workDone", message)
	}
	m.finishWork(done)
	m.workers.Wait()
}

func TestSavedConnectionFailureOpensConfig(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.setup = setupState{path: "/tmp/qbitctl.json", url: "https://qbt.test", user: "admin", hasPassword: true, ready: true}
	m.page = queuePage
	m.connectTarget = "qbt.test"
	m.startWork(connectWork, "Connecting to qbt.test", func(context.Context) workDone {
		return workDone{err: errors.New("Login failed: connection refused")}
	})
	m.finishWork(workDone{kind: connectWork, err: errors.New("Login failed: connection refused"), setup: m.setup})
	if m.page != configPage || m.dialog == nil {
		t.Fatalf("connection failure did not open Config: page=%d dialog=%v", m.page, m.dialog != nil)
	}
	if m.dialog.status != "Connection failed: Login failed: connection refused" {
		t.Fatalf("dialog status = %q", m.dialog.status)
	}
	if !m.failed || !strings.Contains(m.notice, "connection refused") {
		t.Fatalf("failure notice = %q failed=%v", m.notice, m.failed)
	}
	m.workers.Wait()
}

func TestExplicitConnectionFailureReturnsError(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.connectExplicit = true
	m.connectTarget = "qbt.test"
	command := m.startWork(connectWork, "Connecting to qbt.test", func(context.Context) workDone {
		return workDone{err: errors.New("Login failed: bad credentials")}
	})
	done := command().(workDone)
	quit := m.finishWork(done)
	m.workers.Wait()
	if m.exitErr == nil || m.exitErr.Error() != "Login failed: bad credentials" {
		t.Fatalf("exit error = %v", m.exitErr)
	}
	if quit == nil {
		t.Fatal("explicit failure did not quit the program")
	}
	message := quit()
	if _, ok := message.(tea.QuitMsg); !ok {
		t.Fatalf("quit command returned %T", message)
	}
}

func TestEscCancelsConnectionAndOpensConfig(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.setup = setupState{url: "https://qbt.test", user: "admin"}
	m.connectTarget = "qbt.test"
	command := m.startWork(connectWork, "Connecting to qbt.test", func(ctx context.Context) workDone {
		<-ctx.Done()
		return workDone{err: ctx.Err(), setup: m.setup}
	})
	if cmd := m.handleKey("esc"); cmd != nil {
		t.Fatalf("Esc returned command %T, want no command", cmd)
	}
	if !m.connectCanceled || m.busy != "Canceling connection" {
		t.Fatalf("Esc state = canceled=%v busy=%q", m.connectCanceled, m.busy)
	}
	done := command().(workDone)
	m.finishWork(done)
	m.workers.Wait()
	if m.page != configPage || m.dialog == nil || m.dialog.status != "Connection canceled" {
		t.Fatalf("canceled connection state = page %d dialog %v status %q", m.page, m.dialog != nil, func() string {
			if m.dialog == nil {
				return ""
			}
			return m.dialog.status
		}())
	}
	if m.notice != "Connection canceled" || !m.failed {
		t.Fatalf("canceled connection notice = %q failed=%v", m.notice, m.failed)
	}
}

func TestCanceledConnectionDoesNotAdoptLateSessionAndLogsOut(t *testing.T) {
	app, logoutCalls := newMockApp(t)
	m := newModel(t.Context(), nil)
	m.setup = setupState{url: "https://qbt.test", user: "admin"}
	command := m.startWork(connectWork, "Connecting to qbt.test", func(ctx context.Context) workDone {
		<-ctx.Done()
		return workDone{app: app, setup: m.setup}
	})
	if cmd := m.handleKey("esc"); cmd != nil {
		t.Fatalf("Esc returned command %T, want no command", cmd)
	}
	done := command().(workDone)
	m.finishWork(done)
	m.workers.Wait()
	if m.page != configPage || m.backend != nil {
		t.Fatalf("canceled connection adopted state: page=%d backend=%v", m.page, m.backend)
	}
	if got := logoutCalls.Load(); got != 1 {
		t.Fatalf("late app logout calls = %d, want 1", got)
	}
}

func TestCtrlCCancelsConnectionAndLogsOutLateSession(t *testing.T) {
	app, logoutCalls := newMockApp(t)
	m := newModel(t.Context(), nil)
	m.setup = setupState{url: "https://qbt.test", user: "admin"}
	command := m.startWork(connectWork, "Connecting to qbt.test", func(ctx context.Context) workDone {
		<-ctx.Done()
		return workDone{app: app, setup: m.setup}
	})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("Ctrl+C returned command %T, want no command", cmd)
	}
	if !m.quitting || m.busy != "Closing session" {
		t.Fatalf("Ctrl+C state = quitting=%v busy=%q", m.quitting, m.busy)
	}
	done := command().(workDone)
	quit := m.finishWork(done)
	m.workers.Wait()
	if quit == nil {
		t.Fatal("Ctrl+C late connection did not quit")
	}
	message := quit()
	if _, ok := message.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C quit command returned %T", message)
	}
	if got := logoutCalls.Load(); got != 1 {
		t.Fatalf("late app logout calls = %d, want 1", got)
	}
}

func newMockApp(t *testing.T) (*client.App, *atomic.Int32) {
	t.Helper()
	server := qbtest.New(t)
	var logoutCalls atomic.Int32
	restore := client.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{
			Jar: jar,
			Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/v2/auth/logout" {
					logoutCalls.Add(1)
				}
				recorder := httptest.NewRecorder()
				server.Handle(recorder, req)
				return recorder.Result(), nil
			}),
		}
	})
	t.Cleanup(restore)
	creds := server.Creds()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("create mock config: %v", err)
	}
	app, err := client.NewClientWithOutput(context.Background(), &client.Options{
		ConfigPath: configPath,
		URL:        creds.URL,
		User:       creds.User,
		Pass:       creds.Pass,
	}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("create mock app: %v", err)
	}
	return app, &logoutCalls
}

func TestConnectionTargetOmitsCredentials(t *testing.T) {
	if got := connectionTarget(client.Options{URL: "https://admin:secret@example.com:8443"}, setupState{}); got != "example.com:8443" {
		t.Fatalf("connectionTarget = %q", got)
	}
	if got := connectionTarget(client.Options{URL: "https://admin:secret@%invalid"}, setupState{}); got != "server" {
		t.Fatalf("malformed connectionTarget = %q", got)
	}
}
