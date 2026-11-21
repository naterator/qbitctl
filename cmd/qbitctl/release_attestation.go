package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/naterator/qbitctl/internal/releaseattest"
)

const (
	githubReleaseRepository = "naterator/qbitctl"
	githubReleaseAPI        = "https://api.github.com/repos/" + githubReleaseRepository
	releasePredicate        = "https://in-toto.io/attestation/release/v0.2"
)

func (u *githubReleaseUpdater) verifyReleaseAttestation(ctx context.Context, tag, asset, checksum string) error {
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := u.getReleaseJSON(ctx, githubReleaseAPI+"/git/ref/tags/"+url.PathEscape(tag), &ref); err != nil {
		return fmt.Errorf("resolve release tag %q: %w", tag, err)
	}
	if ref.Ref != "refs/tags/"+tag {
		return fmt.Errorf("GitHub returned a different ref for release %q", tag)
	}
	digest, err := hex.DecodeString(ref.Object.SHA)
	if err != nil {
		return fmt.Errorf("invalid digest for release tag %q: %w", tag, err)
	}
	var algorithm string
	switch len(digest) {
	case 20:
		algorithm = "sha1"
	case 32:
		algorithm = "sha256"
	default:
		return fmt.Errorf("unexpected digest length for release tag %q", tag)
	}

	var listing struct {
		Attestations []struct {
			Initiator string          `json:"initiator"`
			Bundle    json.RawMessage `json:"bundle"`
		} `json:"attestations"`
	}
	query := "?predicate_type=release&per_page=100"
	refChecksum := hex.EncodeToString(digest)
	attestationsURL := githubReleaseAPI + "/attestations/" + algorithm + ":" + refChecksum + query
	if err := u.getReleaseJSON(ctx, attestationsURL, &listing); err != nil {
		return fmt.Errorf("fetch release attestations for %q: %w", tag, err)
	}
	var verifier *releaseattest.Verifier
	var lastErr error
	for _, attestation := range listing.Attestations {
		if attestation.Initiator != "github" || len(attestation.Bundle) == 0 || string(attestation.Bundle) == "null" {
			continue
		}
		if verifier == nil {
			verifier, err = releaseattest.NewVerifier(ctx, u.client)
			if err != nil {
				return fmt.Errorf("load GitHub release trust roots: %w", err)
			}
		}
		var payload []byte
		payload, lastErr = verifier.Verify(ctx, attestation.Bundle)
		if lastErr != nil {
			continue
		}
		lastErr = matchReleaseAsset(payload, tag, algorithm, refChecksum, asset, checksum)
		if lastErr == nil {
			return nil
		}
	}
	if lastErr != nil {
		return fmt.Errorf("no valid attestation for %q in release %q: %w", asset, tag, lastErr)
	}
	return fmt.Errorf("no GitHub release attestation found for %q. Publish an immutable release with its assets attached", tag)
}

func (u *githubReleaseUpdater) getReleaseJSON(ctx context.Context, endpoint string, output any) error {
	response, err := u.get(ctx, endpoint)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(output)
}

// matchReleaseAsset binds the verified statement to the requested release, its
// Git ref, and the exact downloaded asset.
func matchReleaseAsset(payload []byte, tag, refAlgorithm, refChecksum, asset, checksum string) error {
	var statement struct {
		Type          string `json:"_type"`
		PredicateType string `json:"predicateType"`
		Predicate     struct {
			Repository string `json:"repository"`
			Tag        string `json:"tag"`
		} `json:"predicate"`
		Subjects []struct {
			Name   string            `json:"name"`
			Digest map[string]string `json:"digest"`
		} `json:"subject"`
	}
	if err := json.Unmarshal(payload, &statement); err != nil {
		return fmt.Errorf("decode signed release statement: %w", err)
	}
	if statement.Type != "https://in-toto.io/Statement/v1" || statement.PredicateType != releasePredicate {
		return errors.New("signed statement is not a GitHub release attestation")
	}
	if statement.Predicate.Repository != githubReleaseRepository || statement.Predicate.Tag != tag {
		return errors.New("signed statement names a different repository or release tag")
	}
	var refMatches, assetMatches bool
	for _, subject := range statement.Subjects {
		refMatches = refMatches || strings.EqualFold(subject.Digest[refAlgorithm], refChecksum)
		assetMatches = assetMatches || (subject.Name == asset && strings.EqualFold(subject.Digest["sha256"], checksum))
	}
	if !refMatches {
		return errors.New("signed statement does not match the release tag object digest")
	}
	if !assetMatches {
		return errors.New("signed statement does not match the downloaded asset name and SHA-256")
	}
	return nil
}
