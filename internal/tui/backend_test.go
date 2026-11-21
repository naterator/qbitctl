//go:build !plan9

package tui

import (
	"testing"

	"github.com/naterator/qbitctl/pkg/client"
)

func TestExpandTrackerRows(t *testing.T) {
	rows := expandTrackerRows([]detailRow{{label: "tracker-list", value: "https://one.example/announce,\nhttps://two.example/announce\n"}})
	if len(rows) != 2 {
		t.Fatalf("expandTrackerRows returned %d rows, want 2", len(rows))
	}
	if rows[0].label != "Tracker 1" || rows[0].value != "https://one.example/announce" {
		t.Fatalf("first tracker row = %#v", rows[0])
	}
	if rows[1].label != "Tracker 2" || rows[1].value != "https://two.example/announce" {
		t.Fatalf("second tracker row = %#v", rows[1])
	}
}

func TestExpandTrackerEntriesPreservesCommasInURLs(t *testing.T) {
	rows := expandTrackerEntries([]client.TrackerEntry{
		{URL: "https://tracker.example/announce?token=a,b"},
		{URL: "** [DHT] **"},
		{URL: "udp://tracker.example:6969"},
	})
	if len(rows) != 2 {
		t.Fatalf("expandTrackerEntries returned %d rows, want 2", len(rows))
	}
	if rows[0].value != "https://tracker.example/announce?token=a,b" {
		t.Fatalf("first tracker URL = %q, want comma-preserving URL", rows[0].value)
	}
	if rows[1].value != "udp://tracker.example:6969" {
		t.Fatalf("second tracker URL = %q", rows[1].value)
	}
}

func TestOrderRows(t *testing.T) {
	rows := orderRows([]detailRow{
		{label: "State", value: "downloading"},
		{label: "Name", value: "Ubuntu"},
		{label: "Future", value: "value"},
	}, detailRowOrder)
	if len(rows) != 3 {
		t.Fatalf("orderRows returned %d rows, want 3", len(rows))
	}
	if rows[0].label != "Name" || rows[1].label != "State" || rows[2].label != "Future" {
		t.Fatalf("ordered labels = %#v", []string{rows[0].label, rows[1].label, rows[2].label})
	}
}

func TestJoinDiagnostics(t *testing.T) {
	if got := joinDiagnostics("  first\n", "", " second "); got != "first\nsecond" {
		t.Fatalf("joinDiagnostics = %q", got)
	}
}
