//go:build !plan9

package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

func TestAddSearchResultDialogSubmitsAddOptions(t *testing.T) {
	backend := &recordingService{}
	m := newModel(t.Context(), backend)
	m.page = searchPage
	m.replaceSearchResults([]client.SearchResult{{ID: 123456789012, Name: "Ubuntu ISO"}})

	pressTUIKey(m, tea.KeyEnter)
	d := m.dialog
	if d == nil || d.kind != dialogAddResult || d.value(0) != "123456789012" {
		t.Fatalf("add dialog = %#v, want the selected result filled in", d)
	}
	d.fields[1].input.SetValue("linux")
	d.fields[2].input.SetValue("/downloads/linux")
	d.fields[3].input.SetValue("yes")
	d.fields[4].input.SetValue("linux,iso")
	runTUIWork(t, m, pressTUIKey(m, tea.KeyEnter))

	got := backend.recordedActions()
	if len(got) != 1 || got[0].kind != "add-result" || got[0].id != 123456789012 {
		t.Fatalf("submitted actions = %#v", got)
	}
	options := got[0].addOptions
	if options.Category != "linux" || options.SavePath != "/downloads/linux" || options.Tags != "linux,iso" || !options.Stopped {
		t.Fatalf("submitted add options = %#v", options)
	}
}
