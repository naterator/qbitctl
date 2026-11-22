package releaseattest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"
)

type failingRoundTripper func(*http.Request) (*http.Response, error)

func (f failingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNewVerifierReportsTUFFetchError(t *testing.T) {
	wantErr := errors.New("offline test transport")
	client := &http.Client{Transport: failingRoundTripper(func(request *http.Request) (*http.Response, error) {
		return nil, wantErr
	})}
	_, err := NewVerifier(context.Background(), client)
	if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("NewVerifier error = %v, want %q", err, wantErr)
	}
}

func TestNewVerifierUsesDefaultHTTPClient(t *testing.T) {
	wantErr := errors.New("offline default transport")
	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: failingRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, wantErr
	})}
	defer func() { http.DefaultClient = originalClient }()

	_, err := NewVerifier(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("NewVerifier error = %v, want %q", err, wantErr)
	}
}

func TestTUFHTTPClientPreservesContext(t *testing.T) {
	ctxKey := struct{}{}
	ctx := context.WithValue(context.Background(), ctxKey, "present")
	client := &http.Client{Transport: failingRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Context().Value(ctxKey) != "present" {
			return nil, errors.New("request context was not attached")
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	response, err := (tufHTTPClient{ctx: ctx, client: client}).Do(mustTestRequest(t))
	if err != nil {
		t.Fatalf("tufHTTPClient.Do: %v", err)
	}
	response.Body.Close()
}

func mustTestRequest(t *testing.T) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestVerifierAcceptsSyntheticBundle(t *testing.T) {
	fixture := newTestFixture(t)
	bundle := fixture.bundle(t, fixture.chain, fixture.tsa)
	payload, err := (&Verifier{trusted: fixture.trusted}).Verify(context.Background(), bundle)
	if err != nil {
		t.Fatalf("verify synthetic bundle: %v", err)
	}
	if string(payload) != string(fixture.payload) {
		t.Fatalf("verified payload = %q, want %q", payload, fixture.payload)
	}
}

func TestVerifierAcceptsMaximumTimestampCount(t *testing.T) {
	fixture := newTestFixture(t)
	doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
	timestamps := doc.VerificationMaterial.TimestampVerificationData.Timestamps
	for len(timestamps) < 32 {
		timestamps = append(timestamps, timestamps[0])
	}
	doc.VerificationMaterial.TimestampVerificationData.Timestamps = timestamps

	if _, err := (&Verifier{trusted: fixture.trusted}).Verify(context.Background(), encodeTestBundle(t, doc)); err != nil {
		t.Fatalf("verify bundle with maximum timestamp count: %v", err)
	}
}

func TestVerifierRejectsMalformedBundle(t *testing.T) {
	fixture := newTestFixture(t)
	verifier := &Verifier{trusted: fixture.trusted}

	tests := []struct {
		name   string
		bundle func() []byte
		want   string
	}{
		{name: "malformed JSON", bundle: func() []byte { return []byte("{") }, want: "decode Sigstore bundle"},
		{name: "wrong media type", bundle: func() []byte {
			doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
			doc.MediaType = "application/vnd.dev.sigstore.bundle.v0.2+json"
			return encodeTestBundle(t, doc)
		}, want: "expected a Sigstore release bundle"},
		{name: "wrong payload type", bundle: func() []byte {
			doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
			doc.Envelope.PayloadType = "text/plain"
			return encodeTestBundle(t, doc)
		}, want: "expected a Sigstore release bundle"},
		{name: "zero signatures", bundle: func() []byte {
			doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
			doc.Envelope.Signatures = nil
			return encodeTestBundle(t, doc)
		}, want: "expected a Sigstore release bundle"},
		{name: "two signatures", bundle: func() []byte {
			doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
			doc.Envelope.Signatures = append(doc.Envelope.Signatures, doc.Envelope.Signatures[0])
			return encodeTestBundle(t, doc)
		}, want: "expected a Sigstore release bundle"},
		{name: "no timestamps", bundle: func() []byte {
			doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
			doc.VerificationMaterial.TimestampVerificationData.Timestamps = nil
			return encodeTestBundle(t, doc)
		}, want: "invalid number of signed timestamps"},
		{name: "too many timestamps", bundle: func() []byte {
			doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
			timestamps := doc.VerificationMaterial.TimestampVerificationData.Timestamps
			for len(timestamps) < 33 {
				timestamps = append(timestamps, timestamps[0])
			}
			doc.VerificationMaterial.TimestampVerificationData.Timestamps = timestamps
			return encodeTestBundle(t, doc)
		}, want: "invalid number of signed timestamps"},
		{name: "unparseable certificate", bundle: func() []byte {
			doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
			doc.VerificationMaterial.Certificate.RawBytes = []byte("not a certificate")
			return encodeTestBundle(t, doc)
		}, want: "parse release signing certificate"},
		{name: "missing GitHub URI", bundle: func() []byte {
			cert, key := alternateTestLeaf(t, fixture.chain, "https://example.test", false)
			return fixture.bundleWithSignature(t, cert, signTestPayload(t, fixture.payload, key), fixture.tsa)
		}, want: "does not identify GitHub Releases"},
		{name: "CA certificate as leaf", bundle: func() []byte {
			cert, key := alternateTestLeaf(t, fixture.chain, "https://dotcom.releases.github.com", true)
			return fixture.bundleWithSignature(t, cert, signTestPayload(t, fixture.payload, key), fixture.tsa)
		}, want: "does not identify GitHub Releases"},
		{name: "tampered payload", bundle: func() []byte {
			doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
			payload, err := base64.StdEncoding.DecodeString(doc.Envelope.Payload)
			if err != nil {
				t.Fatalf("decode payload for mutation: %v", err)
			}
			payload[len(payload)-1] ^= 1
			doc.Envelope.Payload = base64.StdEncoding.EncodeToString(payload)
			return encodeTestBundle(t, doc)
		}, want: "verify release signature"},
		{name: "signature from another key", bundle: func() []byte {
			other := newTestCertificateChain(t, "https://dotcom.releases.github.com", false)
			return fixture.bundleWithSignature(t, fixture.chain.leafCert, signTestPayload(t, fixture.payload, other.leafKey), fixture.tsa)
		}, want: "verify release signature"},
		{name: "untrusted timestamp authority", bundle: func() []byte {
			other := newTestTimestampAuthority(t)
			return fixture.bundle(t, fixture.chain, other)
		}, want: "no valid GitHub timestamp and trust chain"},
		{name: "timestamp over different bytes", bundle: func() []byte {
			doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
			other := newTestCertificateChain(t, "https://dotcom.releases.github.com", false)
			wrongSignature := signTestPayload(t, fixture.payload, other.leafKey)
			doc.VerificationMaterial.TimestampVerificationData.Timestamps[0].SignedTimestamp = createTestTimestamp(t, fixture.tsa, wrongSignature, testTimestampTime)
			return encodeTestBundle(t, doc)
		}, want: "no valid GitHub timestamp and trust chain"},
		{name: "untrusted Fulcio CA", bundle: func() []byte {
			other := newTestCertificateChain(t, "https://dotcom.releases.github.com", false)
			return fixture.bundle(t, other, fixture.tsa)
		}, want: "no valid GitHub timestamp and trust chain"},
		{name: "timestamp outside leaf validity", bundle: func() []byte {
			return fixture.bundle(t, fixture.chain, fixture.tsa, testLeafEnd.Add(time.Hour))
		}, want: "no valid GitHub timestamp and trust chain"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := verifier.Verify(context.Background(), test.bundle())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Verify error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestVerifierAcceptsLaterValidTimestamp(t *testing.T) {
	fixture := newTestFixture(t)
	doc := decodeTestBundle(t, fixture.bundle(t, fixture.chain, fixture.tsa))
	other := newTestCertificateChain(t, "https://dotcom.releases.github.com", false)
	wrongSignature := signTestPayload(t, fixture.payload, other.leafKey)
	timestamps := doc.VerificationMaterial.TimestampVerificationData.Timestamps
	doc.VerificationMaterial.TimestampVerificationData.Timestamps = append([]testTimestampRecord{{
		SignedTimestamp: createTestTimestamp(t, fixture.tsa, wrongSignature, testTimestampTime),
	}}, timestamps...)

	payload, err := (&Verifier{trusted: fixture.trusted}).Verify(context.Background(), encodeTestBundle(t, doc))
	if err != nil {
		t.Fatalf("verify after skipping invalid timestamp: %v", err)
	}
	if string(payload) != string(fixture.payload) {
		t.Fatalf("verified payload = %q, want %q", payload, fixture.payload)
	}
}

func TestVerifierAcceptsRecordedGitHubBundle(t *testing.T) {
	bundle, err := os.ReadFile("testdata/release-1.2.1.bundle.json")
	if err != nil {
		t.Fatalf("read release bundle fixture: %v", err)
	}
	trustedJSON, err := os.ReadFile("testdata/github-trusted-root.json")
	if err != nil {
		t.Fatalf("read trusted root fixture: %v", err)
	}
	trusted, err := root.NewTrustedRootFromJSON(trustedJSON)
	if err != nil {
		t.Fatalf("parse trusted root fixture: %v", err)
	}
	payload, err := (&Verifier{trusted: trusted}).Verify(context.Background(), bundle)
	if err != nil {
		t.Fatalf("verify recorded release bundle: %v", err)
	}
	var statement struct {
		Predicate struct {
			Repository string `json:"repository"`
			Tag        string `json:"tag"`
		} `json:"predicate"`
		Subjects []struct {
			Name string `json:"name"`
		} `json:"subject"`
	}
	if err := json.Unmarshal(payload, &statement); err != nil {
		t.Fatalf("decode verified release statement: %v", err)
	}
	if statement.Predicate.Repository != "naterator/qbitctl" || statement.Predicate.Tag != "1.2.1" {
		t.Fatalf("statement predicate = %#v, want naterator/qbitctl 1.2.1", statement.Predicate)
	}
	var foundLinux bool
	for _, subject := range statement.Subjects {
		foundLinux = foundLinux || subject.Name == "qbitctl-linux-amd64"
	}
	if !foundLinux {
		t.Fatal("recorded statement did not include qbitctl-linux-amd64")
	}
}

func decodeTestBundle(t *testing.T, raw []byte) testBundle {
	t.Helper()
	var bundle testBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode test bundle: %v", err)
	}
	return bundle
}

func encodeTestBundle(t *testing.T, bundle testBundle) []byte {
	t.Helper()
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("encode test bundle: %v", err)
	}
	return raw
}
