package client

import "strings"

// NormalizeTorrentStateFilter converts the stable qbitctl state vocabulary to
// one canonical selector. The qBittorrent Web API has changed a few spellings
// over time, so callers should use this before comparing a row locally.
func NormalizeTorrentStateFilter(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all", "any":
		return ""
	case "errored", "error":
		return "error"
	case "paused", "stopped":
		return "stopped"
	case "uploading", "seeding":
		return "seeding"
	case "stalled-uploading", "stalled_uploading":
		return "stalled-uploading"
	case "stalled-downloading", "stalled_downloading":
		return "stalled-downloading"
	case "queued":
		return "queued"
	case "checking":
		return "checking"
	case "active", "downloading", "completed", "inactive", "running", "stalled",
		"moving", "missingfiles", "missing-files", "forceddl", "forcedup", "metadl",
		"forcedmetadl", "checkingdl", "checkingup", "checkingresumedata", "allocating",
		"unknown", "queueddl", "queuedup", "stalleddl", "stalledup", "stoppedld",
		"stoppedup", "pausedld", "pausedup":
		if strings.EqualFold(value, "missing-files") {
			return "missingfiles"
		}
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

// IsTorrentStateFilter reports whether value is one of qbitctl's portable
// state selectors. Empty and all are valid selectors too.
func IsTorrentStateFilter(value string) bool {
	raw := strings.ToLower(strings.TrimSpace(value))
	if raw == "" || raw == "all" || raw == "any" {
		return true
	}
	return NormalizeTorrentStateFilter(raw) != ""
}

// MatchTorrentState applies qbitctl's stable state vocabulary to one torrent.
// It intentionally uses the same predicates for the TUI and CLI, while the
// native qBittorrent filter remains on the request to reduce server work.
func MatchTorrentState(torrent TorrentInfo, filter string) bool {
	selector := NormalizeTorrentStateFilter(filter)
	if !IsTorrentStateFilter(filter) {
		return false
	}
	state := strings.ToLower(strings.TrimSpace(torrent.State))
	stopped := strings.HasPrefix(state, "stopped") || strings.HasPrefix(state, "paused")
	checking := strings.Contains(state, "check")
	queued := strings.Contains(state, "queued")
	active := torrent.DLSpeed > 0 || torrent.UpSpeed > 0
	seeding := strings.HasSuffix(state, "up") || state == "uploading" || state == "seeding"
	downloading := strings.HasSuffix(state, "dl") || state == "downloading"
	completed := torrent.Progress >= 1 || seeding || (torrent.CompletionOn != nil && *torrent.CompletionOn > 0)

	switch selector {
	case "":
		return true
	case "active":
		return active
	case "downloading":
		return !stopped && !checking && !queued && downloading
	case "seeding":
		return !stopped && !checking && !queued && seeding
	case "completed":
		return completed
	case "inactive":
		return !active
	case "running":
		return !stopped && !checking && !queued && state != "error" && state != "missingfiles" && state != "unknown"
	case "stalled":
		return strings.HasPrefix(state, "stalled") || state == "stalled"
	case "stalled-uploading":
		return strings.HasPrefix(state, "stalledup") || state == "stalled-uploading"
	case "stalled-downloading":
		return strings.HasPrefix(state, "stalleddl") || state == "stalled-downloading"
	case "stopped":
		return stopped
	case "error":
		return state == "error" || state == "missingfiles" || state == "unknown"
	case "queued":
		return queued
	case "checking":
		return checking
	default:
		// Keep qBittorrent-specific state words accepted by earlier TUI
		// versions useful in both clients. Portable selectors above retain
		// aggregate semantics; exact server states use a substring match so
		// checkingResumeData and forcedMetaDL remain easy to filter.
		return strings.Contains(state, selector)
	}
}
