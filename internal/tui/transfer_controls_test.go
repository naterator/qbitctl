//go:build !plan9

package tui

import (
	"context"
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type peerBanTestService struct {
	recordingService
	peerResult client.TorrentPeers
}

func (s *peerBanTestService) peers(context.Context, string, int64) (client.TorrentPeers, error) {
	return s.peerResult, nil
}

func TestServerTransferLimitDialogSubmitsGlobalAction(t *testing.T) {
	backend := &recordingService{}
	m := newModel(t.Context(), backend)
	m.page = serverPage
	if cmd := m.openAction("server-download-limit"); cmd != nil {
		t.Fatal("opening transfer limit dialog returned a command")
	}
	if m.dialog == nil || m.dialog.kind != dialogTransferLimit {
		t.Fatalf("dialog = %#v, want transfer limit dialog", m.dialog)
	}
	m.dialog.fields[0].input.SetValue("2k")
	cmd := m.submitDialog()
	if cmd == nil {
		t.Fatal("submitting transfer limit returned no command")
	}
	runTUIWork(t, m, cmd)
	actions := backend.recordedActions()
	if len(actions) != 1 || actions[0].kind != "transfer-download" || actions[0].value != "2k" {
		t.Fatalf("transfer actions = %#v", actions)
	}
}

func TestServerTransferControlsCoverUploadModeAndBusyPaths(t *testing.T) {
	service := &recordingService{}
	m := newModel(t.Context(), service)
	m.page = serverPage

	if cmd := m.openAction("server-upload-limit"); cmd != nil {
		t.Fatal("opening upload limit dialog returned a command")
	}
	if m.dialog == nil || m.dialog.kind != dialogTransferLimit || m.dialog.request.kind != "transfer-upload" {
		t.Fatalf("upload dialog = %#v, want transfer-upload dialog", m.dialog)
	}
	m.dialog.fields[0].input.SetValue("")
	if cmd := m.submitDialog(); cmd != nil {
		t.Fatal("blank transfer limit returned a command")
	}
	if m.dialog.status == "" {
		t.Fatal("blank transfer limit did not report a status")
	}

	m.dialog = nil
	cmd := m.openAction("server-mode-alternate")
	if cmd == nil {
		t.Fatal("opening alternate mode did not return an action command")
	}
	runTUIWork(t, m, cmd)
	if actions := service.recordedActions(); len(actions) != 1 || actions[0].kind != "transfer-mode" || actions[0].value != "alternate" {
		t.Fatalf("alternate mode actions = %#v", actions)
	}

	m.dialog = nil
	m.busy = "Saving transfer setting"
	if cmd := m.openTransferLimitDialog(false); cmd != nil {
		t.Fatal("busy transfer dialog returned a command")
	}
	if m.dialog != nil || m.notice != "Finish the current request first" {
		t.Fatalf("busy transfer dialog state = dialog=%#v notice=%q", m.dialog, m.notice)
	}
}

func TestServerEnterDoesNotOpenRedundantDetailsDialog(t *testing.T) {
	m := newModel(t.Context(), &recordingService{})
	m.page = serverPage
	m.serverRows = []detailRow{{label: "Connection", value: "connected"}}
	if cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("Server Enter returned a command")
	}
	if m.dialog != nil {
		t.Fatalf("Server Enter opened dialog %#v", m.dialog)
	}
}

func TestServerRefreshShowsLiveModeWithStaleQueueState(t *testing.T) {
	backend, server := newBackendFixture(t)
	checkMode := func(want string) {
		t.Helper()
		rows, err := backend.server(t.Context())
		if err != nil {
			t.Fatalf("server refresh: %v", err)
		}
		for _, row := range rows {
			if row.label == "Speed limits mode" && row.value == want {
				return
			}
		}
		t.Fatalf("server rows = %#v, want %s speed limits mode", rows, want)
	}
	server.SpeedLimitsMode = "1"
	checkMode("alternate")
	backend.queueState.serverState = map[string]json.RawMessage{"use_alt_speed_limits": json.RawMessage("true")}
	server.SpeedLimitsMode = "0"
	checkMode("normal")
}

func TestServerRefreshShowsUnavailableModeWhenEndpointFails(t *testing.T) {
	backend, server := newBackendFixture(t)
	server.StatusQueue["/api/v2/transfer/speedLimitsMode"] = []int{500}
	rows, err := backend.server(context.Background())
	if err != nil {
		t.Fatalf("server refresh: %v", err)
	}
	for _, row := range rows {
		if row.label == "Speed limits mode" {
			if row.value != "unavailable" {
				t.Fatalf("mode row = %q, want unavailable", row.value)
			}
			return
		}
	}
	t.Fatal("server refresh omitted speed limits mode row")
}

