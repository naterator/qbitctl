//go:build !plan9

package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

// switchProfile changes only the current TUI session. It deliberately does
// not write default_profile; users who want a persistent startup change use
// setStartupProfile (or the CLI `profile use` command).
func (m *model) switchProfile(name string) tea.Cmd {
	name, err := client.ValidateAndNormalizeProfileName(name)
	if err != nil {
		// ValidateAndNormalizeProfileName is provided by the client package so
		// the TUI and CLI enforce the same stable profile-name contract.
		m.setNotice(err.Error(), true)
		return nil
	}
	if strings.TrimSpace(m.setup.profile) == name {
		m.setNotice("Already using profile "+name, false)
		return nil
	}
	if strings.TrimSpace(m.configPath) == "" {
		m.setNotice("No configuration path is available", true)
		return nil
	}
	opts := client.Options{ConfigPath: m.configPath, Profile: name, UseCompatibilityCache: true}
	setup, err := client.ReadSetup(opts)
	if err != nil {
		m.setNotice("Profile unavailable: "+err.Error(), true)
		return nil
	}
	state := setupStateFromClient(savedPasswordMetadata(setup))

	// Supersede primary work while keeping the active session and its snapshot.
	// The generation prevents a canceled request from clearing the new attempt.
	previous := m.workFinished
	if m.cancel != nil {
		m.cancel()
	}
	m.cancel, m.busy = nil, ""
	m.remoteGeneration++
	m.connectExplicit = false
	m.connectCanceled = false
	return m.startWork(connectWork, "Connecting to profile "+name, func(ctx context.Context) workDone {
		// A failed switch leaves the old backend in use. Let its canceled
		// request finish before the view can submit another on that backend.
		if previous != nil {
			<-previous
		}
		if err := ctx.Err(); err != nil {
			return workDone{setup: state, profileSwitch: true, err: err}
		}
		app, err := connectClient(ctx, &opts, io.Discard, io.Discard)
		return workDone{app: app, setup: state, profileSwitch: true, err: err}
	})
}

func (m *model) finishProfileSwitch(done workDone, canceled bool) tea.Cmd {
	canceled = canceled || errors.Is(done.err, context.Canceled)
	if canceled || done.err != nil || done.app == nil {
		if m.backend == nil {
			m.setup = done.setup
			if canceled {
				return m.finishCanceledConnection(done.app)
			}
			m.page = configPage
			m.openConfigDialog()
		}
		if done.app != nil {
			done.app.Logout()
		}
		status := "Connection failed: client returned no session"
		if canceled {
			status = "Connection canceled"
		} else if done.err != nil {
			status = "Connection failed: " + done.err.Error()
		}
		m.setNotice(status, true)
		if m.backend == nil && m.dialog != nil {
			m.dialog.status = status
		}
		return nil
	}

	oldApp, owned, oldWorkers := m.app, m.ownsApp, m.remoteWorkers
	m.remoteWorkers = &sync.WaitGroup{}
	m.adoptApp(done.app, done.setup)
	if oldApp != nil && owned {
		m.workers.Add(1)
		go func() {
			defer m.workers.Done()
			oldWorkers.Wait()
			oldApp.Logout()
		}()
	}
	return m.refresh()
}

// openProfilePicker loads only local profile metadata. Authentication starts
// after the user makes a choice, keeping the picker responsive and avoiding a
// connection for an accidental cursor movement.
func (m *model) openProfilePicker() tea.Cmd {
	return m.openProfilePickerMode(false)
}

func (m *model) openStartupProfilePicker() tea.Cmd {
	return m.openProfilePickerMode(true)
}

func (m *model) openProfilePickerMode(startup bool) tea.Cmd {
	path := strings.TrimSpace(m.configPath)
	if path == "" {
		path = strings.TrimSpace(m.setup.path)
	}
	if path == "" {
		m.setNotice("No configuration path is available", true)
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		m.setNotice("Profiles unavailable: "+err.Error(), true)
		return nil
	}
	profiles, defaultName, err := client.ReadProfiles(path)
	if err != nil {
		m.setNotice("Profiles unavailable: "+err.Error(), true)
		return nil
	}
	if len(profiles) == 0 {
		m.setNotice("No saved server profiles", false)
		return nil
	}
	if len(profiles) == 1 && profiles[0].Name == client.DefaultProfileName && strings.TrimSpace(m.setup.profile) == "" {
		m.setNotice("No named profiles yet; use qbitctl profile add NAME", false)
		return nil
	}
	items := make([]dialogItem, 0, len(profiles))
	for _, profile := range profiles {
		description := strings.TrimSpace(profile.URL)
		if profile.Name == m.setup.profile {
			description += " · current"
		}
		if profile.Default {
			description += " · startup default"
		}
		items = append(items, dialogItem{label: profile.Name, action: profile.Name, description: description})
	}
	selectedName := m.setup.profile
	if startup {
		selectedName = defaultName
	}
	selected := 0
	for i, item := range items {
		if item.action == selectedName {
			selected = i
			break
		}
	}
	title := "Switch server profile"
	if startup {
		title = "Choose startup profile"
	}
	m.dialog = &dialog{kind: dialogProfilePicker, title: title, items: items, selected: selected, profileStartup: startup}
	return nil
}

// setStartupProfile persists the selected startup default while leaving the
// active session untouched. The separate operation is useful from the Config
// page where session and startup choices should be visibly distinct.
func (m *model) setStartupProfile(name string) error {
	name, err := client.ValidateAndNormalizeProfileName(name)
	if err != nil {
		return err
	}
	path := strings.TrimSpace(m.configPath)
	if path == "" {
		return fmt.Errorf("no configuration path is available")
	}
	if err := client.SetDefaultProfile(path, name); err != nil {
		return err
	}
	m.setNotice("Startup profile saved: "+name, false)
	return nil
}
