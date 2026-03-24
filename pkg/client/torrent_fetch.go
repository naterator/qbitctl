package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func (a *App) ResolveHash(shortHash string) (string, error) {
	if shortHash == "" {
		return "", codedErrf(ExitBadArgs, "Hash required for this operation")
	}

	if len(shortHash) == 40 {
		if !validateHash(strings.ToLower(shortHash)) {
			return "", codedErrf(ExitBadArgs, "Resolved hash is invalid: '%s'", shortHash)
		}
		return strings.ToLower(shortHash), nil
	}

	hashes, err := a.fetchAllHashes()
	if err != nil {
		return "", err
	}

	input := strings.ToLower(shortHash)
	match := ""
	matches := 0
	for _, hash := range hashes {
		if strings.HasPrefix(strings.ToLower(hash), input) {
			match = hash
			matches++
		}
	}

	switch {
	case matches == 0:
		return "", codedErrf(ExitBadArgs, "No torrent matches short hash: %s", shortHash)
	case matches > 1:
		return "", codedErrf(ExitBadArgs, "Short hash is ambiguous: %s", shortHash)
	case !validateHash(strings.ToLower(match)):
		return "", codedErrf(ExitBadArgs, "Resolved hash is invalid: '%s'", match)
	default:
		return strings.ToLower(match), nil
	}
}

// ResolveHashes resolves multiple short hashes to full hashes.
// It fetches the torrent list once and resolves all inputs against it.
func (a *App) ResolveHashes(inputs []string) ([]string, error) {
	if len(inputs) == 0 {
		return nil, codedErrf(ExitBadArgs, "At least one hash required for this operation")
	}

	// Check if all inputs are already full hashes
	allFull := true
	for _, input := range inputs {
		if len(input) != 40 {
			allFull = false
			break
		}
	}
	if allFull {
		resolved := make([]string, len(inputs))
		for i, input := range inputs {
			lower := strings.ToLower(input)
			if !validateHash(lower) {
				return nil, codedErrf(ExitBadArgs, "Resolved hash is invalid: '%s'", input)
			}
			resolved[i] = lower
		}
		return resolved, nil
	}

	hashes, err := a.fetchAllHashes()
	if err != nil {
		return nil, err
	}

	resolved := make([]string, 0, len(inputs))
	for _, input := range inputs {
		lower := strings.ToLower(input)

		if len(input) == 40 {
			if !validateHash(lower) {
				return nil, codedErrf(ExitBadArgs, "Resolved hash is invalid: '%s'", input)
			}
			resolved = append(resolved, lower)
			continue
		}

		match := ""
		matches := 0
		for _, hash := range hashes {
			if strings.HasPrefix(strings.ToLower(hash), lower) {
				match = hash
				matches++
			}
		}

		switch {
		case matches == 0:
			return nil, codedErrf(ExitBadArgs, "No torrent matches short hash: %s", input)
		case matches > 1:
			return nil, codedErrf(ExitBadArgs, "Short hash is ambiguous: %s", input)
		case !validateHash(strings.ToLower(match)):
			return nil, codedErrf(ExitBadArgs, "Resolved hash is invalid: '%s'", match)
		default:
			resolved = append(resolved, strings.ToLower(match))
		}
	}

	return resolved, nil
}

func (a *App) fetchAllHashes() ([]string, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/torrents/info", nil)
	if err != nil {
		return nil, codedErrf(ExitFetchFail, "Failed to fetch torrent list: %v", err)
	}

	var torrents []TorrentInfo
	if err := json.Unmarshal(body, &torrents); err != nil {
		return nil, codedErrf(ExitFetchFail, "Invalid JSON from torrent list")
	}

	hashes := make([]string, 0, len(torrents))
	for _, torrent := range torrents {
		if torrent.Hash != "" {
			hashes = append(hashes, torrent.Hash)
		}
	}

	return hashes, nil
}

func (a *App) fetchSingleTorrentBody(hash string) ([]byte, error) {
	params := url.Values{"hashes": {hash}}
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/torrents/info", params)
	if err != nil {
		return nil, codedErrf(ExitFetchFail, "Failed to fetch torrent info: %v", err)
	}
	return body, nil
}

func (a *App) GetTorrentInfo(hash string) (TorrentInfo, error) {
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
			return nil, nil, nil
		}
		return nil, nil, codedErrf(ExitFetchFail, "Failed to fetch torrent list: %v", err)
	}

	var torrents []TorrentInfo
	if err := json.Unmarshal(body, &torrents); err != nil {
		return nil, nil, codedErrf(ExitFetchFail, "Invalid JSON from torrent list")
	}

	return torrents, body, nil
}
