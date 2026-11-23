package client

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// TrackerStatusLabel translates the status values returned by qBittorrent's
// torrents/trackers endpoint into labels suitable for a terminal table.
func TrackerStatusLabel(status int) string {
	switch status {
	case 0:
		return "Disabled"
	case 1:
		return "Not contacted"
	case 2:
		return "Working"
	case 3:
		return "Updating"
	case 4:
		return "Not working"
	default:
		return "Unknown"
	}
}

// TrackerHost returns the non-sensitive host portion of a tracker URL. It
// deliberately omits userinfo, query strings, and paths so announce URLs that
// contain passkeys are not disclosed by the default tracker table.
func TrackerHost(rawURL string) string {
	value := strings.TrimSpace(rawURL)
	switch value {
	case "** [DHT] **":
		return "DHT"
	case "** [PeX] **":
		return "PeX"
	case "** [LSD] **":
		return "LSD"
	case "":
		return "Unknown"
	}
	parsed, err := url.Parse(value)
	if err == nil {
		if host := parsed.Hostname(); host != "" {
			return host
		}
	}
	return "Unknown"
}

// AddTrackers adds one or more newline-separated tracker URLs to a torrent.
// qBittorrent's API uses newlines for this endpoint, matching the WebUI's
// tracker textarea.
func (a *App) AddTrackers(hash, urls string) error {
	if strings.TrimSpace(hash) == "" {
		return codedErrf(ExitBadArgs, "Torrent hash is required")
	}
	validated, err := validateTrackerList(urls, "\n")
	if err != nil {
		return err
	}
	params := url.Values{"hash": {strings.TrimSpace(hash)}, "urls": {strings.Join(validated, "\n")}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/addTrackers", params); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to add trackers: %v", err)
	}
	return nil
}

// EditTracker replaces exactly one tracker URL. The original URL is sent
// verbatim after surrounding whitespace is removed; no hostname or URL
// normalization is performed because qBittorrent matches it exactly.
func (a *App) EditTracker(hash, originalURL, newURL string) error {
	if strings.TrimSpace(hash) == "" {
		return codedErrf(ExitBadArgs, "Torrent hash is required")
	}
	original, err := validateTrackerURL(originalURL)
	if err != nil {
		return fmt.Errorf("invalid original tracker URL: %w", err)
	}
	replacement, err := validateTrackerURL(newURL)
	if err != nil {
		return fmt.Errorf("invalid replacement tracker URL: %w", err)
	}
	params := url.Values{
		"hash": {strings.TrimSpace(hash)},
		// qBittorrent 5.0 and older names the original URL origUrl;
		// newer releases use url. Sending both keeps the exact-match
		// operation compatible across the API transition. Both values are
		// intentionally identical so neither version can see a different
		// tracker than the one the caller selected.
		"origUrl": {original},
		"url":     {original},
		"newUrl":  {replacement},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/editTracker", params); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to edit tracker: %v", err)
	}
	return nil
}

// RemoveTrackers removes one or more exact tracker URLs. qBittorrent uses a
// pipe-separated list for this endpoint, so URLs containing a pipe are
// rejected instead of being sent ambiguously.
func (a *App) RemoveTrackers(hash, urls string) error {
	if strings.TrimSpace(hash) == "" {
		return codedErrf(ExitBadArgs, "Torrent hash is required")
	}
	validated, err := validateTrackerList(urls, "|")
	if err != nil {
		return err
	}
	params := url.Values{"hash": {strings.TrimSpace(hash)}, "urls": {strings.Join(validated, "|")}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/removeTrackers", params); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to remove trackers: %v", err)
	}
	return nil
}

func validateTrackerList(raw, separator string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, codedErrf(ExitBadArgs, "At least one tracker URL is required")
	}
	parts := strings.Split(raw, separator)
	validated := make([]string, 0, len(parts))
	for _, part := range parts {
		value, err := validateTrackerURL(part)
		if err != nil {
			return nil, err
		}
		if separator == "|" && strings.Contains(value, "|") {
			return nil, codedErrf(ExitBadArgs, "Tracker URLs cannot contain '|'")
		}
		validated = append(validated, value)
	}
	return validated, nil
}

func validateTrackerURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", codedErrf(ExitBadArgs, "Tracker URL cannot be empty")
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", codedErrf(ExitBadArgs, "Tracker URL cannot contain a newline")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", codedErrf(ExitBadArgs, "Invalid tracker URL %q", value)
	}
	return value, nil
}
