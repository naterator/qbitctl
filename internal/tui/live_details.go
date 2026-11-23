//go:build !plan9

package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type torrentDetailTarget struct {
	hash string
}

type torrentDetailUpdate struct {
	dialog         *dialog
	rows           []detailRow
	trackerEntries []client.TrackerEntry
	files          []client.TorrentFile
	peers          client.TorrentPeers
	tab            detailTab
	err            error
}

func fetchQueueUpdate(ctx context.Context, backend service) workDone {
	torrents, err := backend.queue(ctx)
	// Compatibility backends do not expose change metadata, so retain the
	// historical behavior for them. The incremental-sync adapter can report a
	// true no-op and let the model skip filtering and sorting entirely.
	update := workDone{queue: torrents, queueChanged: true, err: err}
	if source, ok := backend.(interface{ takeQueueNotice() string }); ok && err == nil {
		update.notice = source.takeQueueNotice()
	}
	if source, ok := backend.(interface{ takeQueueChanged() bool }); ok && err == nil {
		update.queueChanged = source.takeQueueChanged()
	}
	if source, ok := backend.(interface{ serverTransferState() queueTransferState }); ok && err == nil {
		update.queueTransfer = source.serverTransferState()
	}
	return update
}

func (m *model) refreshTorrentDetails() tea.Cmd {
	d := m.dialog
	if m.page != queuePage || d == nil || d.torrentDetails == nil || m.backend == nil {
		return nil
	}
	backend, target := m.backend, *d.torrentDetails
	refreshTab := detailTabOverview
	peerRID := int64(0)
	if d.detail != nil {
		refreshTab = d.detail.tab
		peerRID = d.detail.peers.RID
	}
	return m.startWork(detailRefreshWork, "Refreshing queue and torrent details", func(ctx context.Context) workDone {
		update := fetchQueueUpdate(ctx, backend)
		detail := &torrentDetailUpdate{dialog: d, tab: refreshTab}
		switch refreshTab {
		case detailTabOverview:
			detail.rows, detail.err = backend.details(ctx, target.hash, false)
		case detailTabTrackers:
			if trackerBackend, ok := backend.(trackerService); ok {
				detail.trackerEntries, detail.err = trackerBackend.trackers(ctx, target.hash)
			} else {
				detail.err = fmt.Errorf("tracker details are unavailable for this connection")
			}
		case detailTabFiles:
			if source, ok := backend.(torrentFilesService); ok {
				detail.files, detail.err = source.files(ctx, target.hash)
			} else {
				detail.err = fmt.Errorf("file details are unavailable for this connection")
			}
		case detailTabPeers:
			if source, ok := backend.(torrentPeersService); ok {
				detail.peers, detail.err = source.peers(ctx, target.hash, peerRID)
			} else {
				detail.err = fmt.Errorf("peer details are unavailable for this connection")
			}
		}
		update.detailUpdate = detail
		return update
	})
}

func (m *model) finishTorrentDetailsRefresh(done workDone) {
	if done.err != nil {
		m.setNotice("Queue refresh: "+done.err.Error(), true)
	} else {
		m.queueTransfer = done.queueTransfer
		m.updateQueueActivity(done.queueTransfer)
		if done.queueChanged || m.allTorrents == nil {
			m.replaceQueue(done.queue)
		}
		if done.notice != "" {
			m.setNotice(done.notice, false)
		}
	}

	// Closing the dialog never discards a useful queue update. Only the
	// original dialog may receive its detail response.
	update := done.detailUpdate
	if update == nil || m.dialog != update.dialog {
		return
	}
	d := m.dialog
	editingTracker := d.kind == dialogTrackerAdd || d.kind == dialogTrackerEdit
	if update.err != nil {
		if !editingTracker {
			d.status = "Details refresh: " + update.err.Error()
		}
		return
	}
	found := false
	for _, torrent := range done.queue {
		if torrent.Hash == d.torrentDetails.hash {
			found = true
			if editingTracker {
				d.trackerTitle = torrent.Name
			} else {
				d.title = torrent.Name
			}
			break
		}
	}
	if done.err == nil && !found {
		// The focused torrent disappeared during the sync. Do not leave an
		// apparently live dialog showing rows for a torrent that no longer
		// exists on the server.
		m.dialog = nil
		return
	}
	switch update.tab {
	case detailTabFiles:
		d.detail.files = append([]client.TorrentFile(nil), update.files...)
	case detailTabPeers:
		d.detail.peers.Merge(update.peers)
	case detailTabTrackers:
		m.replaceTrackerEntries(update.trackerEntries)
	case detailTabOverview:
		d.rows = update.rows
	}
	if !editingTracker {
		d.status = ""
	}
}
