package client

import (
	"strings"
	"testing"
)

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
	if err := writeTorrentFieldRows(&rows, torrents, []string{"hash", "name", "progress"}, true); err != nil {
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
