//go:build !plan9

package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	field      string
	id         int64
	addOptions client.AddTorrentOptions
}

// tuiClient adapts the existing client.App API to the TUI. The App is copied
// for each operation so a model can safely provide a request context and
// renderer-owned writers without changing the command app. Copying App keeps
// its underlying Client (and therefore its authenticated cookie jar) shared.
type tuiClient struct {
	app *client.App
}

func (c *tuiClient) withContext(ctx context.Context, stdout, stderr io.Writer) *client.App {
	if ctx == nil {
		ctx = context.Background()
	}
	clone := *c.app
	clone.Ctx = ctx
	clone.Stdout = stdout
	clone.Stderr = stderr
	return &clone
}

// queue fetches the complete torrent list using the model's context.
func (c *tuiClient) queue(ctx context.Context) ([]client.TorrentInfo, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	app := c.withContext(ctx, io.Discard, io.Discard)
	torrents, _, err := app.FetchAllTorrentsContext(app.Ctx)
	return torrents, err
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
	results, err := app.Search(query, plugin, timeout)
	warning := joinDiagnostics(stderr.String(), stdout.String())
	if err != nil {
		return nil, warning, err
	}
	if err := app.CacheSearchResults(results); err != nil {
		warning = joinDiagnostics(warning, fmt.Sprintf("[WARN] %v", err))
	}
	return results, warning, nil
}

// details returns the existing detailed torrent presentation as rows. Using
// ShowSingleTorrentInfoAsJSON keeps formatting, field names, and compatibility
// behavior in one place. Tracker rows are fetched only when requested.
func (c *tuiClient) details(ctx context.Context, hash string, trackers bool) ([]detailRow, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	var output bytes.Buffer
	app := c.withContext(ctx, &output, io.Discard)
	if trackers {
		trackers, err := app.FetchTrackerEntries(hash)
		if err != nil {
			return nil, err
		}
		return expandTrackerEntries(trackers), nil
	}

	if err := app.ShowSingleTorrentInfoAsJSON(hash); err != nil {
		return nil, err
	}
	rows, err := decodeRows(output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("decode torrent details: %w", err)
	}
	return orderRows(rows, detailRowOrder), nil
}

// server returns the server/transfer summary in the same order as the
// existing human-readable command, while retaining any future fields added by
// qBittorrent at the end in stable order.
func (c *tuiClient) server(ctx context.Context) ([]detailRow, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("qBittorrent client is not configured")
	}
	var output bytes.Buffer
	app := c.withContext(ctx, &output, io.Discard)
	if err := app.ShowServerInfoAsJSON(); err != nil {
		return nil, err
	}
	rows, err := decodeRows(output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("decode server details: %w", err)
	}
	return orderRows(rows, serverRowOrder), nil
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
		return app.AddSearchResult(req.id)
	case "start":
		return app.StartTorrent(req.hashes)
	case "pause":
		return app.PauseTorrent(req.hashes)
	case "force-start":
		return app.ForceStartTorrent(req.hashes)
	case "move":
		return app.MoveTorrent(req.hashes, req.value)
	case "remove":
		return app.StopAndRemoveTorrent(req.hashes, false)
	case "delete":
		return app.StopAndRemoveTorrent(req.hashes, true)
	case "set":
		return app.SetField(req.hashes, req.field, req.value)
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
