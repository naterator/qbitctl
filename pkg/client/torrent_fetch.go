package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ResolveHash resolves one full hash or prefix and verifies it exists on the
// qBittorrent server. Full hashes therefore make one filtered API request.
func (a *App) ResolveHash(hash string) (string, error) {
	resolved, err := a.ResolveHashes([]string{hash})
	if err != nil {
		return "", err
	}
	return resolved[0], nil
}

// ResolveHashes resolves multiple short hashes to full hashes.
// It fetches the torrent list once and resolves all inputs against it. Full
// hashes are checked against qBittorrent as well as validated locally.
func (a *App) ResolveHashes(inputs []string) ([]string, error) {
	if len(inputs) == 0 {
		return nil, codedErrf(ExitBadArgs, "At least one hash required for this operation")
	}

	normalized := make([]string, 0, len(inputs))
	seenInputs := make(map[string]struct{}, len(inputs))
	allFull := true
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			return nil, codedErrf(ExitBadArgs, "Hash required for this operation")
		}
		if !isHexPrefix(input) {
			return nil, codedErrf(ExitBadArgs, "Invalid hash or prefix: '%s'", input)
		}
		input = strings.ToLower(input)
		if _, exists := seenInputs[input]; exists {
			continue
		}
		seenInputs[input] = struct{}{}
		normalized = append(normalized, input)
		if len(input) != 40 {
			allFull = false
		}
	}

	var torrents []TorrentInfo
	var err error
	if allFull && len(normalized) <= 50 {
		torrents, err = a.fetchTorrents(normalized)
	} else {
		torrents, err = a.fetchAllTorrentsForHashes()
	}
	if err != nil {
		return nil, err
	}

	available := make([]string, 0, len(torrents))
	for _, torrent := range torrents {
		if validateHash(strings.ToLower(torrent.Hash)) {
			available = append(available, strings.ToLower(torrent.Hash))
		}
	}

	resolved := make([]string, 0, len(normalized))
	seen := make(map[string]struct{}, len(normalized))
	for _, input := range normalized {
		match := ""
		matches := 0
		for _, hash := range available {
			if strings.HasPrefix(hash, input) {
				match = hash
				matches++
			}
		}

		switch {
		case matches == 0:
			if len(input) == 40 {
				return nil, codedErrf(ExitBadArgs, "No torrent matches hash: %s", input)
			}
			return nil, codedErrf(ExitBadArgs, "No torrent matches short hash: %s", input)
		case matches > 1:
			return nil, codedErrf(ExitBadArgs, "Short hash is ambiguous: %s", input)
		}
		if _, exists := seen[match]; !exists {
			seen[match] = struct{}{}
			resolved = append(resolved, match)
		}
	}

	return resolved, nil
}

func isHexPrefix(value string) bool {
	if len(value) == 0 || len(value) > 40 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') && !(char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}

func (a *App) fetchAllTorrentsForHashes() ([]TorrentInfo, error) {
	return a.fetchTorrents(nil)
}

func (a *App) fetchTorrents(hashes []string) ([]TorrentInfo, error) {
	var params url.Values
	if len(hashes) > 0 {
		params = url.Values{"hashes": {strings.Join(hashes, "|")}}
	}
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/torrents/info", params)
	if err != nil {
		return nil, codedErrWrapf(ExitFetchFail, err, "Failed to fetch torrent list: %v", err)
	}

	var torrents []TorrentInfo
	if err := json.Unmarshal(body, &torrents); err != nil {
		return nil, codedErrf(ExitFetchFail, "Invalid JSON from torrent list")
	}
	return torrents, nil
}

func (a *App) fetchSingleTorrentBody(hash string) ([]byte, error) {
	params := url.Values{"hashes": {hash}}
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/torrents/info", params)
	if err != nil {
		return nil, codedErrWrapf(ExitFetchFail, err, "Failed to fetch torrent info: %v", err)
	}
	return body, nil
}

// GetTorrentInfo fetches one torrent by full hash. Selectors containing '|'
// or the special "all" value are rejected because this method returns one
// row; callers resolving a batch should use ResolveHashes instead.
func (a *App) GetTorrentInfo(hash string) (TorrentInfo, error) {
	if strings.Contains(hash, "|") || strings.EqualFold(strings.TrimSpace(hash), "all") {
		return TorrentInfo{}, codedErrf(ExitBadArgs, "GetTorrentInfo accepts a single hash")
	}
	body, err := a.fetchSingleTorrentBody(hash)
	if err != nil {
		return TorrentInfo{}, err
	}

	var torrents []TorrentInfo
	if err := json.Unmarshal(body, &torrents); err != nil {
		return TorrentInfo{}, codedErrf(ExitFetchFail, "Failed to parse torrent info response")
	}
	if len(torrents) == 0 {
		return TorrentInfo{}, codedErrf(ExitFetchFail, "No torrent matched the requested hash")
	}

	return torrents[0], nil
}

func (a *App) FetchAllTorrents() ([]TorrentInfo, []byte, error) {
	return a.FetchAllTorrentsContext(a.context())
}

func (a *App) FetchAllTorrentsContext(ctx context.Context) ([]TorrentInfo, []byte, error) {
	body, err := a.client.requestContext(ctx, http.MethodGet, "/api/v2/torrents/info", nil)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, fmt.Errorf("failed to fetch torrent list: %w", err)
		}
		return nil, nil, codedErrWrapf(ExitFetchFail, err, "Failed to fetch torrent list: %v", err)
	}

	var torrents []TorrentInfo
	if err := json.Unmarshal(body, &torrents); err != nil {
		return nil, nil, codedErrf(ExitFetchFail, "Invalid JSON from torrent list")
	}

	return torrents, body, nil
}
