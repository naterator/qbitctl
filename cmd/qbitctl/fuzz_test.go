package main

import (
	"regexp"
	"testing"
)

func FuzzParseSHA256File(f *testing.F) {
	valid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, seed := range []string{valid + "  qbitctl-linux-amd64\n", valid + "\n", "not a checksum\n"} {
		f.Add(seed, "qbitctl-linux-amd64")
	}
	f.Fuzz(func(t *testing.T, content, assetName string) {
		checksum, err := parseSHA256File(content, assetName)
		if err != nil {
			return
		}
		if !regexp.MustCompile(`^[0-9a-fA-F]{64}$`).MatchString(checksum) {
			t.Fatalf("parseSHA256File returned invalid digest %q", checksum)
		}
	})
}
