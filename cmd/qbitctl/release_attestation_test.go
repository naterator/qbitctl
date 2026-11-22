package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
)

type fakeAttestationVerification struct {
	payload []byte
	err     error
}

func (f fakeAttestationVerification) Verify(context.Context, []byte) ([]byte, error) {
	return f.payload, f.err
}

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

func TestVerifyReleaseAttestationUsesInjectedVerifierAndAPIBase(t *testing.T) {
	const tag = "v1.2.3+meta"
	const refChecksum = "0123456789012345678901234567890123456789"
	const asset = "qbitctl-linux-amd64"
	const checksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	statement := releaseStatementPayload(tag, "sha1", refChecksum, asset, checksum)

	var refRequests, attestationRequests int
	var requestedRefURL string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case releaseTestPath(r) == "/git/ref/tags/v1.2.3+meta":
			refRequests++
			requestedRefURL = r.URL.String()
			_, _ = io.WriteString(w, `{"ref":"refs/tags/v1.2.3+meta","object":{"sha":"`+refChecksum+`"}}`)
		case releaseTestPath(r) == "/attestations/sha1:"+refChecksum:
			attestationRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attestations": []map[string]any{{"initiator": "github", "bundle": json.RawMessage(`{"id":"bundle"}`)}},
			})
		default:
			http.NotFound(w, r)
		}
	})

	var factoryCalls int
	updater := &githubReleaseUpdater{
		client: releaseTestHTTPClient(handler),
		newAttestationVerifier: func(context.Context, *http.Client) (attestationVerifier, error) {
			factoryCalls++
			return fakeAttestationVerification{payload: statement}, nil
		},
	}
	if err := updater.verifyReleaseAttestation(context.Background(), tag, asset, checksum); err != nil {
		t.Fatalf("verifyReleaseAttestation returned error: %v", err)
	}
	if refRequests != 1 || attestationRequests != 1 || factoryCalls != 1 {
		t.Fatalf("requests/factory = %d/%d/%d, want 1/1/1", refRequests, attestationRequests, factoryCalls)
	}
	if requestedRefURL != githubReleaseAPI+"/git/ref/tags/v1.2.3+meta" {
		t.Fatalf("requested ref URL = %q, want original GitHub URL with literal +", requestedRefURL)
	}
}

func TestVerifyReleaseAttestationUsesSHA256RefDigest(t *testing.T) {
	const tag = "1.2.3"
	const refChecksum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const asset = "qbitctl-linux-amd64"
	const checksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	statement := releaseStatementPayload(tag, "sha256", refChecksum, asset, checksum)
	var requestedPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = releaseTestPath(r)
		switch {
		case strings.HasPrefix(requestedPath, "/git/ref/tags/"):
			_, _ = io.WriteString(w, `{"ref":"refs/tags/1.2.3","object":{"sha":"`+refChecksum+`"}}`)
		case requestedPath == "/attestations/sha256:"+refChecksum:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attestations": []map[string]any{{"initiator": "github", "bundle": json.RawMessage(`{"id":"bundle"}`)}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	updater := &githubReleaseUpdater{
		client: releaseTestHTTPClient(handler),
		newAttestationVerifier: func(context.Context, *http.Client) (attestationVerifier, error) {
			return fakeAttestationVerification{payload: statement}, nil
		},
	}
	if err := updater.verifyReleaseAttestation(context.Background(), tag, asset, checksum); err != nil {
		t.Fatalf("verifyReleaseAttestation returned error: %v", err)
	}
	if requestedPath != "/attestations/sha256:"+refChecksum {
		t.Fatalf("last requested path = %q, want SHA-256 attestation path", requestedPath)
	}
}

func TestVerifyReleaseAttestationSkipsInvalidBundlesAndAcceptsLaterMatch(t *testing.T) {
	const tag = "1.2.3"
	const refChecksum = "0123456789012345678901234567890123456789"
	const asset = "qbitctl-linux-amd64"
	const checksum = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	statement := releaseStatementPayload(tag, "sha1", refChecksum, asset, checksum)
	client := newAttestationTestClient(t, tag, refChecksum, []map[string]any{
		{"initiator": "user", "bundle": json.RawMessage(`{"id":"ignored-user"}`)},
		{"initiator": "github", "bundle": json.RawMessage(`null`)},
		{"initiator": "github"},
		{"initiator": "github", "bundle": json.RawMessage(`{"id":"bad"}`)},
		{"initiator": "github", "bundle": json.RawMessage(`{"id":"good"}`)},
	})

	verifyCalls := 0
	updater := &githubReleaseUpdater{
		client: client,
		newAttestationVerifier: func(context.Context, *http.Client) (attestationVerifier, error) {
			return attestationVerifierFunc(func(_ context.Context, bundle []byte) ([]byte, error) {
				verifyCalls++
				if strings.Contains(string(bundle), `"bad"`) {
					return nil, errors.New("tampered bundle")
				}
				return statement, nil
			}), nil
		},
	}
	if err := updater.verifyReleaseAttestation(context.Background(), tag, asset, checksum); err != nil {
		t.Fatalf("verifyReleaseAttestation returned error: %v", err)
	}
	if verifyCalls != 2 {
		t.Fatalf("Verify calls = %d, want 2", verifyCalls)
	}
}

