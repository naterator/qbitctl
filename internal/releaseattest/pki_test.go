package releaseattest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/digitorus/timestamp"
	"github.com/secure-systems-lab/go-securesystemslib/dsse"
	"github.com/sigstore/sigstore-go/pkg/root"
)

const (
	testPayloadType = "application/vnd.in-toto+json"
	testMediaType   = "application/vnd.dev.sigstore.bundle.v0.3+json"
)

var (
	testNotBefore     = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testLeafEnd       = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	testNotAfter      = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	testTimestampTime = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
)

type testCertificateChain struct {
	rootCert         *x509.Certificate
	rootKey          *ecdsa.PrivateKey
	intermediateCert *x509.Certificate
	intermediateKey  *ecdsa.PrivateKey
	leafCert         *x509.Certificate
	leafKey          *ecdsa.PrivateKey
}

type testTimestampAuthority struct {
	rootCert *x509.Certificate
	rootKey  *ecdsa.PrivateKey
	leafCert *x509.Certificate
	leafKey  *ecdsa.PrivateKey
}

type testFixture struct {
	chain   *testCertificateChain
	tsa     *testTimestampAuthority
	trusted root.TrustedMaterial
	payload []byte
}

type testBundle struct {
	MediaType            string                   `json:"mediaType"`
	Envelope             dsse.Envelope            `json:"dsseEnvelope"`
	VerificationMaterial testVerificationMaterial `json:"verificationMaterial"`
}

type testVerificationMaterial struct {
	Certificate               testCertificate   `json:"certificate"`
	TimestampVerificationData testTimestampData `json:"timestampVerificationData"`
}

type testCertificate struct {
	RawBytes []byte `json:"rawBytes"`
}

type testTimestampData struct {
	Timestamps []testTimestampRecord `json:"rfc3161Timestamps"`
}

type testTimestampRecord struct {
	SignedTimestamp []byte `json:"signedTimestamp"`
}

func newTestFixture(t *testing.T) *testFixture {
	t.Helper()
	chain := newTestCertificateChain(t, "https://dotcom.releases.github.com", false)
	tsa := newTestTimestampAuthority(t)
	trusted, err := root.NewTrustedRoot(
		root.TrustedRootMediaType01,
		[]root.CertificateAuthority{&root.FulcioCertificateAuthority{
			Root:          chain.rootCert,
			Intermediates: []*x509.Certificate{chain.intermediateCert},
			URI:           "https://fulcio.test",
		}},
		nil,
		[]root.TimestampingAuthority{&root.SigstoreTimestampingAuthority{
			Root: tsa.rootCert,
			Leaf: tsa.leafCert,
			URI:  "https://tsa.test",
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("create test trusted root: %v", err)
	}
	return &testFixture{
		chain:   chain,
		tsa:     tsa,
		trusted: trusted,
		payload: []byte(`{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://in-toto.io/attestation/release/v0.2","predicate":{"repository":"naterator/qbitctl","tag":"1.2.1"},"subject":[{"name":"qbitctl-linux-amd64","digest":{"sha256":"a2967026e231fa8c4f5e8fde2d775711e5c66ece47636704488709fdfd7bf1d6"}}]}`),
	}
}

func newTestCertificateChain(t *testing.T, identity string, isCA bool) *testCertificateChain {
	t.Helper()
	rootKey := newTestKey(t)
	rootCert := createTestCertificate(t, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Fulcio Root"},
		NotBefore:             testNotBefore,
		NotAfter:              testNotAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            2,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}, nil, rootKey, rootKey.Public())
	intermediateKey := newTestKey(t)
	intermediateCert := createTestCertificate(t, &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test Fulcio Intermediate"},
		NotBefore:             testNotBefore,
		NotAfter:              testNotAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}, rootCert, rootKey, intermediateKey.Public())
	leafKey := newTestKey(t)
	identityURL, err := url.Parse(identity)
	if err != nil {
		t.Fatalf("parse identity URL: %v", err)
	}
	leafCert := createTestCertificate(t, &x509.Certificate{
		SerialNumber:          big.NewInt(3),
		Subject:               pkix.Name{CommonName: "Test GitHub Release Signer"},
		NotBefore:             testNotBefore,
		NotAfter:              testLeafEnd,
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		URIs:                  []*url.URL{identityURL},
	}, intermediateCert, intermediateKey, leafKey.Public())
	return &testCertificateChain{
		rootCert: rootCert, rootKey: rootKey,
		intermediateCert: intermediateCert, intermediateKey: intermediateKey,
		leafCert: leafCert, leafKey: leafKey,
	}
}

func newTestTimestampAuthority(t *testing.T) *testTimestampAuthority {
	t.Helper()
	rootKey := newTestKey(t)
	rootCert := createTestCertificate(t, &x509.Certificate{
		SerialNumber:          big.NewInt(11),
		Subject:               pkix.Name{CommonName: "Test TSA Root"},
		NotBefore:             testNotBefore,
		NotAfter:              testNotAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}, nil, rootKey, rootKey.Public())
	leafKey := newTestKey(t)
	timestampExt, err := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}})
	if err != nil {
		t.Fatalf("marshal timestamp EKU: %v", err)
	}
	leafCert := createTestCertificate(t, &x509.Certificate{
		SerialNumber:          big.NewInt(12),
		Subject:               pkix.Name{CommonName: "Test TSA Leaf"},
		NotBefore:             testNotBefore,
		NotAfter:              testNotAfter,
		IsCA:                  false,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
		ExtraExtensions: []pkix.Extension{{
			Id:       asn1.ObjectIdentifier{2, 5, 29, 37},
			Critical: true,
			Value:    timestampExt,
		}},
	}, rootCert, rootKey, leafKey.Public())
	return &testTimestampAuthority{rootCert: rootCert, rootKey: rootKey, leafCert: leafCert, leafKey: leafKey}
}

func newTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return key
}

func createTestCertificate(t *testing.T, template, parent *x509.Certificate, parentKey crypto.Signer, publicKey crypto.PublicKey) *x509.Certificate {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, template, parentOrSelf(template, parent), publicKey, parentKeyOrSelf(template, parentKey))
	if err != nil {
		t.Fatalf("create test certificate %q: %v", template.Subject.CommonName, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse test certificate %q: %v", template.Subject.CommonName, err)
	}
	return cert
}

func parentOrSelf(template, parent *x509.Certificate) *x509.Certificate {
	if parent != nil {
		return parent
	}
	return template
}

func parentKeyOrSelf(template *x509.Certificate, parentKey crypto.Signer) crypto.Signer {
	if parentKey != nil {
		return parentKey
	}
	return nil
}

func (f *testFixture) bundle(t *testing.T, chain *testCertificateChain, tsa *testTimestampAuthority, timestampTimes ...time.Time) []byte {
	t.Helper()
	signature := signTestPayload(t, f.payload, chain.leafKey)
	return f.bundleWithSignature(t, chain.leafCert, signature, tsa, timestampTimes...)
}

func signTestPayload(t *testing.T, payload []byte, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	pae := dsse.PAE(testPayloadType, payload)
	digest := sha256.Sum256(pae)
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign test envelope: %v", err)
	}
	return signature
}

func alternateTestLeaf(t *testing.T, chain *testCertificateChain, identity string, isCA bool) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key := newTestKey(t)
	identityURL, err := url.Parse(identity)
	if err != nil {
		t.Fatalf("parse alternate identity URL: %v", err)
	}
	cert := createTestCertificate(t, &x509.Certificate{
		SerialNumber:          big.NewInt(30),
		Subject:               pkix.Name{CommonName: "Alternate Test Release Signer"},
		NotBefore:             testNotBefore,
		NotAfter:              testLeafEnd,
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		URIs:                  []*url.URL{identityURL},
	}, chain.intermediateCert, chain.intermediateKey, key.Public())
	return cert, key
}

func (f *testFixture) bundleWithSignature(t *testing.T, cert *x509.Certificate, signature []byte, tsa *testTimestampAuthority, timestampTimes ...time.Time) []byte {
	t.Helper()
	if len(timestampTimes) == 0 {
		timestampTimes = []time.Time{testTimestampTime}
	}
	timestamps := make([]testTimestampRecord, 0, len(timestampTimes))
	for _, timestampTime := range timestampTimes {
		timestamps = append(timestamps, testTimestampRecord{SignedTimestamp: createTestTimestamp(t, tsa, signature, timestampTime)})
	}
	bundle := testBundle{
		MediaType: testMediaType,
		Envelope: dsse.Envelope{
			PayloadType: testPayloadType,
			Payload:     base64.StdEncoding.EncodeToString(f.payload),
			Signatures:  []dsse.Signature{{Sig: base64.StdEncoding.EncodeToString(signature)}},
		},
		VerificationMaterial: testVerificationMaterial{
			Certificate:               testCertificate{RawBytes: cert.Raw},
			TimestampVerificationData: testTimestampData{Timestamps: timestamps},
		},
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal test bundle: %v", err)
	}
	return raw
}

func createTestTimestamp(t *testing.T, tsa *testTimestampAuthority, signature []byte, timestampTime time.Time) []byte {
	t.Helper()
	requestBytes, err := timestamp.CreateRequest(bytesReader(signature), &timestamp.RequestOptions{Hash: crypto.SHA256})
	if err != nil {
		t.Fatalf("create timestamp request: %v", err)
	}
	request, err := timestamp.ParseRequest(requestBytes)
	if err != nil {
		t.Fatalf("parse timestamp request: %v", err)
	}
	response, err := (&timestamp.Timestamp{
		HashAlgorithm:     request.HashAlgorithm,
		HashedMessage:     request.HashedMessage,
		Time:              timestampTime,
		Policy:            asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 2},
		ExtraExtensions:   request.Extensions,
		AddTSACertificate: true,
		Certificates:      []*x509.Certificate{tsa.rootCert},
	}).CreateResponseWithOpts(tsa.leafCert, tsa.leafKey, crypto.SHA256)
	if err != nil {
		t.Fatalf("create timestamp response: %v", err)
	}
	return response
}

// bytesReader avoids importing bytes in every test file that uses this helper.
func bytesReader(value []byte) *testByteReader { return &testByteReader{value: value} }

type testByteReader struct {
	value  []byte
	offset int
}

func (r *testByteReader) Read(p []byte) (int, error) {
	if r.offset == len(r.value) {
		return 0, io.EOF
	}
	n := copy(p, r.value[r.offset:])
	r.offset += n
	return n, nil
}
