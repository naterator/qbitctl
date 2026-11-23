//go:build !plan9

package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/naterator/qbitctl/pkg/client"
)

// detailRow is the display-ready form of one value returned by qBittorrent.
// Keeping the label separate from the value lets the view render the same
// transport response as a table, a compact panel, or a detail dialog.
type detailRow struct {
	label string
	value string
}

// actionRequest is the small command vocabulary shared by the TUI model and
// the client adapter. hashes may contain the pipe-separated hash list accepted
// by qBittorrent's torrent endpoints.
type actionRequest struct {
	kind       string
	hashes     string
	value      string
	original   string
	field      string
	id         int64
	addOptions client.AddTorrentOptions
}

type queueSync struct {
	rid          int64
	torrents     map[string]map[string]json.RawMessage
	decoded      map[string]client.TorrentInfo
	snapshot     []client.TorrentInfo
	serverState  map[string]json.RawMessage
	queueChanged bool
	fallback     bool
	notice       string
}

// tuiClient adapts the existing client.App API to the TUI. The App is copied
// for each operation so a model can safely provide a request context and
// renderer-owned writers without changing the command app. Copying App keeps
// its underlying Client (and therefore its authenticated cookie jar) shared.
type tuiClient struct {
	app          *client.App
	queueState   queueSync
	dataUnitsMu  sync.RWMutex
	dataUnits    client.DataUnits
	dataUnitsSet bool
}

func (c *tuiClient) setDataUnits(mode client.DataUnits) {
	if c == nil {
		return
	}
	c.dataUnitsMu.Lock()
	c.dataUnits, c.dataUnitsSet = mode, true
	c.dataUnitsMu.Unlock()
}

func (c *tuiClient) searchPlugins(ctx context.Context) ([]searchPluginInfo, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	app := c.withContext(ctx, io.Discard, io.Discard)
	return app.SearchPlugins()
}

func (c *tuiClient) categories(ctx context.Context) ([]client.Category, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	app := c.withContext(ctx, io.Discard, io.Discard)
	return app.GetCategories()
}

func (c *tuiClient) tags(ctx context.Context) ([]string, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	app := c.withContext(ctx, io.Discard, io.Discard)
	return app.GetTags()
}

func (c *tuiClient) withContext(ctx context.Context, stdout, stderr io.Writer) *client.App {
	if ctx == nil {
		ctx = context.Background()
	}
	c.dataUnitsMu.RLock()
	mode, modeSet := c.dataUnits, c.dataUnitsSet
	c.dataUnitsMu.RUnlock()
	clone := *c.app
	if modeSet {
		clone.DataUnits = mode
	}
	clone.Ctx = ctx
	clone.Stdout = stdout
	clone.Stderr = stderr
	return &clone
}

// queue fetches the torrent list using qBittorrent's incremental sync API.
// The fallback is deliberately sticky for this session: a server that does
// not implement maindata should not be probed every five seconds.
func (c *tuiClient) queue(ctx context.Context) ([]client.TorrentInfo, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	// The marker belongs to the response currently being fetched. Clearing it
	// before every request prevents a failed or directly-consumed request from
	// making a later no-op response rebuild the queue.
	c.queueState.queueChanged = false
	app := c.withContext(ctx, io.Discard, io.Discard)
	if c.queueState.fallback {
		return c.fetchQueueInfo(app)
	}

	body, err := app.FetchMainData(app.Ctx, c.queueState.rid)
	if err != nil {
		if !isMainDataNotFound(err) {
			return nil, err
		}
		return c.fallbackQueue(app, err)
	}
	torrents, err := c.applyMainData(body)
	if err != nil {
		return c.fallbackQueue(app, err)
	}
	return torrents, nil
}

