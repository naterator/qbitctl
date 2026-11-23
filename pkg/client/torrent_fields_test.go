package client

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtendedTorrentFields(t *testing.T) {
	var torrent TorrentInfo
	payload := `{
		"added_on":1700000000,"completion_on":0,"last_activity":1700000001,
		"seen_complete":-1,"time_active":300,"save_path":"/done","download_path":"/incomplete",
		"content_path":"/done/data","amount_left":0,"completed":2048,"availability":1.75,
		"num_seeds":0,"num_leechs":2,"num_complete":100,"num_incomplete":4,"priority":3
	}`
	if err := json.Unmarshal([]byte(payload), &torrent); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"added-on": "1700000000", "completion-on": "0", "last-activity": "1700000001",
		"seen-complete": "-1", "time-active": "300", "save-path": "/done",
		"temp-download-path": "/incomplete", "dl-path": "/done/data",
		"remaining-bytes": "0", "completed-bytes": "2048", "availability": "1.75",
		"seeds": "0", "leechers": "2", "swarm-seeds": "100", "swarm-leechers": "4",
		"queue-position": "3", "tracker-count": "",
	}
	for field, expected := range want {
		if !IsGetField(field) {
			t.Errorf("get does not expose %q", field)
		}
		if got := torrentFieldValue(torrent, field); got != expected {
			t.Errorf("%s = %q, want %q", field, got, expected)
		}
	}
	for _, tc := range []struct {
		units DataUnits
		field string
		want  string
	}{
		{DataUnitsHuman, "completed-bytes", DataUnitsHuman.FormatSize(2048)},
		{DataUnitsBytes, "completed-bytes", "2048 B"},
		{DataUnitsHuman, "added-on", "2023-11-14T22:13:20Z"},
	} {
		if got := torrentDisplayFieldValue(tc.units, torrent, tc.field); got != tc.want {
			t.Errorf("%s (%s) = %q, want %q", tc.field, tc.units, got, tc.want)
		}
	}
	details := (&App{}).TorrentDisplayFieldsFromInfo(torrent)
	if details["Seeds: Connected"] != "0" || details["Temporary Download Path"] != "/incomplete" {
		t.Errorf("unexpected detail rows: %v", details)
	}
	if _, ok := details["Tracker Count"]; ok {
		t.Error("detail shows a tracker count the server omitted")
	}
}

func TestParseSelectableTorrentFields(t *testing.T) {
	fields, err := ParseSelectableTorrentFields("hash, download-path, state")
	if err != nil {
		t.Fatalf("parseSelectableTorrentFields returned error: %v", err)
	}
	want := []string{"hash", "dl-path", "state"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Fatalf("parseSelectableTorrentFields = %v, want %v", fields, want)
	}

	if _, err := ParseSelectableTorrentFields("tracker-list"); err == nil {
		t.Fatal("parseSelectableTorrentFields unexpectedly accepted tracker-list")
	}
}

func TestWriteTorrentFieldRowsAndTemplate(t *testing.T) {
	torrents := []TorrentInfo{{
		Name:     "Ubuntu ISO",
		Hash:     "0123456789abcdef0123456789abcdef01234567",
		State:    "downloading",
		Progress: 0.9999,
	}}

	var rows strings.Builder
	if err := writeTorrentFieldRows(&rows, torrents, []string{"hash", "name", "progress"}, true, DataUnitsHuman); err != nil {
		t.Fatalf("writeTorrentFieldRows returned error: %v", err)
	}
	text := rows.String()
	if !strings.Contains(text, "hash\tname\tprogress") {
		t.Fatalf("field output missing header: %q", text)
	}
	if !strings.Contains(text, "Ubuntu ISO") || !strings.Contains(text, "0.999900") {
		t.Fatalf("field output = %q", text)
	}

	var tmpl strings.Builder
	if err := renderTorrentTemplate(&tmpl, "test", `{{range .}}{{field "hash" .}} {{field "name" .}} {{field "progress" .}}{{end}}`, torrents); err != nil {
		t.Fatalf("renderTorrentTemplate returned error: %v", err)
	}
	if strings.TrimSpace(tmpl.String()) != "01234567 Ubuntu ISO 0.999900" {
		t.Fatalf("template output = %q", tmpl.String())
	}
}
