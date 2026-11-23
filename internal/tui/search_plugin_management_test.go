//go:build !plan9

package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type cancelSearchPluginTestService struct {
	service
	started chan struct{}
}

func (s *cancelSearchPluginTestService) searchPlugins(context.Context) ([]searchPluginInfo, error) {
	return nil, nil
}

func (s *cancelSearchPluginTestService) searchPluginAction(ctx context.Context, _, _ string) error {
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}

type searchPluginManagementTestService struct {
	service
	plugins []searchPluginInfo
	actions []string
}

func (s *searchPluginManagementTestService) searchPlugins(context.Context) ([]searchPluginInfo, error) {
	return append([]searchPluginInfo(nil), s.plugins...), nil
}

func (s *searchPluginManagementTestService) searchPluginAction(_ context.Context, action, name string) error {
	s.actions = append(s.actions, action+":"+name)
	for i := range s.plugins {
		if s.plugins[i].Name != name {
			continue
		}
		switch action {
		case searchPluginActionEnable:
			s.plugins[i].Enabled = true
		case searchPluginActionDisable:
			s.plugins[i].Enabled = false
		case searchPluginActionRemove:
			s.plugins = append(s.plugins[:i], s.plugins[i+1:]...)
		}
		break
	}
	return nil
}

func TestSearchPluginManagementRefreshesAndPreservesDisabledDefault(t *testing.T) {
	service := &searchPluginManagementTestService{
		service: &recordingService{},
		plugins: []searchPluginInfo{{Name: "jackett", FullName: "Jackett", Enabled: true}},
	}
	m := newModel(t.Context(), service)
	m.page = searchPage
	m.searchPlugins = append([]searchPluginInfo(nil), service.plugins...)
	m.searchPluginsAttempted = true
	m.savedSearchPlugin = "jackett"
	m.searchPlugin = "jackett"

	runTUIWork(t, m, m.openSearchPluginPicker())
	m.update(tea.KeyPressMsg{Code: 'm'})
	if m.dialog == nil || m.dialog.kind != dialogSearchPluginManage {
		t.Fatalf("manage dialog = %#v", m.dialog)
	}
	if len(m.dialog.items) < 3 || m.dialog.items[2].action != searchPluginManagePrefix+"jackett" {
		t.Fatalf("manage items = %#v", m.dialog.items)
	}
	m.dialog.selected = 2
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || m.dialog.kind != dialogSearchPluginActions {
		t.Fatalf("action dialog = %#v", m.dialog)
	}
	m.dialog.selected = 0
	staleGeneration := m.searchPluginGeneration
	m.searchPluginsLoading = true
	oldCanceled := false
	m.searchPluginCancel = func() { oldCanceled = true }
	cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runTUIWork(t, m, cmd)
	m.finishWork(workDone{
		kind: searchPluginsWork, pluginGeneration: staleGeneration,
		plugins: []searchPluginInfo{{Name: "jackett", Enabled: true}},
	})
	if !oldCanceled || m.searchPluginsLoading {
		t.Fatal("the plugin mutation did not discard the in-flight picker refresh")
	}
	if len(service.actions) != 1 || service.actions[0] != searchPluginActionDisable+":jackett" {
		t.Fatalf("plugin actions = %v", service.actions)
	}
	if m.searchPlugin != client.DefaultSearchPlugins {
		t.Fatalf("disabled effective default = %q, want %q", m.searchPlugin, client.DefaultSearchPlugins)
	}
	if m.savedSearchPlugin != "jackett" {
		t.Fatalf("disabled saved default = %q, want jackett", m.savedSearchPlugin)
	}
	if m.dialog == nil || m.dialog.kind != dialogSearchPluginManage {
		t.Fatalf("dialog after refresh = %#v, want management dialog", m.dialog)
	}

	// Re-enable through the same management surface. The saved exact ID should
	// become effective again after the refreshed catalog is validated.
	m.dialog.selected = 2
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.dialog.selected = 0
	runTUIWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if len(service.actions) != 2 || service.actions[1] != searchPluginActionEnable+":jackett" {
		t.Fatalf("re-enable actions = %v", service.actions)
	}
	if m.searchPlugin != "jackett" || m.savedSearchPlugin != "jackett" {
		t.Fatalf("re-enabled provider = effective %q saved %q", m.searchPlugin, m.savedSearchPlugin)
	}
}

