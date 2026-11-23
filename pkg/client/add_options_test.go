package client

import "testing"

func TestNormalizeAddTorrentOptions(t *testing.T) {
	ratio := -1.0
	seedMinutes := int64(-2)
	options, err := NormalizeAddTorrentOptions(AddTorrentOptions{
		Category:              " linux ",
		ContentLayout:         "no-subfolder",
		StopCondition:         "files",
		ShareRatioLimit:       &ratio,
		ShareSeedingTimeLimit: &seedMinutes,
		ShareLimitsMode:       "match-any",
		ShareLimitAction:      "remove-with-content",
	})
	if err != nil {
		t.Fatalf("NormalizeAddTorrentOptions returned error: %v", err)
	}
	if options.Category != "linux" || options.ContentLayout != "NoSubfolder" || options.StopCondition != "FilesChecked" {
		t.Fatalf("normalized options = %#v", options)
	}
	if options.ShareLimitsMode != "MatchAny" || options.ShareLimitAction != "RemoveWithContent" || *options.ShareRatioLimit != -1 || *options.ShareSeedingTimeLimit != -2 {
		t.Fatalf("normalized share options = %#v", options)
	}
}

func TestNormalizeAddTorrentOptionsRejectsInvalidValues(t *testing.T) {
	for name, options := range map[string]AddTorrentOptions{
		"layout":       {ContentLayout: "nested"},
		"stop":         {StopCondition: "ratio"},
		"ratio":        {ShareRatioLimit: func() *float64 { value := -3.0; return &value }()},
		"seed time":    {ShareSeedingTimeLimit: func() *int64 { value := int64(-3); return &value }()},
		"share mode":   {ShareLimitsMode: "sometimes"},
		"share action": {ShareLimitAction: "explode"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAddTorrentOptions(options); err == nil {
				t.Fatal("ValidateAddTorrentOptions unexpectedly succeeded")
			}
		})
	}
}
