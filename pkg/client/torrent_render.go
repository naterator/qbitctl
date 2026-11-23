package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type fieldSpec struct {
	label string
	field string
}

var detailedTorrentFields = []fieldSpec{
	{label: "Name", field: "name"},
	{label: "Hash: Short", field: "hash"},
	{label: "Hash: Full", field: "hash-full"},
	{label: "Progress", field: "progress"},
	{label: "State", field: "state"},
	{label: "Tags", field: "tags"},
	{label: "Auto TMM", field: "autotmm"},
	{label: "Category", field: "category"},
	{label: "ETA", field: "eta"},
	{label: "Full Path", field: "dl-path"},
	{label: "Limit: Download", field: "dl-limit"},
	{label: "Limit: Upload", field: "up-limit"},
	{label: "Private", field: "private"},
	{label: "Ratio: Current", field: "ratio"},
	{label: "Ratio: Limit", field: "ratio-limit"},
	{label: "Seedtime: Current", field: "seedtime"},
	{label: "Seedtime: Limit", field: "seedtime-limit"},
	{label: "Sequential Download", field: "seqdl"},
	{label: "Size", field: "size"},
	{label: "Speed: Download", field: "dl-speed"},
	{label: "Speed: Upload", field: "up-speed"},
	{label: "Superseed", field: "superseed"},
	{label: "Tracker", field: "tracker"},
	{label: "Transferred: Down", field: "downloaded"},
	{label: "Transferred: Up", field: "uploaded"},
}

func (a *App) ShowAllTorrentsInfoJSON() error {
	return a.ShowAllTorrentsInfoJSONWithFilter(TorrentListFilter{})
}

func (a *App) ShowAllTorrentsInfoJSONWithFilter(filter TorrentListFilter) error {
	_, body, err := a.FetchTorrentsContext(a.context(), filter)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, string(body))
	return nil
}

func (a *App) ShowSingleTorrentInfoJSON(hash string) error {
	body, err := a.fetchSingleTorrentBody(hash)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, string(body))
	return nil
}

func (a *App) ShowAllTorrentsFields(fields []string, header bool) error {
	return a.ShowAllTorrentsFieldsWithFilter(fields, header, TorrentListFilter{})
}

func (a *App) ShowAllTorrentsFieldsWithFilter(fields []string, header bool, filter TorrentListFilter) error {
	torrents, _, err := a.FetchTorrentsContext(a.context(), filter)
	if err != nil {
		return err
	}
	if err := writeTorrentFieldRows(a.Stdout, torrents, fields, header, a.DataUnits); err != nil {
		return codedErrf(ExitFetchFail, "Failed to render field output: %v", err)
	}
	return nil
}

func (a *App) ShowSingleTorrentFields(hash string, fields []string, header bool) error {
	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return err
	}
	if err := writeTorrentFieldRows(a.Stdout, []TorrentInfo{torrent}, fields, header, a.DataUnits); err != nil {
		return codedErrf(ExitFetchFail, "Failed to render field output: %v", err)
	}
	return nil
}

func (a *App) ShowAllTorrentsTemplate(tmpl string) error {
	return a.ShowAllTorrentsTemplateWithFilter(tmpl, TorrentListFilter{})
}

func (a *App) ShowAllTorrentsTemplateWithFilter(tmpl string, filter TorrentListFilter) error {
	torrents, _, err := a.FetchTorrentsContext(a.context(), filter)
	if err != nil {
		return err
	}
	if err := renderTorrentTemplate(a.Stdout, "list", tmpl, torrents); err != nil {
		return codedErrf(ExitBadArgs, "Failed to render template: %v", err)
	}
	return nil
}

func (a *App) ShowSingleTorrentTemplate(hash, tmpl string) error {
	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return err
	}
	if err := renderTorrentTemplate(a.Stdout, "show", tmpl, torrent); err != nil {
		return codedErrf(ExitBadArgs, "Failed to render template: %v", err)
	}
	return nil
}

var summaryTorrentFields = []fieldSpec{
	{label: "Name", field: "name"},
	{label: "Hash", field: "hash"},
	{label: "Done", field: "progress-pct"},
	{label: "State", field: "state"},
	{label: "Tags", field: "tags"},
}

type labeledRow struct {
	label string
	value string
}

func rowsToMap(rows []labeledRow) map[string]string {
	m := make(map[string]string, len(rows))
	for _, row := range rows {
		m[row.label] = row.value
	}
	return m
}

func renderBlurbJSON(w io.Writer, rows []labeledRow) error {
	data, err := json.MarshalIndent(rowsToMap(rows), "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, string(data))
	return nil
}

func renderBlurbJSONArray(w io.Writer, items [][]labeledRow) error {
	maps := make([]map[string]string, len(items))
	for i, rows := range items {
		maps[i] = rowsToMap(rows)
	}
	data, err := json.MarshalIndent(maps, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, string(data))
	return nil
}

func renderBlurb(w io.Writer, rows []labeledRow) {
	maxLen := 0
	for _, row := range rows {
		if len(row.label) > maxLen {
			maxLen = len(row.label)
		}
	}
	for _, row := range rows {
		dots := strings.Repeat(".", maxLen-len(row.label)+2)
		fmt.Fprintf(w, "%s%s| %s\n", row.label, dots, row.value)
	}
}

func torrentBlurbRows(torrent TorrentInfo, fields []fieldSpec) []labeledRow {
	rows := make([]labeledRow, len(fields))
	for i, f := range fields {
		rows[i] = labeledRow{label: f.label, value: torrentFieldValue(torrent, f.field)}
	}
	return rows
}

func torrentDisplayBlurbRows(torrent TorrentInfo, fields []fieldSpec, units DataUnits) []labeledRow {
	rows := make([]labeledRow, len(fields))
	for i, f := range fields {
		rows[i] = labeledRow{label: f.label, value: torrentDisplayFieldValue(units, torrent, f.field)}
	}
	return rows
}

func (a *App) ShowSingleTorrentInfo(hash string) error {
	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return err
	}
	renderBlurb(a.Stdout, torrentDisplayBlurbRows(torrent, detailedTorrentFields, a.DataUnits))
	return nil
}