func (c *tuiClient) fetchQueueInfo(app *client.App) ([]client.TorrentInfo, error) {
	torrents, _, err := app.FetchAllTorrentsContext(app.Ctx)
	if err == nil {
		decoded := make(map[string]client.TorrentInfo, len(torrents))
		for _, torrent := range torrents {
			if torrent.Hash != "" {
				decoded[torrent.Hash] = torrent
			}
		}
		c.queueState.decoded = decoded
		c.queueState.snapshot = append([]client.TorrentInfo(nil), torrents...)
		c.queueState.queueChanged = true
		// A full torrent-list fallback does not contain server_state. Do not
		// keep presenting a potentially stale transfer snapshot on Server.
		c.queueState.serverState = nil
	}
	return torrents, err
}

func isMainDataNotFound(err error) bool {
	var httpErr *client.HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}

func (c *tuiClient) fallbackQueue(app *client.App, reason error) ([]client.TorrentInfo, error) {
	if !c.queueState.fallback {
		c.queueState.fallback = true
		c.queueState.notice = fmt.Sprintf(
			"Incremental queue refresh unavailable (%v); using full refresh",
			reason,
		)
	}
	return c.fetchQueueInfo(app)
}

func (c *tuiClient) takeQueueNotice() string {
	notice := c.queueState.notice
	c.queueState.notice = ""
	return notice
}

type mainDataResponse struct {
	RID             *int64                                `json:"rid"`
	FullUpdate      bool                                  `json:"full_update"`
	Torrents        map[string]map[string]json.RawMessage `json:"torrents"`
	TorrentsRemoved []string                              `json:"torrents_removed"`
	ServerState     map[string]json.RawMessage            `json:"server_state"`
}

func (c *tuiClient) applyMainData(body []byte) ([]client.TorrentInfo, error) {
	var response mainDataResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("invalid maindata response: %w", err)
	}
	if response.RID == nil {
		return nil, fmt.Errorf("maindata response omitted rid")
	}

	if !response.FullUpdate && (c.queueState.torrents == nil || c.queueState.decoded == nil) {
		return nil, fmt.Errorf("maindata response was partial before a full update")
	}
	if !response.FullUpdate && *response.RID < c.queueState.rid {
		return nil, fmt.Errorf("maindata rid reset from %d to %d without a full update", c.queueState.rid, *response.RID)
	}

	if response.FullUpdate {
		merged := make(map[string]map[string]json.RawMessage, len(response.Torrents))
		decoded := make(map[string]client.TorrentInfo, len(response.Torrents))
		for hash, fields := range response.Torrents {
			if hash == "" {
				return nil, fmt.Errorf("maindata response contained an empty torrent hash")
			}
			current := cloneTorrentFields(fields)
			torrent, err := decodeMainDataTorrent(hash, current)
			if err != nil {
				return nil, err
			}
			merged[hash] = current
			decoded[hash] = torrent
		}
		c.queueState.rid = *response.RID
		c.queueState.torrents = merged
		c.queueState.decoded = decoded
		c.queueState.serverState = cloneTorrentFields(response.ServerState)
		c.queueState.queueChanged = true
		c.queueState.snapshot = mainDataTorrentList(decoded)
		return c.queueState.snapshot, nil
	}

	// Decode every changed torrent before touching the live maps. This keeps a
	// malformed partial response from leaving a half-applied queue behind and
	// avoids re-decoding unchanged entries on every refresh.
	updates := make(map[string]map[string]json.RawMessage, len(response.Torrents))
	decodedUpdates := make(map[string]client.TorrentInfo, len(response.Torrents))
	queueChanged := false
	for hash, fields := range response.Torrents {
		if hash == "" {
			return nil, fmt.Errorf("maindata response contained an empty torrent hash")
		}
		current := cloneTorrentFields(c.queueState.torrents[hash])
		if current == nil {
			current = make(map[string]json.RawMessage, len(fields)+1)
			queueChanged = true
		}
		for field, value := range fields {
			if !rawMessagesEqual(current[field], value) {
				queueChanged = true
			}
			current[field] = append(json.RawMessage(nil), value...)
		}
		torrent, err := decodeMainDataTorrent(hash, current)
		if err != nil {
			return nil, err
		}
		updates[hash] = current
		decodedUpdates[hash] = torrent
	}
	for _, hash := range response.TorrentsRemoved {
		if deleteTorrentHash(c.queueState.torrents, hash) {
			queueChanged = true
		}
		deleteTorrentHashInfo(c.queueState.decoded, hash)
	}
	for hash, fields := range updates {
		c.queueState.torrents[hash] = fields
		c.queueState.decoded[hash] = decodedUpdates[hash]
	}
	if response.ServerState != nil {
		if c.queueState.serverState == nil {
			c.queueState.serverState = make(map[string]json.RawMessage, len(response.ServerState))
		}
		for field, value := range response.ServerState {
			c.queueState.serverState[field] = append(json.RawMessage(nil), value...)
		}
	}
	c.queueState.rid = *response.RID
	c.queueState.queueChanged = queueChanged
	if queueChanged || c.queueState.snapshot == nil {
		c.queueState.snapshot = mainDataTorrentList(c.queueState.decoded)
	}
	return c.queueState.snapshot, nil
}