func TestTUIClientRoutesTransferControls(t *testing.T) {
	backend, server := newBackendFixture(t)
	server.SpeedLimitsMode = "0"
	if err := backend.action(context.Background(), actionRequest{kind: "transfer-download", value: "2k"}); err != nil {
		t.Fatalf("download action: %v", err)
	}
	if got := server.Forms["/api/v2/transfer/setDownloadLimit"][0].Get("limit"); got != "2048" {
		t.Fatalf("download form = %q, want 2048", got)
	}
	if err := backend.action(context.Background(), actionRequest{kind: "transfer-upload", value: "unlimited"}); err != nil {
		t.Fatalf("upload action: %v", err)
	}
	if got := server.Forms["/api/v2/transfer/setUploadLimit"][0].Get("limit"); got != "0" {
		t.Fatalf("upload form = %q, want 0", got)
	}
	if err := backend.action(context.Background(), actionRequest{kind: "transfer-mode", value: "alternate"}); err != nil {
		t.Fatalf("mode action: %v", err)
	}
	if server.SpeedLimitsMode != "1" || countTransferPath(server.CallOrder, "/api/v2/transfer/toggleSpeedLimitsMode") != 1 {
		t.Fatalf("mode state=%q calls=%#v", server.SpeedLimitsMode, server.CallOrder)
	}
	if err := backend.action(context.Background(), actionRequest{kind: "ban-peer", value: "[2001:db8::7]:51413"}); err != nil {
		t.Fatalf("ban action: %v", err)
	}
	if got := server.Forms["/api/v2/transfer/banPeers"][0].Get("peers"); got != "[2001:db8::7]:51413" {
		t.Fatalf("ban form = %q", got)
	}
}

func countTransferPath(paths []string, want string) int {
	count := 0
	for _, path := range paths {
		if path == want {
			count++
		}
	}
	return count
}

func TestPeerBanRequiresConfirmationAndUsesBracketedIPv6(t *testing.T) {
	backend := &peerBanTestService{peerResult: client.TorrentPeers{
		Peers: map[string]client.TorrentPeer{
			"peer": {IP: "2001:db8::7", Port: 51413},
		},
	}}
	backend.torrents = []client.TorrentInfo{{Hash: "torrent", Name: "Test torrent"}}
	m := newModel(t.Context(), backend)
	m.page = queuePage
	m.dialog = &dialog{
		kind:           dialogDetails,
		torrentDetails: &torrentDetailTarget{hash: "torrent"},
		detail:         &detailDialogState{tab: detailTabPeers, peers: backend.peerResult, fileMarks: map[int]bool{}},
	}
	if cmd := m.update(tea.KeyPressMsg{Code: 'b'}); cmd != nil {
		t.Fatal("opening peer ban confirmation returned a command")
	}
	if m.dialog == nil || m.dialog.kind != dialogConfirm || m.dialog.confirmAction != "ban-peer" {
		t.Fatalf("dialog = %#v, want peer confirmation", m.dialog)
	}
	if m.dialog.request.value != "[2001:db8::7]:51413" {
		t.Fatalf("peer address = %q, want bracketed IPv6", m.dialog.request.value)
	}
	runTUIWork(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	actions := backend.recordedActions()
	if len(actions) != 1 || actions[0].kind != "ban-peer" || actions[0].value != "[2001:db8::7]:51413" {
		t.Fatalf("ban actions = %#v", actions)
	}
	if m.dialog == nil || m.dialog.kind != dialogDetails {
		t.Fatalf("details dialog after ban = %#v, want restored details", m.dialog)
	}
}

func TestPeerBanRejectsNonIPPeer(t *testing.T) {
	backend := &peerBanTestService{peerResult: client.TorrentPeers{
		Peers: map[string]client.TorrentPeer{
			"i2p": {I2PDestination: "dest", Port: 51413},
		},
	}}
	backend.torrents = []client.TorrentInfo{{Hash: "torrent", Name: "Test torrent"}}
	m := newModel(t.Context(), backend)
	m.page = queuePage
	m.dialog = &dialog{
		kind:           dialogDetails,
		torrentDetails: &torrentDetailTarget{hash: "torrent"},
		detail:         &detailDialogState{tab: detailTabPeers, peers: backend.peerResult, fileMarks: map[int]bool{}},
	}
	m.update(tea.KeyPressMsg{Code: 'b'})
	if m.dialog == nil || m.dialog.kind != dialogDetails || m.dialog.status == "" {
		t.Fatalf("invalid peer dialog = %#v, want detail status", m.dialog)
	}
	if actions := backend.recordedActions(); len(actions) != 0 {
		t.Fatalf("invalid peer triggered actions = %#v", actions)
	}
}

func TestPeerBanSelectionRejectsMissingOrInvalidPeerAddress(t *testing.T) {
	m := newModel(t.Context(), &recordingService{})
	for _, detail := range []*dialog{
		nil,
		{},
		{detail: &detailDialogState{tab: detailTabPeers, peers: client.TorrentPeers{Peers: map[string]client.TorrentPeer{
			"peer": {IP: "192.0.2.7", Port: 0},
		}}}},
	} {
		if address, err := m.selectedPeerAddress(detail); err == nil || address != "" {
			t.Fatalf("selected peer address = %q, err=%v; want rejection for %#v", address, err, detail)
		}
	}

	m.busy = "Existing request"
	detail := &dialog{kind: dialogDetails}
	if cmd := m.openPeerBanDialog(detail); cmd != nil {
		t.Fatal("busy peer ban returned a command")
	}
	if detail.status != "Finish the current request first" {
		t.Fatalf("busy peer ban status = %q", detail.status)
	}
}
