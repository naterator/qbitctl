package client

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestGetTrackerList(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.trackers = []TrackerEntry{
		{URL: "http://Tracker.Example.com:8080/announce"},
		{URL: "http://tracker.example.com/other"},
		{URL: "udp://tracker.two.example.com:80/announce"},
		{URL: "** [DHT] **"},
	}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.getTrackerList(fullHash); err != nil {
		t.Fatalf("getTrackerList err = %v", err)
	}
	stdout := buf.String()
	if !strings.Contains(stdout, "http://Tracker.Example.com:8080/announce") || !strings.Contains(stdout, "udp://tracker.two.example.com:80/announce") {
		t.Fatalf("tracker list = %q", stdout)
	}
}

func TestShowSingleTorrentInfo(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{{
		Name:     "Ubuntu ISO",
		Hash:     fullHash,
		State:    "downloading",
		Progress: 0.75,
		Tags:     "linux",
		Category: "iso",
	}}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.ShowSingleTorrentInfo(fullHash); err != nil {
		t.Fatalf("ShowSingleTorrentInfo err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Ubuntu ISO") {
		t.Fatalf("output missing name: %q", out)
	}
	if !strings.Contains(out, "downloading") {
		t.Fatalf("output missing state: %q", out)
	}
	if !strings.Contains(out, "linux") {
		t.Fatalf("output missing tags: %q", out)
	}
}

func TestShowSingleTorrentInfoAsJSON(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{{
		Name:  "Ubuntu ISO",
		Hash:  fullHash,
		State: "stalledUP",
	}}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.ShowSingleTorrentInfoAsJSON(fullHash); err != nil {
		t.Fatalf("ShowSingleTorrentInfoAsJSON err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"Name"`) || !strings.Contains(out, "Ubuntu ISO") {
		t.Fatalf("JSON output = %q", out)
	}
}

func TestShowAllTorrentsInfo(t *testing.T) {
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{
		{Name: "Alpha", Hash: "0123456789abcdef0123456789abcdef01234567", Progress: 0.5},
		{Name: "Beta", Hash: "fedcba9876543210fedcba9876543210fedcba98", Progress: 1.0},
	}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.ShowAllTorrentsInfo(); err != nil {
		t.Fatalf("ShowAllTorrentsInfo err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Alpha") || !strings.Contains(out, "Beta") {
		t.Fatalf("output = %q", out)
	}
	// Beta (100%) should appear before Alpha (50%) due to sorting
	betaIdx := strings.Index(out, "Beta")
	alphaIdx := strings.Index(out, "Alpha")
	if betaIdx > alphaIdx {
		t.Fatalf("expected Beta before Alpha in sorted output")
	}
}

func TestShowAllTorrentsInfoAsJSON(t *testing.T) {
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{
		{Name: "Alpha", Hash: "0123456789abcdef0123456789abcdef01234567", Progress: 0.5},
		{Name: "Beta", Hash: "fedcba9876543210fedcba9876543210fedcba98", Progress: 1.0},
	}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.ShowAllTorrentsInfoAsJSON(); err != nil {
		t.Fatalf("ShowAllTorrentsInfoAsJSON err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Alpha") || !strings.Contains(out, "Beta") {
		t.Fatalf("JSON output = %q", out)
	}
}

func TestGetField(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{{
		Name:     "Ubuntu ISO",
		Hash:     fullHash,
		Category: "linux",
	}}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.GetField(fullHash, "name"); err != nil {
		t.Fatalf("GetField err = %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "Ubuntu ISO" {
		t.Fatalf("GetField name = %q, want Ubuntu ISO", got)
	}

	buf.Reset()
	if err := app.GetField(fullHash, "category"); err != nil {
		t.Fatalf("GetField category err = %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "linux" {
		t.Fatalf("GetField category = %q, want linux", got)
	}
}

func TestGetFieldUnknown(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	app := server.newApp()

	err := app.GetField(fullHash, "nonexistent")
	if err == nil {
		t.Fatal("GetField unexpectedly succeeded for unknown field")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
		t.Fatalf("GetField err = %v, want CodedError with ExitBadArgs", err)
	}
}

func TestGetFieldJSON(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{{
		Name: "Ubuntu ISO",
		Hash: fullHash,
	}}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.GetFieldJSON(fullHash, "name"); err != nil {
		t.Fatalf("GetFieldJSON err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"name"`) || !strings.Contains(out, "Ubuntu ISO") {
		t.Fatalf("GetFieldJSON output = %q", out)
	}
}

func TestGetFieldJSONTrackerList(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.trackers = []TrackerEntry{
		{URL: "http://tracker.example.com/announce"},
	}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.GetFieldJSON(fullHash, "tracker-list"); err != nil {
		t.Fatalf("GetFieldJSON tracker-list err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "tracker-list") || !strings.Contains(out, "tracker.example.com") {
		t.Fatalf("GetFieldJSON tracker-list output = %q", out)
	}
}

func TestShowSingleTorrentInfoJSON(t *testing.T) {
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{{Name: "Ubuntu ISO", Hash: fullHash}}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.ShowSingleTorrentInfoJSON(fullHash); err != nil {
		t.Fatalf("ShowSingleTorrentInfoJSON err = %v", err)
	}
	if !strings.Contains(buf.String(), "Ubuntu ISO") {
		t.Fatalf("raw JSON output = %q", buf.String())
	}
}

func TestShowAllTorrentsInfoJSON(t *testing.T) {
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{
		{Name: "Alpha", Hash: "0123456789abcdef0123456789abcdef01234567"},
	}

	app := server.newApp()
	var buf bytes.Buffer
	app.Stdout = &buf

	if err := app.ShowAllTorrentsInfoJSON(); err != nil {
		t.Fatalf("ShowAllTorrentsInfoJSON err = %v", err)
	}
	if !strings.Contains(buf.String(), "Alpha") {
		t.Fatalf("raw JSON output = %q", buf.String())
	}
}

func TestRenderTemplateError(t *testing.T) {
	var buf bytes.Buffer
	err := renderTorrentTemplate(&buf, "bad", "{{.Invalid", nil)
	if err == nil {
		t.Fatal("renderTorrentTemplate unexpectedly succeeded with bad template")
	}
}
