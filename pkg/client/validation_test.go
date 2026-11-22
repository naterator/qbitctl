package client

import "testing"

func TestValidateTorrentTemplate(t *testing.T) {
	if err := ValidateTorrentTemplate(`{{.Name}} {{field "hash" .}}`); err != nil {
		t.Fatalf("valid template returned error: %v", err)
	}
	if err := ValidateTorrentTemplate(`{{.Name`); err == nil {
		t.Fatal("invalid template unexpectedly succeeded")
	}
}

func TestIsTorrentField(t *testing.T) {
	for _, field := range []string{"name", "status", "trackers", "tracker-list"} {
		if !IsGetField(field) {
			t.Errorf("IsGetField(%q) = false", field)
		}
	}
	if IsGetField("nmae") {
		t.Error("IsGetField accepted unknown field")
	}
	for _, field := range []string{"up-limit", "upload-limit", "auto-tmm", "tags"} {
		if !IsSetField(field) {
			t.Errorf("IsSetField(%q) = false", field)
		}
	}
	if IsSetField("name") {
		t.Error("IsSetField accepted read-only field")
	}
}

func TestValidateSetValue(t *testing.T) {
	valid := map[string]string{
		"category":       "movies",
		"tags":           "4k,hevc",
		"up-limit":       "512k",
		"dl-limit":       "2048",
		"ratio-limit":    "2.0",
		"seedtime-limit": "3d",
		"seqdl":          "true",
		"autotmm":        "false",
		"superseed":      "true",
	}
	for field, value := range valid {
		if err := ValidateSetValue(field, value); err != nil {
			t.Errorf("ValidateSetValue(%q, %q) returned error: %v", field, value, err)
		}
	}

	invalid := map[string]string{
		"up-limit":       "12q",
		"ratio-limit":    "-1",
		"seedtime-limit": "30s",
		"seqdl":          "maybe",
		"name":           "value",
	}
	for field, value := range invalid {
		if err := ValidateSetValue(field, value); err == nil {
			t.Errorf("ValidateSetValue(%q, %q) unexpectedly succeeded", field, value)
		}
	}
}
