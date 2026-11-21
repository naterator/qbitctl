// Package releaseattest verifies GitHub's signed release bundles using the
// GitHub TUF repository and its pinned bootstrap root.
package releaseattest

import (
	"context"
	"crypto"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/secure-systems-lab/go-securesystemslib/dsse"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore/pkg/signature"
	sigdsse "github.com/sigstore/sigstore/pkg/signature/dsse"
	"github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
)

// The v9 bootstrap root was verified through GitHub's signed TUF rotation
// from the bootstrap root shipped with GitHub CLI v2.104.0.
//
//go:embed github_root.json
var githubRoot []byte

type Verifier struct {
	trusted *root.TrustedRoot
}

func NewVerifier(ctx context.Context, client *http.Client) (*Verifier, error) {
	if client == nil {
		client = http.DefaultClient
	}
	download := fetcher.NewDefaultFetcher()
	download.SetHTTPClient(tufHTTPClient{ctx: ctx, client: client})
	options := tuf.DefaultOptions().
		WithRoot(githubRoot).
		WithRepositoryBaseURL("https://tuf-repo.github.com").
		WithDisableLocalCache().
		WithContext(ctx).
		WithFetcher(download)
	tufClient, err := tuf.New(options)
	if err != nil {
		return nil, err
	}
	trusted, err := root.GetTrustedRoot(tufClient)
	if err != nil {
		return nil, err
	}
	return &Verifier{trusted: trusted}, nil
}

// Verify authenticates a GitHub release statement using its DSSE signature
// and a signed timestamp over that signature from GitHub's trust root.
func (v *Verifier) Verify(ctx context.Context, rawBundle []byte) ([]byte, error) {
	var candidate struct {
		MediaType            string        `json:"mediaType"`
		Envelope             dsse.Envelope `json:"dsseEnvelope"`
		VerificationMaterial struct {
			Certificate struct {
				RawBytes []byte `json:"rawBytes"`
			} `json:"certificate"`
			TimestampVerificationData struct {
				Timestamps []struct {
					SignedTimestamp []byte `json:"signedTimestamp"`
				} `json:"rfc3161Timestamps"`
			} `json:"timestampVerificationData"`
		} `json:"verificationMaterial"`
	}
	if err := json.Unmarshal(rawBundle, &candidate); err != nil {
		return nil, fmt.Errorf("decode Sigstore bundle: %w", err)
	}
	if candidate.MediaType != "application/vnd.dev.sigstore.bundle.v0.3+json" ||
		candidate.Envelope.PayloadType != "application/vnd.in-toto+json" ||
		len(candidate.Envelope.Signatures) != 1 {
		return nil, errors.New("expected a Sigstore release bundle with one in-toto signature")
	}
	signedTimestamps := candidate.VerificationMaterial.TimestampVerificationData.Timestamps
	if len(signedTimestamps) == 0 || len(signedTimestamps) > 32 {
		return nil, errors.New("invalid number of signed timestamps in release bundle")
	}
	cert, err := x509.ParseCertificate(candidate.VerificationMaterial.Certificate.RawBytes)
	if err != nil {
		return nil, fmt.Errorf("parse release signing certificate: %w", err)
	}
	if cert.IsCA || !slices.ContainsFunc(cert.URIs, func(uri *url.URL) bool {
		return uri.String() == "https://dotcom.releases.github.com"
	}) {
		return nil, errors.New("signing certificate does not identify GitHub Releases")
	}

	key, err := signature.LoadVerifier(cert.PublicKey, crypto.SHA256)
	if err != nil {
		return nil, err
	}
	envelopeVerifier, err := dsse.NewEnvelopeVerifier(&sigdsse.VerifierAdapter{
		SignatureVerifier: key,
		Pub:               cert.PublicKey,
	})
	if err != nil {
		return nil, err
	}
	if _, err := envelopeVerifier.Verify(ctx, &candidate.Envelope); err != nil {
		return nil, fmt.Errorf("verify release signature: %w", err)
	}

	signatureBytes, err := base64.StdEncoding.DecodeString(candidate.Envelope.Signatures[0].Sig)
	if err != nil {
		return nil, fmt.Errorf("decode release signature: %w", err)
	}
	for _, signed := range signedTimestamps {
		for _, tsa := range v.trusted.TimestampingAuthorities() {
			timestamp, err := tsa.Verify(signed.SignedTimestamp, signatureBytes)
			if err != nil {
				continue
			}
			for _, ca := range v.trusted.FulcioCertificateAuthorities() {
				if _, err := ca.Verify(cert, timestamp.Time); err == nil {
					return candidate.Envelope.DecodeB64Payload()
				}
			}
		}
	}
	return nil, errors.New("release signing certificate has no valid GitHub timestamp and trust chain")
}

type tufHTTPClient struct {
	ctx    context.Context
	client *http.Client
}

func (c tufHTTPClient) Do(request *http.Request) (*http.Response, error) {
	return c.client.Do(request.WithContext(c.ctx))
}
