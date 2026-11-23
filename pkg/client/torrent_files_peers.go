package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// FilePriority is the qBittorrent wire value used by torrents/filePrio.
// qBittorrent intentionally leaves gaps between Normal, High, and Maximum.
type FilePriority int

const (
	FilePrioritySkip    FilePriority = 0
	FilePriorityNormal  FilePriority = 1
	FilePriorityHigh    FilePriority = 6
	FilePriorityMaximum FilePriority = 7
)

// TorrentFile is one entry returned by torrents/files. Index is the stable
// file identifier to send back to torrents/filePrio, even when a filtered
// files request changes the entry's position in the response.
type TorrentFile struct {
	Index        int     `json:"index"`
	Name         string  `json:"name"`
	Size         int64   `json:"size"`
	Progress     float64 `json:"progress"`
	Priority     int     `json:"priority"`
	IsSeed       bool    `json:"is_seed"`
	PieceRange   []int   `json:"piece_range"`
	Availability float64 `json:"availability"`
}

// ParseFilePriority accepts qBittorrent's wire values and readable aliases.
// Numeric values remain available for future server-defined priorities, but
// the four values currently supported by qBittorrent are enforced.
func ParseFilePriority(value string) (FilePriority, error) {
	raw := strings.ToLower(strings.TrimSpace(value))
	switch raw {
	case "skip", "none", "do-not-download", "do_not_download":
		return FilePrioritySkip, nil
	case "normal":
		return FilePriorityNormal, nil
	case "high":
		return FilePriorityHigh, nil
	case "maximum", "max":
		return FilePriorityMaximum, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid file priority %q (use skip, normal, high, maximum, or 0/1/6/7)", value)
	}
	switch FilePriority(parsed) {
	case FilePrioritySkip, FilePriorityNormal, FilePriorityHigh, FilePriorityMaximum:
		return FilePriority(parsed), nil
	default:
		return 0, fmt.Errorf("invalid file priority %q (use skip, normal, high, maximum, or 0/1/6/7)", value)
	}
}

func (priority FilePriority) String() string {
	switch priority {
	case FilePrioritySkip:
		return "skip"
	case FilePriorityNormal:
		return "normal"
	case FilePriorityHigh:
		return "high"
	case FilePriorityMaximum:
		return "maximum"
	default:
		return strconv.Itoa(int(priority))
	}
}

// FetchTorrentFiles returns the complete file list for one torrent.
func (a *App) FetchTorrentFiles(hash string) ([]TorrentFile, error) {
	return a.FetchTorrentFilesContext(a.context(), hash, nil)
}

// FetchTorrentFilesContext returns torrent contents, optionally restricted to
// stable file indexes separated by the qBittorrent API's `|` convention.
func (a *App) FetchTorrentFilesContext(ctx context.Context, hash string, indexes []int) ([]TorrentFile, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, codedErrf(ExitBadArgs, "Torrent hash required")
	}
	params := url.Values{"hash": {hash}}
	if len(indexes) > 0 {
		values := make([]string, len(indexes))
		for i, index := range indexes {
			if index < 0 {
				return nil, codedErrf(ExitBadArgs, "File index must not be negative: %d", index)
			}
			values[i] = strconv.Itoa(index)
		}
		params.Set("indexes", strings.Join(values, "|"))
	}
	body, err := a.client.requestContext(ctx, http.MethodGet, "/api/v2/torrents/files", params)
	if err != nil {
		return nil, codedErrWrapf(ExitFetchFail, err, "Failed to fetch torrent files: %v", err)
	}
	var files []TorrentFile
	if err := json.Unmarshal(body, &files); err != nil {
		return nil, codedErrf(ExitFetchFail, "Invalid JSON from torrent files")
	}
	return files, nil
}

