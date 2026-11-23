//go:build !plan9

package tui

import (
	"net"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

func (m *model) openTransferLimitDialog(upload bool) tea.Cmd {
	if m.busy != "" {
		m.setNotice("Finish the current request first", false)
		return nil
	}
	kind, title := "transfer-download", "Set global download limit"
	if upload {
		kind, title = "transfer-upload", "Set global upload limit"
	}
	m.dialog = &dialog{
		kind:    dialogTransferLimit,
		title:   title,
		request: actionRequest{kind: kind},
		fields: []dialogField{
			newDialogField("Limit", "", "bytes/s, Nk, or unlimited", "Sets the active global limit. Enter 0 for unlimited."),
		},
	}
	m.dialog.focusField(0)
	return nil
}

func (m *model) selectedPeerAddress(d *dialog) (string, error) {
	if d == nil || d.detail == nil {
		return "", client.ValidatePeerAddress("")
	}
	keys := make([]string, 0, len(d.detail.peers.Peers))
	for key := range d.detail.peers.Peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if d.detail.peerCursor < 0 || d.detail.peerCursor >= len(keys) {
		return "", client.ValidatePeerAddress("")
	}
	peer := d.detail.peers.Peers[keys[d.detail.peerCursor]]
	if net.ParseIP(strings.TrimSpace(peer.IP)) == nil || peer.Port < 1 {
		return "", client.ValidatePeerAddress("")
	}
	address := net.JoinHostPort(strings.TrimSpace(peer.IP), strconv.Itoa(peer.Port))
	if err := client.ValidatePeerAddress(address); err != nil {
		return "", err
	}
	return address, nil
}

func (m *model) openPeerBanDialog(d *dialog) tea.Cmd {
	if m.busy != "" {
		d.status = "Finish the current request first"
		return nil
	}
	address, err := m.selectedPeerAddress(d)
	if err != nil {
		d.status = "Only a peer with a valid IP address and port can be banned"
		return nil
	}
	m.dialog = &dialog{
		kind:          dialogConfirm,
		title:         "Ban peer",
		message:       "Ban peer " + viewSafe(client.SanitizeServerText(address)) + "?",
		request:       actionRequest{kind: "ban-peer", value: address},
		confirmAction: "ban-peer",
		confirmParent: d,
	}
	return nil
}
