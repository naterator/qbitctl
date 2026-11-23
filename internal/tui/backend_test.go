//go:build !plan9

package tui

import (
	"strings"
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

func TestApplyMainDataTracksQueueChanges(t *testing.T) {
	c := &tuiClient{}
	full := []byte(`{"rid":1,"full_update":true,"torrents":{"hash":{"name":"Alpha","state":"downloading"}}}`)
	first, err := c.applyMainData(full)
	if err != nil {
		t.Fatalf("full maindata update: %v", err)
	}
	if !c.takeQueueChanged() {
		t.Fatal("full maindata update was not marked changed")
	}

	unchanged := []byte(`{"rid":2,"full_update":false,"torrents":{"hash":{"name":"Alpha"}}}`)
	second, err := c.applyMainData(unchanged)
	if err != nil {
		t.Fatalf("unchanged partial update: %v", err)
	}
	if c.takeQueueChanged() {
		t.Fatal("identical partial update was marked changed")
	}
	if len(first) == 0 || len(second) == 0 || &first[0] != &second[0] {
		t.Fatal("identical partial update rebuilt the sorted queue snapshot")
	}

	changed := []byte(`{"rid":3,"full_update":false,"torrents":{"hash":{"state":"stalledDL"}}}`)
	if _, err := c.applyMainData(changed); err != nil {
		t.Fatalf("changed partial update: %v", err)
	}
	if !c.takeQueueChanged() {
		t.Fatal("changed partial update was not marked changed")
	}

	removed := []byte(`{"rid":4,"full_update":false,"torrents_removed":["hash"]}`)
	if _, err := c.applyMainData(removed); err != nil {
		t.Fatalf("removed partial update: %v", err)
	}
	if !c.takeQueueChanged() {
		t.Fatal("removed torrent was not marked changed")
	}
	if len(c.queueState.decoded) != 0 {
		t.Fatalf("decoded queue after removal = %#v, want empty", c.queueState.decoded)
	}
}

func TestApplyMainDataMergesServerState(t *testing.T) {
	c := &tuiClient{}
	full := []byte(`{"rid":1,"full_update":true,"torrents":{},"server_state":{"dl_info_speed":1024,"up_info_speed":64,"free_space_on_disk":1048576}}`)
	if _, err := c.applyMainData(full); err != nil {
		t.Fatalf("full maindata update: %v", err)
	}
	transfer := c.serverTransferState()
	if transfer.download == nil || *transfer.download != 1024 || transfer.free == nil || *transfer.free != 1048576 {
		t.Fatalf("server state = %#v", transfer)
	}
	partial := []byte(`{"rid":2,"full_update":false,"server_state":{"dl_info_speed":2048}}`)
	if _, err := c.applyMainData(partial); err != nil {
		t.Fatalf("partial server state update: %v", err)
	}
	transfer = c.serverTransferState()
	if transfer.download == nil || *transfer.download != 2048 || transfer.free == nil || *transfer.free != 1048576 {
		t.Fatalf("merged server state = %#v", transfer)
	}
	m := newModel(t.Context(), nil)
	m.dataUnits = client.DataUnitsBytes
	m.finishWork(workDone{kind: queueWork, queueTransfer: transfer})
	for _, text := range []string{"2048 B/s", "64 B/s", "1048576 B free"} {
		if frame := m.View().Content; !strings.Contains(frame, text) {
			t.Fatalf("queue view omitted %q from sync state", text)
		}
	}
}