// TorrentDisplayFields returns the human-facing fields used by the terminal
// UI's torrent detail view. It intentionally shares the CLI renderer so unit
// selection and sentinel handling stay consistent across both interfaces.
func (a *App) TorrentDisplayFields(hash string) (map[string]string, error) {
	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return nil, err
	}
	return a.TorrentDisplayFieldsFromInfo(torrent), nil
}

// TorrentDisplayFieldsFromInfo renders the same human-facing detail fields as
// TorrentDisplayFields without making an HTTP request. TUI callers can use a
// torrent already returned by sync/maindata to avoid refetching the row.
func (a *App) TorrentDisplayFieldsFromInfo(torrent TorrentInfo) map[string]string {
	return rowsToMap(torrentDisplayBlurbRows(torrent, detailedTorrentFields, a.DataUnits))
}

func (a *App) ShowSingleTorrentInfoAsJSON(hash string) error {
	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return err
	}
	if err := renderBlurbJSON(a.Stdout, torrentBlurbRows(torrent, detailedTorrentFields)); err != nil {
		return codedErrf(ExitFetchFail, "Failed to render JSON: %v", err)
	}
	return nil
}

func (a *App) ShowAllTorrentsInfo() error {
	return a.ShowAllTorrentsInfoWithFilter(TorrentListFilter{})
}

func (a *App) ShowAllTorrentsInfoWithFilter(filter TorrentListFilter) error {
	torrents, _, err := a.FetchTorrentsContext(a.context(), filter)
	if err != nil {
		return err
	}

	sort.Slice(torrents, func(i, j int) bool {
		return torrents[i].Progress > torrents[j].Progress
	})

	for i, torrent := range torrents {
		if i > 0 {
			fmt.Fprintln(a.Stdout)
		}
		renderBlurb(a.Stdout, torrentDisplayBlurbRows(torrent, summaryTorrentFields, a.DataUnits))
	}

	return nil
}

func (a *App) ShowAllTorrentsInfoAsJSON() error {
	return a.ShowAllTorrentsInfoAsJSONWithFilter(TorrentListFilter{})
}

func (a *App) ShowAllTorrentsInfoAsJSONWithFilter(filter TorrentListFilter) error {
	torrents, _, err := a.FetchTorrentsContext(a.context(), filter)
	if err != nil {
		return err
	}

	sort.Slice(torrents, func(i, j int) bool {
		return torrents[i].Progress > torrents[j].Progress
	})

	items := make([][]labeledRow, len(torrents))
	for i, torrent := range torrents {
		items[i] = torrentBlurbRows(torrent, summaryTorrentFields)
	}
	if err := renderBlurbJSONArray(a.Stdout, items); err != nil {
		return codedErrf(ExitFetchFail, "Failed to render JSON: %v", err)
	}
	return nil
}

func (a *App) fetchTrackers(hash string) ([]TrackerEntry, error) {
	params := url.Values{"hash": {hash}}
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/torrents/trackers", params)
	if err != nil {
		return nil, codedErrWrapf(ExitFetchFail, err, "Failed to fetch tracker list: %v", err)
	}

	var trackers []TrackerEntry
	if err := json.Unmarshal(body, &trackers); err != nil {
		return nil, codedErrf(ExitFetchFail, "Invalid JSON from tracker endpoint")
	}
	return trackers, nil
}

// FetchTrackerEntries returns the structured tracker records for a torrent.
// Callers that render a UI should use these records instead of parsing the
// CLI's comma-separated tracker-list representation; tracker URLs may contain
// commas in query values.
func (a *App) FetchTrackerEntries(hash string) ([]TrackerEntry, error) {
	return a.fetchTrackers(hash)
}

func (a *App) getTrackerList(hash string) error {
	trackers, err := a.fetchTrackers(hash)
	if err != nil {
		return err
	}

	output := trackerListOutput(trackers)
	if output != "" {
		fmt.Fprintln(a.Stdout, output)
	}

	return nil
}

func (a *App) GetField(hash, field string) error {
	if field == "tracker-list" {
		return a.getTrackerList(hash)
	}
	if !isSelectableTorrentField(field) {
		return codedErrf(ExitBadArgs, "Unknown field: %s", field)
	}
	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, torrentDisplayFieldValue(a.DataUnits, torrent, field))
	return nil
}

func (a *App) GetFieldJSON(hash, field string) error {
	if field != "tracker-list" && !isSelectableTorrentField(field) {
		return codedErrf(ExitBadArgs, "Unknown field: %s", field)
	}

	var value string
	if field == "tracker-list" {
		trackers, err := a.fetchTrackers(hash)
		if err != nil {
			return err
		}
		value = trackerListOutput(trackers)
	} else {
		torrent, err := a.GetTorrentInfo(hash)
		if err != nil {
			return err
		}
		value = torrentFieldValue(torrent, field)
	}
	rows := []labeledRow{{label: field, value: value}}
	if err := renderBlurbJSON(a.Stdout, rows); err != nil {
		return codedErrf(ExitFetchFail, "Failed to render JSON: %v", err)
	}
	return nil
}