// SetFilePriority changes one or more file priorities. IDs are stable file
// indexes from TorrentFile.Index, not response positions.
func (a *App) SetFilePriority(hash string, ids []int, priority FilePriority) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return codedErrf(ExitBadArgs, "Torrent hash required")
	}
	if len(ids) == 0 {
		return codedErrf(ExitBadArgs, "At least one file index required")
	}
	for _, id := range ids {
		if id < 0 {
			return codedErrf(ExitBadArgs, "File index must not be negative: %d", id)
		}
	}
	switch priority {
	case FilePrioritySkip, FilePriorityNormal, FilePriorityHigh, FilePriorityMaximum:
	default:
		return codedErrf(ExitBadArgs, "Invalid file priority: %d", priority)
	}
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.Itoa(id)
	}
	params := url.Values{
		"hash":     {hash},
		"id":       {strings.Join(values, "|")},
		"priority": {strconv.Itoa(int(priority))},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/filePrio", params); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to set torrent file priority: %v", err)
	}
	return nil
}

func (a *App) RenameTorrentFile(hash, oldPath, newPath string) error {
	return a.renameTorrentPath("file", hash, oldPath, newPath)
}

func (a *App) RenameTorrentFolder(hash, oldPath, newPath string) error {
	return a.renameTorrentPath("folder", hash, oldPath, newPath)
}

func (a *App) renameTorrentPath(kind, hash, oldPath, newPath string) error {
	hash = strings.TrimSpace(hash)
	if hash == "" || strings.TrimSpace(oldPath) == "" || strings.TrimSpace(newPath) == "" {
		return codedErrf(ExitBadArgs, "Torrent hash, old path, and new path are required")
	}
	endpoint := "/api/v2/torrents/renameFile"
	if kind == "folder" {
		endpoint = "/api/v2/torrents/renameFolder"
	}
	params := url.Values{"hash": {hash}, "oldPath": {oldPath}, "newPath": {newPath}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, endpoint, params); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to rename torrent %s: %v", kind, err)
	}
	return nil
}

// TorrentPeer is the structured representation returned by sync/torrentPeers.
// Peer keys are not stable across reconnects, so callers should use the map
// key from TorrentPeers.Peers as the identity when applying deltas.
type TorrentPeer struct {
	Client           string  `json:"client"`
	PeerIDClient     string  `json:"peer_id_client"`
	Connection       string  `json:"connection"`
	Country          string  `json:"country"`
	CountryCode      string  `json:"country_code"`
	DLSpeed          int64   `json:"dl_speed"`
	Files            string  `json:"files"`
	Flags            string  `json:"flags"`
	FlagsDescription string  `json:"flags_desc"`
	HostName         string  `json:"host_name"`
	IP               string  `json:"ip"`
	I2PDestination   string  `json:"i2p_dest"`
	Port             int     `json:"port"`
	Progress         float64 `json:"progress"`
	Relevance        float64 `json:"relevance"`
	Contribution     float64 `json:"contribution"`
	Downloaded       int64   `json:"downloaded"`
	Uploaded         int64   `json:"uploaded"`
	UPSpeed          int64   `json:"up_speed"`
}

// TorrentPeers is one peer sync response. A partial response contains only
// changed peers and PeersRemoved; Merge applies it to a prior snapshot.
type TorrentPeers struct {
	RID          int64                  `json:"rid"`
	FullUpdate   bool                   `json:"full_update"`
	ShowFlags    bool                   `json:"show_flags"`
	Peers        map[string]TorrentPeer `json:"peers"`
	PeersRemoved []string               `json:"peers_removed"`

	rawPeers     map[string]json.RawMessage
	showFlagsSet bool
}

