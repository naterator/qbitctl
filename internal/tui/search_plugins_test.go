//go:build !plan9

package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type searchPluginTestService struct {
	service
	plugins []client.SearchPlugin
	err     error
	started chan struct{}
	release chan struct{}
}

func (s *searchPluginTestService) searchPlugins(ctx context.Context) ([]searchPluginInfo, error) {
	if s.started != nil {
		close(s.started)
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return append([]searchPluginInfo(nil), s.plugins...), s.err
}

func TestSearchPluginDiscoveryUsesCurrentConnectionAndUpdatesDialog(t *testing.T) {
	service := &searchPluginTestService{
		service: &recordingService{},
		plugins: []client.SearchPlugin{
			{Name: "jackett", FullName: "Jackett", Enabled: true},
			{Name: "disabled", FullName: "Disabled provider", Enabled: false},
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	m := newModel(t.Context(), service)

	cmd := m.goTo(searchPage)
	if cmd == nil {
		t.Fatal("entering Search did not start provider discovery")
	}
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("provider discovery did not start")
	}
	if !m.searchPluginsLoading {
		t.Fatal("provider discovery was not marked loading")
	}
	if m.busy != "" {
		t.Fatalf("provider discovery blocked the model with busy=%q", m.busy)
	}

	m.openSearchDialog()
	if m.dialog == nil || m.dialog.title != "Search Enabled search plugins" {
		t.Fatalf("loading search dialog = %#v, want neutral enabled title", m.dialog)
	}
	close(service.release)
	message := cmd()
	done, ok := message.(workDone)
	if !ok {
		t.Fatalf("discovery command returned %T, want workDone", message)
	}
	m.finishWork(done)
	if m.dialog.title != "Search Jackett" {
		t.Fatalf("discovered search dialog title = %q, want full provider name", m.dialog.title)
	}
	if !strings.Contains(m.dialog.fields[1].help, "Disabled provider") {
		t.Fatalf("provider help = %q, want installed catalog", m.dialog.fields[1].help)
	}
}

func TestSearchPluginDiscoveryFailureIsStaleAndRetryable(t *testing.T) {
	service := &searchPluginTestService{
		service: &recordingService{},
		plugins: []client.SearchPlugin{{Name: "jackett", FullName: "Jackett", Enabled: true}},
		err:     errors.New("catalog unavailable"),
	}
	m := newModel(t.Context(), service)
	m.searchPlugins = append([]searchPluginInfo(nil), service.plugins...)
	m.searchPluginsAttempted = true
	m.openSearchDialog()
	// A fresh discovery is allowed even though the last catalog was successful.
	m.searchPluginsAttempted = false
	cmd := m.ensureSearchPlugins()
	if cmd == nil {
		t.Fatal("retry did not start after a temporary catalog failure")
	}
	done := cmd().(workDone)
	m.finishWork(done)
	if !m.searchPluginsStale || m.searchPluginsError == "" {
		t.Fatalf("failed discovery state = stale=%v error=%q", m.searchPluginsStale, m.searchPluginsError)
	}
	if m.notice != "" || m.failed {
		t.Fatalf("catalog failure polluted global notice: %q failed=%v", m.notice, m.failed)
	}
	if !strings.Contains(m.dialog.fields[1].help, "stale") {
		t.Fatalf("stale provider help = %q", m.dialog.fields[1].help)
	}
}

func TestSearchPluginLabelsAndSelections(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.searchPlugins = []searchPluginInfo{
		{Name: "prowlarr", FullName: "Prowlarr", Enabled: true},
		{Name: "jackett", FullName: "Jackett", Enabled: true},
	}
	if got := m.searchPluginLabel(client.DefaultSearchPlugins); got != "Enabled search plugins" {
		t.Fatalf("enabled label = %q", got)
	}
	if got := m.searchPluginLabel("prowlarr"); got != "Prowlarr" {
		t.Fatalf("named label = %q", got)
	}
	if got := m.searchPluginLabel("prowlarr|jackett"); got != "Prowlarr | Jackett" {
		t.Fatalf("multi label = %q", got)
	}
	if got := m.searchPluginSelection(" Prowlarr | jackett "); got != "prowlarr|jackett" {
		t.Fatalf("resolved selection = %q", got)
	}
	if got := m.searchPluginSelection("prowlarr||jackett"); got != "prowlarr||jackett" {
		t.Fatalf("invalid selection was rewritten to %q", got)
	}

	m.searchPlugins = []searchPluginInfo{{Name: "prowlarr", FullName: "Prowlarr", Enabled: true}}
	if got := m.searchPluginLabel(client.DefaultSearchPlugins); got != "Prowlarr" {
		t.Fatalf("sole enabled label = %q, want full name", got)
	}
}

func TestSearchPluginPickerSetsNextSearchWithoutChangingResults(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.page = searchPage
	m.searchPlugins = []searchPluginInfo{
		{Name: "prowlarr", FullName: "Prowlarr", Enabled: true},
		{Name: "jackett", FullName: "Jackett", Enabled: false},
	}
	m.searchPluginsAttempted = true
	m.searchPlugin = client.DefaultSearchPlugins
	m.query = "Ubuntu"
	m.replaceSearchResults([]client.SearchResult{{ID: 101, Name: "Ubuntu ISO", Source: "prowlarr"}})

	// Opening the picker may refresh the catalog, but it must never launch a
	// search or alter the loaded results.
	m.handleKey("p")
	if m.dialog == nil || m.dialog.kind != dialogSearchPlugins {
		t.Fatalf("provider picker = %#v, want search provider dialog", m.dialog)
	}
	if m.dialog.title != "Choose search provider" {
		t.Fatalf("provider picker title = %q, want Choose search provider", m.dialog.title)
	}
	frame := m.View().Content
	if !strings.Contains(frame, "Jackett") || !strings.Contains(frame, "enter") || !strings.Contains(frame, "select") {
		t.Fatalf("provider picker frame omitted provider list/footer: %q", frame)
	}
	var selected int
	for i, item := range m.dialog.items {
		if item.action == "prowlarr" {
			selected = i
			if item.label != "Prowlarr" {
				t.Fatalf("provider label = %q, want display name Prowlarr", item.label)
			}
		}
	}
	m.dialog.selected = selected
	m.update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.dialog != nil {
		t.Fatal("provider picker remained open after selection")
	}
	if m.searchPlugin != "prowlarr" {
		t.Fatalf("selected provider = %q, want prowlarr", m.searchPlugin)
	}
	if m.busy != "" {
		t.Fatalf("provider selection started a search: busy=%q", m.busy)
	}
	if len(m.results) != 1 || m.results[0].Name != "Ubuntu ISO" {
		t.Fatalf("provider selection changed results: %+v", m.results)
	}
	if view := strings.Join(m.viewSearchList(80, 20), "\n"); !strings.Contains(view, "Provider: Prowlarr") {
		t.Fatalf("search page omitted selected provider: %q", view)
	}
	m.searchPlugin = "PROWLARR"
	m.handleKey("p")
	if got := m.dialog.items[m.dialog.selected].action; got != "prowlarr" {
		t.Fatalf("picker highlighted %q for the selected alias, want prowlarr", got)
	}
}

func TestSearchPluginPickerRefreshesCatalogInPlace(t *testing.T) {
	service := &searchPluginTestService{
		service: &recordingService{},
		plugins: []client.SearchPlugin{{Name: "old", FullName: "Old provider", Enabled: true}},
		release: make(chan struct{}),
	}
	m := newModel(t.Context(), service)
	m.page = searchPage
	m.searchPlugins = []searchPluginInfo{{Name: "old", FullName: "Old provider", Enabled: true}}
	m.searchPluginsAttempted = true

	cmd := m.openSearchPluginPicker()
	if cmd == nil || !m.searchPluginsLoading {
		t.Fatal("reopening provider picker did not refresh the catalog")
	}
	if len(m.dialog.items) != 3 || m.dialog.items[2].label != "Old provider" {
		t.Fatalf("picker did not keep the existing catalog while refreshing: %+v", m.dialog.items)
	}
	service.plugins = []client.SearchPlugin{{Name: "new", FullName: "New provider", Enabled: true}}
	close(service.release)
	message := cmd()
	done, ok := message.(workDone)
	if !ok {
		t.Fatalf("provider refresh command returned %T, want workDone", message)
	}
	m.finishWork(done)
	if m.dialog == nil || len(m.dialog.items) != 3 || m.dialog.items[2].label != "New provider" {
		t.Fatalf("picker did not refresh in place: %#v", m.dialog)
	}
}

func TestSearchPluginDiscoveryIgnoresOldConnectionReply(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.searchPluginGeneration = 2
	m.searchPluginsLoading = true
	m.searchPluginCancel = func() {}
	m.finishWork(workDone{kind: searchPluginsWork, pluginGeneration: 1, plugins: []searchPluginInfo{{Name: "old"}}})
	if !m.searchPluginsLoading || m.searchPluginCancel == nil {
		t.Fatal("old provider reply cleared current discovery state")
	}
}
