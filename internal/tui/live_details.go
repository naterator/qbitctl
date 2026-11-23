//go:build !plan9

package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

type torrentDetailTarget struct {
	hash     string
	trackers bool
}

func (target torrentDetailTarget) title(name string) string {
	if target.trackers {
		return "Trackers · " + name
	}
	return name
}

type torrentDetailUpdate struct {
	dialog *dialog
	rows   []detailRow
	err    error
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
	return m.startWork(detailRefreshWork, "Refreshing queue and torrent details", func(ctx context.Context) workDone {
		update := fetchQueueUpdate(ctx, backend)
		rows, err := backend.details(ctx, target.hash, target.trackers)
		update.detailUpdate = &torrentDetailUpdate{dialog: d, rows: rows, err: err}
		return update
	})
}

func (m *model) finishTorrentDetailsRefresh(done workDone) {
	if done.err != nil {
		m.setNotice("Queue refresh: "+done.err.Error(), true)
	} else {
		m.queueTransfer = done.queueTransfer
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
	if update.err != nil {
		d.status = "Details refresh: " + update.err.Error()
		return
	}
	found := false
	for _, torrent := range done.queue {
		if torrent.Hash == d.torrentDetails.hash {
			found = true
			d.title = d.torrentDetails.title(torrent.Name)
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
	d.rows, d.status = update.rows, ""
}
