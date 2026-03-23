package client

import "testing"

func TestParseLimit(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		ok    bool
	}{
		{input: "512", want: 512, ok: true},
		{input: "512k", want: 512 * 1024, ok: true},
		{input: "-1", want: -1, ok: true},
		{input: "", want: -1, ok: true},
		{input: "12m", ok: false},
	}

	for _, tt := range tests {
		got, err := parseLimit(tt.input)
		if tt.ok && err != nil {
			t.Fatalf("parseLimit(%q) returned error: %v", tt.input, err)
		}
		if !tt.ok && err == nil {
			t.Fatalf("parseLimit(%q) unexpectedly succeeded", tt.input)
		}
		if tt.ok && got != tt.want {
			t.Fatalf("parseLimit(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseSeedtimeLimit(t *testing.T) {
	got, err := parseSeedtimeLimit("3600")
	if err != nil {
		t.Fatalf("parseSeedtimeLimit returned error: %v", err)
	}
	if got != 3600 {
		t.Fatalf("parseSeedtimeLimit = %d, want 3600", got)
	}

	if _, err := parseSeedtimeLimit("abc"); err == nil {
		t.Fatal("parseSeedtimeLimit unexpectedly accepted non-numeric input")
	}
}

func TestValidateHash(t *testing.T) {
	if !validateHash("0123456789abcdef0123456789abcdef01234567") {
		t.Fatal("expected valid hash")
	}
	if validateHash("not-a-hash") {
		t.Fatal("expected invalid hash")
	}
}

func TestTrackerListOutput(t *testing.T) {
	trackers := []TrackerEntry{
		{URL: "http://Tracker.Example.com:8080/announce"},
		{URL: "http://tracker.example.com/other"},
		{URL: "udp://tracker.two.example.com:80/announce"},
		{URL: "** [DHT] **"},
	}

	got := trackerListOutput(trackers)
	want := "http://Tracker.Example.com:8080/announce,http://tracker.example.com/other,udp://tracker.two.example.com:80/announce"
	if got != want {
		t.Fatalf("trackerListOutput = %q, want %q", got, want)
	}
}

func TestParseToggleValueAndTrackerNormalization(t *testing.T) {
	if got, err := parseToggleValue("true"); err != nil || !got {
		t.Fatalf("parseToggleValue true = %v, %v", got, err)
	}
	if got, err := parseToggleValue("false"); err != nil || got {
		t.Fatalf("parseToggleValue false = %v, %v", got, err)
	}
	if _, err := parseToggleValue("1"); err == nil {
		t.Fatal("parseToggleValue unexpectedly accepted '1'")
	}

	if !looksLikeOpaqueCiphertext("0123456789abcdef0123456789abcdef01234567") {
		t.Fatal("looksLikeOpaqueCiphertext expected true")
	}
}

func TestLooksLikeTorrentSourceURL(t *testing.T) {
	tests := map[string]bool{
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567": true,
		"https://example.com/test.torrent":                             true,
		"http://example.com/test.torrent":                              true,
		"ftp://example.com/test.torrent":                               true,
		"./test.torrent":                                               false,
		"/tmp/test.torrent":                                            false,
	}

	for input, want := range tests {
		if got := looksLikeTorrentSourceURL(input); got != want {
			t.Fatalf("looksLikeTorrentSourceURL(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestFormatProgress(t *testing.T) {
	if got := formatProgress(0.9999); got != "0.999900" {
		t.Fatalf("formatProgress = %q, want 0.999900", got)
	}
}