func TestVerifyReleaseAttestationErrors(t *testing.T) {
	const tag = "1.2.3"
	const refChecksum = "0123456789012345678901234567890123456789"
	const asset = "qbitctl-linux-amd64"
	const checksum = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	tests := []struct {
		name       string
		ref        string
		refSHA     string
		attest     []map[string]any
		factoryErr error
		verifyErr  error
		want       string
	}{
		{
			name:   "different ref",
			ref:    "refs/tags/other",
			refSHA: refChecksum,
			want:   "different ref",
		},
		{
			name:   "non hex digest",
			ref:    "refs/tags/" + tag,
			refSHA: "not-a-digest",
			want:   "invalid digest",
		},
		{
			name:   "wrong digest length",
			ref:    "refs/tags/" + tag,
			refSHA: "0123456789abcdef",
			want:   "unexpected digest length",
		},
		{
			name:   "empty list",
			ref:    "refs/tags/" + tag,
			refSHA: refChecksum,
			want:   "no GitHub release attestation found",
		},
		{
			name:       "factory error",
			ref:        "refs/tags/" + tag,
			refSHA:     refChecksum,
			attest:     []map[string]any{{"initiator": "github", "bundle": json.RawMessage(`{"id":"bundle"}`)}},
			factoryErr: errors.New("trust roots unavailable"),
			want:       "load GitHub release trust roots",
		},
		{
			name:      "verifier error and no match",
			ref:       "refs/tags/" + tag,
			refSHA:    refChecksum,
			attest:    []map[string]any{{"initiator": "github", "bundle": json.RawMessage(`{"id":"bundle"}`)}},
			verifyErr: errors.New("invalid signature"),
			want:      "no valid attestation",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newAttestationTestClientWithRef(t, test.ref, test.refSHA, test.attest)
			updater := &githubReleaseUpdater{client: client}
			factoryCalls := 0
			updater.newAttestationVerifier = func(context.Context, *http.Client) (attestationVerifier, error) {
				factoryCalls++
				if test.factoryErr != nil {
					return nil, test.factoryErr
				}
				return attestationVerifierFunc(func(context.Context, []byte) ([]byte, error) {
					return nil, test.verifyErr
				}), nil
			}
			err := updater.verifyReleaseAttestation(context.Background(), tag, asset, checksum)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if len(test.attest) == 0 && factoryCalls != 0 {
				t.Fatalf("factory calls = %d, want 0 for empty list", factoryCalls)
			}
		})
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	})
	updater := &githubReleaseUpdater{client: releaseTestHTTPClient(handler)}
	err := updater.verifyReleaseAttestation(context.Background(), tag, asset, checksum)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404 error = %v, want status", err)
	}
}

func TestVerifyReleaseAttestationWrapsAssetMatchErrors(t *testing.T) {
	const tag = "1.2.3"
	const refChecksum = "0123456789012345678901234567890123456789"
	const asset = "qbitctl-linux-amd64"
	const checksum = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	for _, test := range []struct {
		name        string
		payload     []byte
		wantMessage string
	}{
		{
			name:        "release digest mismatch",
			payload:     releaseStatementPayload(tag, "sha1", strings.Repeat("0", 40), asset, checksum),
			wantMessage: "signed statement does not match the release tag object digest",
		},
		{
			name:        "asset name mismatch",
			payload:     releaseStatementPayload(tag, "sha1", refChecksum, "qbitctl-darwin-arm64", checksum),
			wantMessage: "signed statement does not match the downloaded asset name and SHA-256",
		},
		{
			name:        "asset checksum mismatch",
			payload:     releaseStatementPayload(tag, "sha1", refChecksum, asset, strings.Repeat("0", 64)),
			wantMessage: "signed statement does not match the downloaded asset name and SHA-256",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newAttestationTestClient(t, tag, refChecksum, []map[string]any{
				{"initiator": "github", "bundle": json.RawMessage(`{"id":"bundle"}`)},
			})
			updater := &githubReleaseUpdater{
				client: client,
				newAttestationVerifier: func(context.Context, *http.Client) (attestationVerifier, error) {
					return fakeAttestationVerification{payload: test.payload}, nil
				},
			}
			err := updater.verifyReleaseAttestation(context.Background(), tag, asset, checksum)
			if err == nil || !strings.Contains(err.Error(), "no valid attestation") || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("error = %v, want wrapped match error %q", err, test.wantMessage)
			}
		})
	}
}

type attestationVerifierFunc func(context.Context, []byte) ([]byte, error)

func (f attestationVerifierFunc) Verify(ctx context.Context, bundle []byte) ([]byte, error) {
	return f(ctx, bundle)
}

func releaseStatementPayload(tag, algorithm, refChecksum, asset, checksum string) []byte {
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"predicateType": releasePredicate,
		"predicate": map[string]any{
			"tag":        tag,
			"repository": githubReleaseRepository,
		},
		"subject": []map[string]any{
			{"digest": map[string]string{algorithm: refChecksum}},
			{"name": asset, "digest": map[string]string{"sha256": checksum}},
		},
	}
	payload, _ := json.Marshal(statement)
	return payload
}

const releaseTestAPIBase = "https://api.github.com"

func releaseTestPath(r *http.Request) string {
	return strings.TrimPrefix(r.URL.Path, "/repos/"+githubReleaseRepository)
}

func releaseTestHTTPClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Result(), nil
	})}
}

func newAttestationTestClient(t *testing.T, tag, refSHA string, attestations []map[string]any) *http.Client {
	return newAttestationTestClientWithRef(t, "refs/tags/"+tag, refSHA, attestations)
}

func newAttestationTestClientWithRef(t *testing.T, ref, refSHA string, attestations []map[string]any) *http.Client {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := releaseTestPath(r)
		switch {
		case strings.HasPrefix(path, "/git/ref/tags/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"ref": ref, "object": map[string]string{"sha": refSHA}})
		case strings.HasPrefix(path, "/attestations/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"attestations": attestations})
		default:
			http.NotFound(w, r)
		}
	})
	return releaseTestHTTPClient(handler)
}
