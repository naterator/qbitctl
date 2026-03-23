package client

import (
	"bytes"
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
