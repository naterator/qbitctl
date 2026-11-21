package main

import (
	"strings"
	"testing"
)

func TestMatchReleaseAsset(t *testing.T) {
	const tag = "1.1.1"
	const refChecksum = "fc9ce0b555e83d8a13a9d07f98c941b0bdbfa541"
	const asset = "qbitctl-windows-amd64.exe"
	const checksum = "daf9b03513cc183616ab5c8deaa54f97f1549a7f1d95f62ec917d1219583d369"
	statement := `{
		"_type": "https://in-toto.io/Statement/v1",
		"predicateType": "https://in-toto.io/attestation/release/v0.2",
		"predicate": {"tag": "1.1.1", "repository": "naterator/qbitctl"},
		"subject": [
			{"uri": "pkg:github/naterator/qbitctl@1.1.1", "digest": {"sha1": "` + refChecksum + `"}},
			{"name": "qbitctl-windows-amd64.exe", "digest": {"sha256": "` + checksum + `"}}
		]
	}`
	if err := matchReleaseAsset([]byte(statement), tag, "sha1", refChecksum, asset, checksum); err != nil {
		t.Fatalf("matching signed statement: %v", err)
	}

	for _, test := range []struct {
		name string
		old  string
		new  string
	}{
		{name: "repository", old: `"repository": "naterator/qbitctl"`, new: `"repository": "another/qbitctl"`},
		{name: "exact tag", old: `"tag": "1.1.1"`, new: `"tag": "v1.1.1"`},
		{name: "tag digest", old: refChecksum, new: strings.Repeat("0", 40)},
		{name: "predicate type", old: releasePredicate, new: "https://slsa.dev/provenance/v1"},
		{name: "asset name", old: asset, new: "qbitctl-darwin-arm64"},
		{name: "asset digest", old: checksum, new: strings.Repeat("0", 64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := strings.Replace(statement, test.old, test.new, 1)
			if err := matchReleaseAsset([]byte(other), tag, "sha1", refChecksum, asset, checksum); err == nil {
				t.Fatal("accepted an attestation for the wrong release asset")
			}
		})
	}
}