func TestSearchPluginManagementEscapeCancelsAndKeepsDialog(t *testing.T) {
	service := &cancelSearchPluginTestService{
		service: &recordingService{},
		started: make(chan struct{}),
	}
	m := newModel(t.Context(), service)
	m.dialog = &dialog{
		kind:       dialogSearchPluginActions,
		title:      "Manage Jackett",
		pluginName: "jackett",
		items:      []dialogItem{{label: "Disable plugin", action: searchPluginActionDisable}},
	}
	cmd := m.startSearchPluginAction(searchPluginActionDisable, "jackett")
	if cmd == nil {
		t.Fatal("plugin action did not start")
	}
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("plugin action did not start")
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.dialog == nil || m.dialog.kind != dialogSearchPluginActions {
		t.Fatalf("Escape closed management dialog: %#v", m.dialog)
	}
	if m.busy != "Canceling plugin action" {
		t.Fatalf("busy status after Escape = %q", m.busy)
	}
	message := cmd()
	done, ok := message.(workDone)
	if !ok {
		t.Fatalf("plugin cancellation command returned %T", message)
	}
	m.finishWork(done)
	if m.dialog == nil || m.dialog.status != "Plugin request canceled" {
		t.Fatalf("dialog after cancellation = %#v", m.dialog)
	}
	if m.busy != "" {
		t.Fatalf("busy status after cancellation = %q", m.busy)
	}
}

func TestSearchPluginManagementPaletteOffersCancellation(t *testing.T) {
	m := newModel(t.Context(), &recordingService{})
	m.busy = "Managing search plugins"
	m.work = searchPluginManageWork
	canceled := false
	m.cancel = func() { canceled = true }
	items := m.paletteCommands()
	var found *dialogItem
	for i := range items {
		if items[i].action == "cancel-plugin-request" {
			found = &items[i]
			break
		}
	}
	if found == nil || found.label != "Cancel plugin request" {
		t.Fatalf("palette items omitted plugin cancellation: %v", items)
	}
	m.openAction("cancel-plugin-request")
	if !canceled || m.busy != "Canceling plugin action" {
		t.Fatalf("palette cancellation did not cancel request: canceled=%v busy=%q", canceled, m.busy)
	}
}

func TestSearchPluginManagementPaletteIsReachableFromModalAndRefreshes(t *testing.T) {
	service := &cancelSearchPluginTestService{
		service: &recordingService{},
		started: make(chan struct{}),
	}
	m := newModel(t.Context(), service)
	m.dialog = &dialog{
		kind:       dialogSearchPluginActions,
		title:      "Manage Jackett",
		pluginName: "jackett",
		items:      []dialogItem{{label: "Disable plugin", action: searchPluginActionDisable}},
	}
	cmd := m.startSearchPluginAction(searchPluginActionDisable, "jackett")
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("plugin action did not start")
	}
	m.update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.dialog == nil || m.dialog.kind != dialogPalette {
		t.Fatalf("Ctrl+P did not open the palette from plugin modal: %#v", m.dialog)
	}
	selected := -1
	for i, item := range m.dialog.items {
		if item.action == "cancel-plugin-request" {
			selected = i
			break
		}
	}
	if selected < 0 {
		t.Fatalf("palette omitted plugin cancellation: %v", m.dialog.items)
	}
	m.dialog.selected = selected
	// Escape is the palette's cancellation shortcut while the request is
	// active. The modal remains visible long enough for the completed work
	// message to rebuild it without the stale cancellation item.
	m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.busy != "Canceling plugin action" {
		t.Fatalf("palette action did not cancel plugin request: %q", m.busy)
	}
	message := cmd()
	m.Update(message)
	if m.dialog == nil || m.dialog.kind != dialogPalette {
		t.Fatalf("palette closed after cancellation: %#v", m.dialog)
	}
	for _, item := range m.dialog.items {
		if item.action == "cancel-plugin-request" {
			t.Fatal("completed plugin cancellation remained in palette")
		}
	}
}