func rawMessagesEqual(left, right json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(left), bytes.TrimSpace(right))
}

func cloneTorrentFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	if fields == nil {
		return nil
	}
	clone := make(map[string]json.RawMessage, len(fields)+1)
	for field, value := range fields {
		clone[field] = append(json.RawMessage(nil), value...)
	}
	return clone
}

func deleteTorrentHash(torrents map[string]map[string]json.RawMessage, hash string) bool {
	if _, ok := torrents[hash]; ok {
		delete(torrents, hash)
		return true
	}
	for existing := range torrents {
		if strings.EqualFold(existing, hash) {
			delete(torrents, existing)
			return true
		}
	}
	return false
}

func deleteTorrentHashInfo(torrents map[string]client.TorrentInfo, hash string) {
	if _, ok := torrents[hash]; ok {
		delete(torrents, hash)
		return
	}
	for existing := range torrents {
		if strings.EqualFold(existing, hash) {
			delete(torrents, existing)
			return
		}
	}
}

// takeQueueChanged is consumed by fetchQueueUpdate after a sync response has
// been decoded. Keeping this as an optional backend method preserves the
// lightweight service interface used by model tests and other adapters.
func (c *tuiClient) takeQueueChanged() bool {
	changed := c.queueState.queueChanged
	c.queueState.queueChanged = false
	return changed
}

type queueTransferState struct {
	download *int64
	upload   *int64
	free     *int64
}

func (c *tuiClient) serverTransferState() queueTransferState {
	state := c.queueState.serverState
	return queueTransferState{
		download: syncNumber(state["dl_info_speed"]),
		upload:   syncNumber(state["up_info_speed"]),
		free:     syncNumber(state["free_space_on_disk"]),
	}
}

func syncNumber(raw json.RawMessage) *int64 {
	var value *int64
	if len(raw) > 0 && json.Unmarshal(raw, &value) == nil && value != nil && *value >= 0 {
		return value
	}
	return nil
}

func decodeMainDataTorrent(hash string, fields map[string]json.RawMessage) (client.TorrentInfo, error) {
	data := cloneTorrentFields(fields)
	if data == nil {
		data = make(map[string]json.RawMessage, 1)
	}
	encodedHash, err := json.Marshal(hash)
	if err != nil {
		return client.TorrentInfo{}, fmt.Errorf("encode torrent hash: %w", err)
	}
	data["hash"] = encodedHash
	encoded, err := json.Marshal(data)
	if err != nil {
		return client.TorrentInfo{}, fmt.Errorf("encode torrent %s: %w", hash, err)
	}
	var torrent client.TorrentInfo
	if err := json.Unmarshal(encoded, &torrent); err != nil {
		return client.TorrentInfo{}, fmt.Errorf("decode torrent %s: %w", hash, err)
	}
	return torrent, nil
}

