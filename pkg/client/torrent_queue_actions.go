package client

import (
	"net/http"
	"net/url"
	"strings"
)

// RecheckTorrent asks qBittorrent to verify the selected torrent data. The
// hashes argument may contain the pipe-separated selector accepted by the Web
// API, so callers can submit a resolved batch in one request.
func (a *App) RecheckTorrent(hashes string) error {
	return a.qbtAction(hashes, "/api/v2/torrents/recheck", nil)
}

// ReannounceTorrent asks the selected torrents to announce to their trackers.
func (a *App) ReannounceTorrent(hashes string) error {
	return a.qbtAction(hashes, "/api/v2/torrents/reannounce", nil)
}

// QueueUpTorrent moves the selected torrents one position toward the front of
// qBittorrent's queue.
func (a *App) QueueUpTorrent(hashes string) error {
	return a.qbtAction(hashes, "/api/v2/torrents/increasePrio", nil)
}

// QueueDownTorrent moves the selected torrents one position toward the back of
// qBittorrent's queue.
func (a *App) QueueDownTorrent(hashes string) error {
	return a.qbtAction(hashes, "/api/v2/torrents/decreasePrio", nil)
}

// QueueTopTorrent moves the selected torrents to the front of the queue.
func (a *App) QueueTopTorrent(hashes string) error {
	return a.qbtAction(hashes, "/api/v2/torrents/topPrio", nil)
}

// QueueBottomTorrent moves the selected torrents to the back of the queue.
func (a *App) QueueBottomTorrent(hashes string) error {
	return a.qbtAction(hashes, "/api/v2/torrents/bottomPrio", nil)
}

// RenameTorrent changes the display name of one torrent. qBittorrent's rename
// endpoint intentionally takes a singular hash, unlike the bulk action
// endpoints above.
func (a *App) RenameTorrent(hash, name string) error {
	hash = strings.TrimSpace(hash)
	name = strings.TrimSpace(name)
	if hash == "" {
		return codedErrf(ExitBadArgs, "Torrent hash required for rename")
	}
	if name == "" {
		return codedErrf(ExitBadArgs, "Torrent name required for rename")
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/rename", url.Values{
		"hash": {hash},
		"name": {name},
	}); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to rename torrent: %v", err)
	}
	return nil
}
