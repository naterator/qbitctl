package main

import (
	"fmt"
	"strings"

	qbt "github.com/naterator/qbitctl/pkg/client"
)

// listTorrentFilter converts qbitctl's stable state vocabulary to the names
// accepted by qBittorrent. queued and checking remain portable local filters;
// the client deliberately does not send those selectors to older Web APIs.
func listTorrentFilter(state, category, tag string) (qbt.TorrentListFilter, error) {
	state = strings.ToLower(strings.TrimSpace(state))
	switch state {
	case "", "all", "any":
		state = ""
	case "active", "downloading", "completed", "inactive", "running", "stalled":
	case "stalled-uploading", "stalled_uploading":
		state = "stalled_uploading"
	case "stalled-downloading", "stalled_downloading":
		state = "stalled_downloading"
	case "seeding", "uploading":
		state = "seeding"
	case "stopped", "paused":
		state = "stopped"
	case "error", "errored":
		state = "errored"
	case "queued", "checking":
		// qBittorrent has no corresponding info filter. The client applies
		// these two states after retrieving the queue.
	default:
		return qbt.TorrentListFilter{}, fmt.Errorf("unsupported list state %q", state)
	}
	return qbt.TorrentListFilter{State: state, Category: category, Tag: tag}, nil
}
