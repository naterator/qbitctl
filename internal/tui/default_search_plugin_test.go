//go:build !plan9

package tui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/naterator/qbitctl/pkg/client"
)

type savedDefaultSearchService struct {
	service
	plugins      []client.SearchPlugin
	searchCalls  []string
	catalogCalls int
}

func (s *savedDefaultSearchService) search(context.Context, string, string, time.Duration) ([]client.SearchResult, string, error) {
	return nil, "", nil
}

func (s *savedDefaultSearchService) searchPlugins(context.Context) ([]searchPluginInfo, error) {
	s.catalogCalls++
	return append([]searchPluginInfo(nil), s.plugins...), nil
}

func (s *savedDefaultSearchService) searchWithCatalog(_ context.Context, _ string, plugin string, _ time.Duration, _ []searchPluginInfo) ([]client.SearchResult, string, error) {
	s.searchCalls = append(s.searchCalls, plugin)
	return nil, "", nil
}

func TestSavedSearchPluginLoadsAndPersistsFromPicker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := client.SetDefaultSearchPlugin(path, "prowlarr"); err != nil {
		t.Fatal(err)
	}

	m := newModel(t.Context(), nil)
	m.loadPreferences(path)
	if m.savedSearchPlugin != "prowlarr" || m.searchPlugin != "prowlarr" || !m.searchPluginDefaultActive {
		t.Fatalf("loaded default state = saved=%q effective=%q active=%v", m.savedSearchPlugin, m.searchPlugin, m.searchPluginDefaultActive)
	}

	if err := m.setDefaultSearchPlugin("all"); err != nil {
		t.Fatal(err)
	}
	if got, err := client.ReadDefaultSearchPlugin(path); err != nil || got != "all" {
		t.Fatalf("picker preference = %q, %v; want all", got, err)
	}
}

func TestSavedSearchPluginFallsBackWithoutRewritingAndRecovers(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.savedSearchPlugin = "prowlarr"
	m.searchPlugin = "prowlarr"
	m.searchPlugins = []searchPluginInfo{{Name: "prowlarr", Enabled: false}}
	m.searchPluginsAttempted = true
	m.searchPluginsStale = false

	m.revalidateSavedSearchPlugin(false)
	if m.searchPlugin != client.DefaultSearchPlugins || m.savedSearchPlugin != "prowlarr" {
		t.Fatalf("disabled default = saved=%q effective=%q; want saved prowlarr/effective enabled", m.savedSearchPlugin, m.searchPlugin)
	}

	m.searchPlugins = []searchPluginInfo{{Name: "prowlarr", FullName: "Prowlarr", Enabled: true}}
	m.revalidateSavedSearchPlugin(false)
	if m.searchPlugin != "prowlarr" || m.savedSearchPlugin != "prowlarr" {
		t.Fatalf("re-enabled default = saved=%q effective=%q; want prowlarr", m.savedSearchPlugin, m.searchPlugin)
	}
}

func TestManualSearchOverrideSurvivesDefaultValidation(t *testing.T) {
	m := newModel(t.Context(), &recordingService{})
	m.savedSearchPlugin = "prowlarr"
	m.searchPlugin = "prowlarr"
	m.searchPlugins = []searchPluginInfo{{Name: "prowlarr", Enabled: true}}
	m.searchPluginsAttempted = true
	if cmd := m.launchSearch("Ubuntu", "jackett", time.Second); cmd == nil {
		t.Fatal("manual search did not start")
	} else {
		done, ok := cmd().(workDone)
		if !ok {
			t.Fatalf("search command returned %T, want workDone", done)
		}
		m.finishWork(done)
	}
	if m.searchPlugin != "jackett" || m.searchPluginDefaultActive {
		t.Fatalf("manual search state = provider=%q active=%v", m.searchPlugin, m.searchPluginDefaultActive)
	}

	m.revalidateSavedSearchPlugin(false)
	if m.searchPlugin != "jackett" {
		t.Fatalf("manual provider was overwritten by default validation: %q", m.searchPlugin)
	}
}

func TestSavedProviderIdentityIsCaseSensitive(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.savedSearchPlugin = "prowlarr"
	m.searchPlugin = "jackett"
	m.searchPluginDefaultActive = false
	if m.searchSelectionFollowsDefault("PROWLARR") {
		t.Fatal("case-colliding provider ID was treated as the saved default")
	}
	if !m.searchSelectionFollowsDefault("prowlarr") {
		t.Fatal("exact saved provider ID was not recognized")
	}
}

func TestDefaultSearchValidatesFreshCatalogAndUsesEffectiveProvider(t *testing.T) {
	service := &savedDefaultSearchService{
		service:     &recordingService{},
		plugins:     []client.SearchPlugin{{Name: "prowlarr", Enabled: false}},
		searchCalls: nil,
	}
	m := newModel(t.Context(), service)
	m.savedSearchPlugin = "prowlarr"
	m.searchPlugin = "prowlarr"

	cmd := m.launchSearch("Ubuntu", m.searchPlugin, time.Second)
	if cmd == nil {
		t.Fatal("default search did not start")
	}
	done, ok := cmd().(workDone)
	if !ok {
		t.Fatalf("search command returned %T, want workDone", done)
	}
	if done.effectivePlugin != client.DefaultSearchPlugins {
		t.Fatalf("effective provider = %q, want enabled fallback", done.effectivePlugin)
	}
	if service.catalogCalls != 1 || len(service.searchCalls) != 1 || service.searchCalls[0] != client.DefaultSearchPlugins {
		t.Fatalf("fresh validation calls = catalog %d, searches %v; want one catalog and enabled search", service.catalogCalls, service.searchCalls)
	}
	m.finishWork(done)
	if m.searchPlugin != client.DefaultSearchPlugins || m.savedSearchPlugin != "prowlarr" {
		t.Fatalf("post-search state = saved=%q effective=%q", m.savedSearchPlugin, m.searchPlugin)
	}

	service.plugins = []client.SearchPlugin{{Name: "prowlarr", Enabled: true}}
	cmd = m.launchSearch("Ubuntu", m.searchPlugin, time.Second)
	done = cmd().(workDone)
	if done.effectivePlugin != "prowlarr" {
		t.Fatalf("re-enabled effective provider = %q, want prowlarr", done.effectivePlugin)
	}
	if service.catalogCalls != 2 {
		t.Fatalf("revalidation catalog calls = %d, want 2", service.catalogCalls)
	}
	m.finishWork(done)
}

func TestSearchResultCannotOverwritePickerChoice(t *testing.T) {
	m := newModel(t.Context(), nil)
	m.searchPlugin = "prowlarr"
	m.savedSearchPlugin = "prowlarr"
	m.searchPluginDefaultActive = true
	selectionGeneration := m.searchPluginSelectionGeneration

	if err := m.setDefaultSearchPlugin("jackett"); err != nil {
		t.Fatal(err)
	}
	m.finishWork(workDone{
		kind:                      searchWork,
		effectivePlugin:           client.DefaultSearchPlugins,
		searchPluginWasDefault:    true,
		pluginSelectionGeneration: selectionGeneration,
	})
	if m.searchPlugin != "jackett" || m.savedSearchPlugin != "jackett" {
		t.Fatalf("stale search result overwrote picker choice: saved=%q effective=%q", m.savedSearchPlugin, m.searchPlugin)
	}
}
