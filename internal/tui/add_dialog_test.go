//go:build !plan9

package tui

import (
	"strings"
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
	d.fields[addContentLayoutField].input.SetValue("subfolder")
	d.fields[addDownloadPathField].input.SetValue("/incomplete")
	d.fields[addSkipCheckingField].input.SetValue("yes")
	d.fields[addStopConditionField].input.SetValue("metadata")
	d.fields[addSequentialField].input.SetValue("yes")
	d.fields[addFirstLastPieceField].input.SetValue("yes")
	d.fields[addRatioLimitField].input.SetValue("1.5")
	d.fields[addSeedingTimeField].input.SetValue("90m")
	d.fields[addInactiveSeedingTimeField].input.SetValue("-1")
	d.fields[addShareLimitsModeField].input.SetValue("match-any")
	d.fields[addShareLimitActionField].input.SetValue("stop")
	runTUIWork(t, m, pressTUIKey(m, tea.KeyEnter))

	got := backend.recordedActions()
	if len(got) != 1 || got[0].kind != "add-result" || got[0].id != 123456789012 {
		t.Fatalf("submitted actions = %#v", got)
	}
	options := got[0].addOptions
	if options.Category != "linux" || options.SavePath != "/downloads/linux" || options.Tags != "linux,iso" || !options.Stopped ||
		options.ContentLayout != "Subfolder" || options.DownloadPath != "/incomplete" || !options.SkipChecking || !options.SkipCheckingSet ||
		options.StopCondition != "MetadataReceived" || !options.SequentialDownload || !options.SequentialDownloadSet ||
		!options.FirstLastPiecePriority || !options.FirstLastPiecePrioritySet || options.ShareRatioLimit == nil || *options.ShareRatioLimit != 1.5 ||
		options.ShareSeedingTimeLimit == nil || *options.ShareSeedingTimeLimit != 90 || options.ShareInactiveSeedingTimeLimit == nil || *options.ShareInactiveSeedingTimeLimit != -1 ||
		options.ShareLimitsMode != "MatchAny" || options.ShareLimitAction != "Stop" {
		t.Fatalf("submitted add options = %#v", options)
	}
}

func TestAddDialogRequiresDestructiveShareActionConfirmation(t *testing.T) {
	d := &dialog{fields: torrentAddFields(newDialogField("Source", "magnet:?xt=urn:btih:test", "", ""))}
	d.fields[addShareLimitActionField].input.SetValue("remove-with-content")
	if _, err := parseAddDialogOptions(d); err == nil {
		t.Fatal("destructive share action accepted without confirmation")
	}
	d.fields[addDestructiveConfirmField].input.SetValue("yes")
	options, err := parseAddDialogOptions(d)
	if err != nil {
		t.Fatalf("confirmed destructive share action returned error: %v", err)
	}
	if options.ShareLimitAction != "RemoveWithContent" {
		t.Fatalf("share action = %q, want RemoveWithContent", options.ShareLimitAction)
	}
}

func TestAddDialogAdvancedSectionIsCollapsedAndPreservesInput(t *testing.T) {
	m := newModel(t.Context(), &recordingService{})
	m.openAddDialog()
	d := m.dialog
	if d.advanced {
		t.Fatal("add dialog advanced section is expanded by default")
	}
	if strings.Contains(m.dialogView(100, 40), "Advanced: Content layout") {
		t.Fatal("collapsed add dialog rendered advanced fields")
	}
	d.focusField(addTagsField)
	d.cycleField(false)
	if d.focus != addSourceField {
		t.Fatalf("collapsed add navigation entered hidden field: focus=%d", d.focus)
	}
	d.fields[addContentLayoutField].input.SetValue("subfolder")
	m.update(tea.KeyPressMsg{Code: tea.KeyF2})
	if !d.advanced || !strings.Contains(m.dialogView(100, 40), "Advanced: Content layout") {
		t.Fatal("F2 did not expand the advanced section")
	}
	d.focusField(addContentLayoutField)
	m.update(tea.KeyPressMsg{Code: tea.KeyF2})
	if d.advanced || d.focus >= addContentLayoutField || d.value(addContentLayoutField) != "subfolder" {
		t.Fatalf("collapsing advanced section lost state: advanced=%v focus=%d value=%q", d.advanced, d.focus, d.value(addContentLayoutField))
	}
}
