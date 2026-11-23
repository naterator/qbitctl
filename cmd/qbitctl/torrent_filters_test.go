package main

import "testing"

func TestListTorrentFilterMapsStateAliases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: ""},
		{input: "any", want: ""},
		{input: "paused", want: "stopped"},
		{input: "uploading", want: "seeding"},
		{input: "stalled-uploading", want: "stalled_uploading"},
		{input: "stalled-downloading", want: "stalled_downloading"},
		{input: "errored", want: "errored"},
		{input: "checking", want: "checking"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			filter, err := listTorrentFilter(tt.input, "category", "tag")
			if err != nil {
				t.Fatalf("listTorrentFilter(%q) error = %v", tt.input, err)
			}
			if filter.State != tt.want || filter.Category != "category" || filter.Tag != "tag" {
				t.Fatalf("listTorrentFilter(%q) = %#v, want state %q with category/tag", tt.input, filter, tt.want)
			}
		})
	}
}

func TestListTorrentFilterRejectsUnknownState(t *testing.T) {
	if _, err := listTorrentFilter("not-a-state", "", ""); err == nil {
		t.Fatal("listTorrentFilter unexpectedly accepted unknown state")
	}
}

func TestListTorrentFilterAcceptsSharedStateVocabulary(t *testing.T) {
	for _, state := range []string{"active", "downloading", "completed", "inactive", "running", "stalled", "queued", "checking", "moving", "forceddl", "checkingresumedata"} {
		if _, err := listTorrentFilter(state, "", ""); err != nil {
			t.Errorf("listTorrentFilter(%q) returned error: %v", state, err)
		}
	}
}