func (peers *TorrentPeers) UnmarshalJSON(data []byte) error {
	var decoded struct {
		RID          int64                  `json:"rid"`
		FullUpdate   bool                   `json:"full_update"`
		ShowFlags    bool                   `json:"show_flags"`
		Peers        map[string]TorrentPeer `json:"peers"`
		PeersRemoved []string               `json:"peers_removed"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw struct {
		Peers     map[string]json.RawMessage `json:"peers"`
		ShowFlags *bool                      `json:"show_flags"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	peers.RID = decoded.RID
	peers.FullUpdate = decoded.FullUpdate
	peers.ShowFlags = decoded.ShowFlags
	peers.Peers = decoded.Peers
	if peers.Peers == nil {
		peers.Peers = make(map[string]TorrentPeer)
	}
	peers.PeersRemoved = decoded.PeersRemoved
	peers.rawPeers = cloneRawPeers(raw.Peers)
	peers.showFlagsSet = raw.ShowFlags != nil
	return nil
}

func cloneRawPeers(peers map[string]json.RawMessage) map[string]json.RawMessage {
	if peers == nil {
		return nil
	}
	clone := make(map[string]json.RawMessage, len(peers))
	for key, value := range peers {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}

func (a *App) FetchTorrentPeers(hash string, rid int64) (TorrentPeers, error) {
	return a.FetchTorrentPeersContext(a.context(), hash, rid)
}

// FetchTorrentPeersContext gets a per-torrent incremental peer response. The
// rid is independent of sync/maindata and must be tracked per torrent.
func (a *App) FetchTorrentPeersContext(ctx context.Context, hash string, rid int64) (TorrentPeers, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return TorrentPeers{}, codedErrf(ExitBadArgs, "Torrent hash required")
	}
	if rid < 0 {
		return TorrentPeers{}, codedErrf(ExitBadArgs, "Peer response ID must not be negative")
	}
	params := url.Values{"hash": {hash}, "rid": {strconv.FormatInt(rid, 10)}}
	body, err := a.client.requestContext(ctx, http.MethodGet, "/api/v2/sync/torrentPeers", params)
	if err != nil {
		return TorrentPeers{}, codedErrWrapf(ExitFetchFail, err, "Failed to fetch torrent peers: %v", err)
	}
	var peers TorrentPeers
	if err := json.Unmarshal(body, &peers); err != nil {
		return TorrentPeers{}, codedErrf(ExitFetchFail, "Invalid JSON from torrent peers")
	}
	if peers.Peers == nil {
		peers.Peers = make(map[string]TorrentPeer)
	}
	return peers, nil
}

// Merge applies a peer sync response and returns the receiver for convenient
// use by a live details view. A first response is treated as full even when a
// fixture omits full_update, matching qBittorrent's rid=0 behavior.
func (peers *TorrentPeers) Merge(update TorrentPeers) {
	if peers.Peers == nil || update.FullUpdate {
		peers.Peers = make(map[string]TorrentPeer, len(update.Peers))
		for key, peer := range update.Peers {
			peers.Peers[key] = peer
		}
		peers.rawPeers = cloneRawPeers(update.rawPeers)
	} else {
		if peers.rawPeers == nil {
			peers.rawPeers = make(map[string]json.RawMessage, len(peers.Peers))
			for key, peer := range peers.Peers {
				if raw, err := json.Marshal(peer); err == nil {
					peers.rawPeers[key] = raw
				}
			}
		}
		for key, peer := range update.Peers {
			if raw, ok := update.rawPeers[key]; ok {
				merged := mergePeerJSON(peers.rawPeers[key], raw)
				if err := json.Unmarshal(merged, &peer); err == nil {
					peers.rawPeers[key] = merged
				}
			}
			peers.Peers[key] = peer
		}
		for _, key := range update.PeersRemoved {
			delete(peers.Peers, key)
			delete(peers.rawPeers, key)
		}
	}
	peers.RID = update.RID
	peers.FullUpdate = update.FullUpdate
	if update.FullUpdate || update.showFlagsSet {
		peers.ShowFlags = update.ShowFlags
	}
}

func mergePeerJSON(current, update json.RawMessage) json.RawMessage {
	var currentFields, updateFields map[string]json.RawMessage
	if json.Unmarshal(current, &currentFields) != nil {
		currentFields = make(map[string]json.RawMessage)
	}
	if json.Unmarshal(update, &updateFields) != nil {
		return append(json.RawMessage(nil), update...)
	}
	if currentFields == nil {
		currentFields = make(map[string]json.RawMessage, len(updateFields))
	}
	for key, value := range updateFields {
		currentFields[key] = append(json.RawMessage(nil), value...)
	}
	merged, err := json.Marshal(currentFields)
	if err != nil {
		return append(json.RawMessage(nil), update...)
	}
	return merged
}
