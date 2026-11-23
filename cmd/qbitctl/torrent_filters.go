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
	if !qbt.IsTorrentStateFilter(state) {
		return qbt.TorrentListFilter{}, fmt.Errorf("unsupported list state %q", state)
	}
	switch qbt.NormalizeTorrentStateFilter(state) {
	case "":
		state = ""
	case "stalled-uploading":
		state = "stalled_uploading"
	case "stalled-downloading":
		state = "stalled_downloading"
	case "seeding":
		state = "seeding"
	case "stopped":
		state = "stopped"
	case "error":
		state = "errored"
	case "missingfiles":
		state = "missingFiles"
	case "forceddl":
		state = "forcedDL"
	case "forcedup":
		state = "forcedUP"
	case "metadl":
		state = "metaDL"
	case "forcedmetadl":
		state = "forcedMetaDL"
	case "checkingdl":
		state = "checkingDL"
	case "checkingup":
		state = "checkingUP"
	case "checkingresumedata":
		state = "checkingResumeData"
	case "queueddl":
		state = "queuedDL"
	case "queuedup":
		state = "queuedUP"
	case "stalleddl":
		state = "stalledDL"
	case "stalledup":
		state = "stalledUP"
	case "stoppedld":
		state = "stoppedDL"
	case "stoppedup":
		state = "stoppedUP"
	case "pausedld":
		state = "pausedDL"
	case "pausedup":
		state = "pausedUP"
	default:
		state = qbt.NormalizeTorrentStateFilter(state)
	}
	return qbt.TorrentListFilter{State: state, Category: category, Tag: tag}, nil
}
