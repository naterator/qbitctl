package client

import "testing"

func TestMatchTorrentStateUsesStableVocabulary(t *testing.T) {
	completion := int64(123)
	tests := []struct {
		name   string
		row    TorrentInfo
		filter string
		want   bool
	}{
		{"all", TorrentInfo{State: "error"}, "all", true},
		{"active", TorrentInfo{State: "downloading", DLSpeed: 1}, "active", true},
		{"inactive", TorrentInfo{State: "stoppedDL"}, "inactive", true},
		{"downloading", TorrentInfo{State: "stalledDL"}, "downloading", true},
		{"seeding alias", TorrentInfo{State: "stalledUP"}, "uploading", true},
		{"completed by progress", TorrentInfo{State: "pausedDL", Progress: 1}, "completed", true},
		{"completed by timestamp", TorrentInfo{State: "pausedDL", CompletionOn: &completion}, "completed", true},
		{"queued", TorrentInfo{State: "queuedUP"}, "queued", true},
		{"checking", TorrentInfo{State: "checkingResumeData"}, "checking", true},
		{"checking resume data", TorrentInfo{State: "checkingResumeData"}, "checkingresumedata", true},
		{"stalled uploading", TorrentInfo{State: "stalledUP"}, "stalled-uploading", true},
		{"stalled downloading", TorrentInfo{State: "stalledDL"}, "stalled_downloading", true},
		{"error", TorrentInfo{State: "missingFiles"}, "errored", true},
		{"moving", TorrentInfo{State: "moving"}, "moving", true},
		{"forced download", TorrentInfo{State: "forcedDL"}, "forceddl", true},
		{"running excludes paused", TorrentInfo{State: "pausedUP", UpSpeed: 10}, "running", false},
		{"downloading excludes paused", TorrentInfo{State: "pausedDL"}, "downloading", false},
		{"unknown filter", TorrentInfo{State: "downloading"}, "not-a-state", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchTorrentState(tt.row, tt.filter); got != tt.want {
				t.Fatalf("MatchTorrentState(%q, %q) = %t, want %t", tt.row.State, tt.filter, got, tt.want)
			}
		})
	}
}

func TestNormalizeTorrentStateFilterAliases(t *testing.T) {
	for input, want := range map[string]string{
		"": "", "any": "", "paused": "stopped", "errored": "error",
		"uploading": "seeding", "stalled_uploading": "stalled-uploading",
		"stalled-downloading": "stalled-downloading", "CHECKING": "checking",
	} {
		if got := NormalizeTorrentStateFilter(input); got != want {
			t.Errorf("NormalizeTorrentStateFilter(%q) = %q, want %q", input, got, want)
		}
	}
}