func mainDataTorrentList(torrents map[string]client.TorrentInfo) []client.TorrentInfo {
	list := make([]client.TorrentInfo, 0, len(torrents))
	for _, torrent := range torrents {
		list = append(list, torrent)
	}
	sort.Slice(list, func(i, j int) bool {
		leftName, rightName := strings.ToLower(list[i].Name), strings.ToLower(list[j].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		leftHash, rightHash := strings.ToLower(list[i].Hash), strings.ToLower(list[j].Hash)
		if leftHash != rightHash {
			return leftHash < rightHash
		}
		return list[i].Hash < list[j].Hash
	})
	return list
}

// search runs one qBittorrent search job, captures client diagnostics, and
// assigns the same local cache IDs used by the command-line search command.
// Partial results are a successful result from App.Search; its warning is
// returned separately so the model can show it without polluting the table.
func (c *tuiClient) search(ctx context.Context, query, plugin string, timeout time.Duration) ([]client.SearchResult, string, error) {
	if c == nil || c.app == nil {
		return nil, "", fmt.Errorf("qBittorrent client is not configured")
	}

	var stdout, stderr bytes.Buffer
	app := c.withContext(ctx, &stdout, &stderr)
	resolved, err := app.ResolveSearchPlugins(plugin)
	if err != nil {
		return nil, joinDiagnostics(stderr.String(), stdout.String()), err
	}
	return c.searchResolved(app, &stdout, &stderr, query, resolved, timeout)
}

// searchWithCatalog resolves against a catalog that was already fetched by
// the TUI. Unknown selectors fall back to App.ResolveSearchPlugins so older
// servers and a catalog changed outside qbitctl retain the existing behavior.
func (c *tuiClient) searchWithCatalog(ctx context.Context, query, plugin string, timeout time.Duration, catalog []searchPluginInfo) ([]client.SearchResult, string, error) {
	if c == nil || c.app == nil {
		return nil, "", fmt.Errorf("qBittorrent client is not configured")
	}

	var stdout, stderr bytes.Buffer
	app := c.withContext(ctx, &stdout, &stderr)
	resolved, err := client.ResolveSearchPluginSelection(plugin, catalog)
	if err != nil {
		resolved, err = app.ResolveSearchPlugins(plugin)
		if err != nil {
			return nil, joinDiagnostics(stderr.String(), stdout.String()), err
		}
	}
	return c.searchResolved(app, &stdout, &stderr, query, resolved, timeout)
}

func (c *tuiClient) searchResolved(app *client.App, stdout, stderr *bytes.Buffer, query, resolved string, timeout time.Duration) ([]client.SearchResult, string, error) {
	results, err := app.Search(query, resolved, timeout)
	warning := joinDiagnostics(stderr.String(), stdout.String())
	if err != nil {
		return nil, warning, err
	}
	if err := app.CacheSearchResultsForQuery(query, results); err != nil {
		warning = joinDiagnostics(warning, fmt.Sprintf("[WARN] %v", err))
	}
	return results, warning, nil
}

// details returns the existing detailed torrent presentation as rows. The
// client owns unit formatting and sentinel handling; tracker rows are fetched
// only when requested.
func (c *tuiClient) details(ctx context.Context, hash string, trackers bool) ([]detailRow, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	app := c.withContext(ctx, io.Discard, io.Discard)
	if trackers {
		trackers, err := app.FetchTrackerEntries(hash)
		if err != nil {
			return nil, err
		}
		return expandTrackerEntries(trackers), nil
	}
	// maindata already carries the standard torrent fields used by the detail
	// view. Reuse that row instead of issuing a second /torrents/info request;
	// tracker details intentionally remain a separate request below.
	if torrent, ok := lookupTorrent(c.queueState.decoded, hash); ok {
		fields := app.TorrentDisplayFieldsFromInfo(torrent)
		return orderRows(detailRowsFromMap(fields), detailRowOrder), nil
	}
	if c.queueState.decoded != nil {
		return nil, fmt.Errorf("torrent %q is no longer present in the synchronized queue", hash)
	}

	fields, err := app.TorrentDisplayFields(hash)
	if err != nil {
		return nil, err
	}
	return orderRows(detailRowsFromMap(fields), detailRowOrder), nil
}

func (c *tuiClient) trackers(ctx context.Context, hash string) ([]client.TrackerEntry, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	app := c.withContext(ctx, io.Discard, io.Discard)
	return app.FetchTrackerEntries(hash)
}

// server returns the server/transfer summary in the same order as the
// existing human-readable command, while retaining any future fields added by
// qBittorrent at the end in stable order.
func (c *tuiClient) server(ctx context.Context) ([]detailRow, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	app := c.withContext(ctx, io.Discard, io.Discard)
	// The active preset can change while the Server tab is open. The queue's
	// sync state only updates when the queue is polled.
	fields, err := app.ServerDisplayFields()
	if err != nil {
		return nil, err
	}
	alternate, modeErr := app.FetchSpeedLimitsMode()
	mode := "normal"
	if modeErr != nil {
		mode = "unavailable"
	} else if alternate {
		mode = "alternate"
	}
	fields["Speed limits mode"] = mode
	return orderRows(detailRowsFromMap(fields), serverRowOrder), nil
}

func lookupTorrent(torrents map[string]client.TorrentInfo, hash string) (client.TorrentInfo, bool) {
	if torrent, ok := torrents[hash]; ok {
		return torrent, true
	}
	for existing, torrent := range torrents {
		if strings.EqualFold(existing, hash) || strings.EqualFold(torrent.Hash, hash) {
			return torrent, true
		}
	}
	return client.TorrentInfo{}, false
}

// action executes one TUI mutation against the shared authenticated client.
// Diagnostics are intentionally kept off the terminal; the returned client
// error remains the source of truth for the model's status line.
func (c *tuiClient) action(ctx context.Context, req actionRequest) error {
	if c == nil || c.app == nil {
		return fmt.Errorf("qBittorrent client is not configured")
	}
	app := c.withContext(ctx, io.Discard, io.Discard)
	switch req.kind {
	case "add":
		source := req.value
		if strings.HasPrefix(source, "~/") || strings.HasPrefix(source, `~\`) {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("locate home directory for torrent file: %w", err)
			}
			source = filepath.Join(home, source[2:])
		}
		return app.AddTorrentWithOptions(source, req.addOptions)
	case "add-result":
		return app.AddSearchResultWithOptions(req.id, req.addOptions)
	case "start":
		return app.StartTorrent(req.hashes)
	case "pause":
		return app.PauseTorrent(req.hashes)
	case "force-start":
		return app.ForceStartTorrent(req.hashes)
	case "recheck":
		return app.RecheckTorrent(req.hashes)
	case "reannounce":
		return app.ReannounceTorrent(req.hashes)
	case "queue-up":
		return app.QueueUpTorrent(req.hashes)
	case "queue-down":
		return app.QueueDownTorrent(req.hashes)
	case "queue-top":
		return app.QueueTopTorrent(req.hashes)
	case "queue-bottom":
		return app.QueueBottomTorrent(req.hashes)
	case "move":
		return app.MoveTorrent(req.hashes, req.value)
	case "rename":
		return app.RenameTorrent(req.hashes, req.value)
	case "remove":
		return app.StopAndRemoveTorrent(req.hashes, false)
	case "delete":
		return app.StopAndRemoveTorrent(req.hashes, true)
	case "set":
		return app.SetField(req.hashes, req.field, req.value)
	case "tracker-add":
		return app.AddTrackers(req.hashes, req.value)
	case "tracker-edit":
		return app.EditTracker(req.hashes, req.original, req.value)
	case "tracker-remove":
		return app.RemoveTrackers(req.hashes, req.value)
	case "transfer-download":
		return app.SetGlobalDownloadLimit(req.value)
	case "transfer-upload":
		return app.SetGlobalUploadLimit(req.value)
	case "transfer-mode":
		return app.SetSpeedLimitsMode(strings.EqualFold(strings.TrimSpace(req.value), "alternate"))
	case "ban-peer":
		return app.BanPeers(req.value)
	default:
		return fmt.Errorf("unsupported TUI action %q", req.kind)
	}
}

var serverRowOrder = []string{
	"qBittorrent",
	"WebAPI",
	"Connection",
	"DHT Nodes",
	"Speed: Download",
	"Speed: Upload",
	"Transferred: Down",
	"Transferred: Up",
	"Limit: Download",
	"Limit: Upload",
	"Speed limits mode",
}

var detailRowOrder = []string{
	"Name",
	"Hash: Short",
	"Hash: Full",
	"Progress",
	"State",
	"Tags",
	"Auto TMM",
	"Category",
	"ETA",
	"Full Path",
	"Limit: Download",
	"Limit: Upload",
	"Private",
	"Ratio: Current",
	"Ratio: Limit",
	"Seedtime: Current",
	"Seedtime: Limit",
	"Sequential Download",
	"Size",
	"Speed: Download",
	"Speed: Upload",
	"Superseed",
	"Tracker",
	"Transferred: Down",
	"Transferred: Up",
	"tracker-list",
}

func decodeRows(data []byte) ([]detailRow, error) {
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	rows := make([]detailRow, 0, len(values))
	for label, value := range values {
		rows = append(rows, detailRow{label: label, value: value})
	}
	return rows, nil
}

func detailRowsFromMap(values map[string]string) []detailRow {
	rows := make([]detailRow, 0, len(values))
	for label, value := range values {
		rows = append(rows, detailRow{label: label, value: value})
	}
	return rows
}

func expandTrackerRows(rows []detailRow) []detailRow {
	trackers := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.label != "tracker-list" {
			continue
		}
		for _, line := range strings.Split(row.value, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			for _, value := range strings.Split(line, ",") {
				value = strings.TrimSpace(value)
				if value != "" {
					trackers = append(trackers, value)
				}
			}
		}
	}
	result := make([]detailRow, 0, len(trackers))
	for i, tracker := range trackers {
		result = append(result, detailRow{label: fmt.Sprintf("Tracker %d", i+1), value: tracker})
	}
	return result
}

func expandTrackerEntries(trackers []client.TrackerEntry) []detailRow {
	result := make([]detailRow, 0, len(trackers))
	for _, tracker := range trackers {
		url := strings.TrimSpace(tracker.URL)
		switch url {
		case "", "** [DHT] **", "** [PeX] **", "** [LSD] **":
			continue
		}
		result = append(result, detailRow{label: fmt.Sprintf("Tracker %d", len(result)+1), value: url})
	}
	return result
}

func orderRows(rows []detailRow, preferred []string) []detailRow {
	if len(rows) < 2 {
		return rows
	}
	byLabel := make(map[string]detailRow, len(rows))
	for _, row := range rows {
		byLabel[row.label] = row
	}
	ordered := make([]detailRow, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, label := range preferred {
		if row, ok := byLabel[label]; ok {
			ordered = append(ordered, row)
			seen[label] = struct{}{}
		}
	}
	remaining := make([]detailRow, 0, len(rows)-len(ordered))
	for _, row := range rows {
		if _, ok := seen[row.label]; !ok {
			remaining = append(remaining, row)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		return remaining[i].label < remaining[j].label
	})
	return append(ordered, remaining...)
}

func joinDiagnostics(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			clean = append(clean, part)
		}
	}
	return strings.Join(clean, "\n")
}
