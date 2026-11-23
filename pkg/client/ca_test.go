package client

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/naterator/qbitctl/internal/configfile"
)

type caTestServer struct {
	server *httptest.Server
	caPEM  []byte
}

func newCATestServer(t *testing.T, names []string) *caTestServer {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "qbitctl test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "qbitctl test server"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     names,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			serverTemplate.IPAddresses = append(serverTemplate.IPAddresses, ip)
		}
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)}),
	)
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case loginEndpoint:
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-session", Path: "/", Secure: true})
			_, _ = io.WriteString(w, "Ok.")
		case logoutEndpoint:
		case "/api/v2/app/version":
			_, _ = io.WriteString(w, "v5.3.0")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.16.2")
		default:
			http.NotFound(w, r)
		}
	}))
	server.Listener = listener
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return &caTestServer{server: server, caPEM: caPEM}
}

func writeTestCAPEM(t *testing.T, fixture *caTestServer, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, fixture.caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCAFileConfigRoundTripPreservesUnrelatedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"future":{"keep":true},"url":"https://example.test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetCAFile(path, "private-ca.pem"); err != nil {
		t.Fatal(err)
	}
	expectedPath, err := filepath.Abs("private-ca.pem")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ReadCAFile(path); err != nil || got != expectedPath {
		t.Fatalf("ReadCAFile = %q, %v; want %q", got, err, expectedPath)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	var future map[string]bool
	if err := json.Unmarshal(object["future"], &future); err != nil || !future["keep"] {
		t.Fatalf("unrelated field changed: %s", object["future"])
	}
	if err := SetCAFile(path, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadCAFile(path); err != nil || got != "" {
		t.Fatalf("cleared ReadCAFile = %q, %v", got, err)
	}
}

func TestSavedCAIsNotUsedForUnrelatedURLOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"url":"https://saved.example/qbt","ca_file":"/tmp/saved-ca.pem"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := caFileForConnection(path, "", "https://other.example/qbt"); err != nil || got != "" {
		t.Fatalf("unrelated URL CA selection = %q, %v; want empty", got, err)
	}
	if got, err := caFileForConnection(path, "", "https://saved.example/qbt"); err != nil || got != "/tmp/saved-ca.pem" {
		t.Fatalf("same-base CA selection = %q, %v; want saved bundle", got, err)
	}
	if got, err := caFileForConnection(path, "", "https://saved.example/other"); err != nil || got != "" {
		t.Fatalf("different proxy path CA selection = %q, %v; want empty", got, err)
	}
}

func TestNamedProfileCADoesNotFallbackToRootValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"profiles":{"default":{"url":"https://saved.example"}},"default_profile":"default","ca_file":"/tmp/legacy-ca.pem"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := caFileForConnection(path, "default", "https://saved.example"); err != nil || got != "" {
		t.Fatalf("named profile CA fallback = %q, %v; want empty", got, err)
	}
}

func TestNewNamedProfileHasNoSavedCARequirement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"profiles":{"default":{"url":"https://saved.example"}},"default_profile":"default"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := caFileForConnection(path, "lab", "https://new.example"); err != nil || got != "" {
		t.Fatalf("new profile CA selection = %q, %v; want empty", got, err)
	}
}

func TestLoadCAPoolRejectsDirectoryAndMalformedPEM(t *testing.T) {
	if _, err := loadCAPool(t.TempDir()); err == nil {
		t.Fatal("directory CA path unexpectedly succeeded")
	}
	path := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCAPool(path); err == nil || !strings.Contains(err.Error(), "valid PEM certificate") {
		t.Fatalf("malformed PEM error = %v", err)
	}
}

func TestConfigureHTTPClientCARejectsInsecureTransport(t *testing.T) {
	fixture := newCATestServer(t, []string{"127.0.0.1"})
	caPath := writeTestCAPEM(t, fixture, "ca.pem")
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec -- the production code must reject this setting.
	if _, err := configureHTTPClientCA(client, caPath); err == nil || !strings.Contains(err.Error(), "insecure TLS") {
		t.Fatalf("insecure transport error = %v", err)
	}
}

func TestPrivateCAAuthenticatesBeforeCompatibilityAndPreservesHostnameChecks(t *testing.T) {
	fixture := newCATestServer(t, []string{"127.0.0.1"})
	caPath := writeTestCAPEM(t, fixture, "ca.pem")
	creds := Credentials{URL: fixture.server.URL, User: "admin", Pass: "secret"}
	client, err := newClientWithTimeoutAndCA(creds, io.Discard, 0, caPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.loginContext(context.Background()); err != nil {
		t.Fatalf("private CA login failed: %v", err)
	}
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport == http.DefaultTransport {
		t.Fatal("private CA client did not receive a transport clone")
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatal("private CA roots were not configured")
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("private CA enabled insecure hostname verification")
	}

	withoutCA, err := newClientWithTimeout(creds, io.Discard, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := withoutCA.loginContext(context.Background()); err == nil {
		t.Fatal("TLS login without private CA unexpectedly succeeded")
	}

	wrongName := newCATestServer(t, []string{"wrong.example"})
	wrongCAPath := writeTestCAPEM(t, wrongName, "ca.pem")
	wrong, err := newClientWithTimeoutAndCA(Credentials{URL: wrongName.server.URL, User: "admin", Pass: "secret"}, io.Discard, 0, wrongCAPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrong.loginContext(context.Background()); err == nil {
		t.Fatal("hostname mismatch unexpectedly succeeded with trusted CA")
	}
}

func TestNewClientUsesSavedCAAndExplicitOverride(t *testing.T) {
	withMockMasterKey(t)
	fixture := newCATestServer(t, []string{"127.0.0.1"})
	caPath := writeTestCAPEM(t, fixture, "saved-ca.pem")
	overridePath := writeTestCAPEM(t, fixture, "override-ca.pem")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveAuthFile(path, Credentials{URL: fixture.server.URL, User: "admin", Pass: "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := SetCAFile(path, caPath); err != nil {
		t.Fatal(err)
	}

	app, err := NewClientWithOutput(context.Background(), &Options{ConfigPath: path}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("saved CA login failed: %v", err)
	}
	if app.CAFile != caPath || app.ConnectionOptions().CAFile != caPath {
		t.Fatalf("saved CA metadata = %q / %q, want %q", app.CAFile, app.ConnectionOptions().CAFile, caPath)
	}
	app.Logout()
	if err := configfile.Update(path, func(object configfile.Object) (bool, error) {
		object[caFileConfigKey] = json.RawMessage(`123`)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	app, err = NewClientWithOutput(context.Background(), &Options{ConfigPath: path, CAFile: overridePath}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("explicit CA login failed: %v", err)
	}
	if app.CAFile != overridePath {
		t.Fatalf("explicit CA metadata = %q, want %q", app.CAFile, overridePath)
	}
	app.Logout()
	object, err := configfile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(object[caFileConfigKey]) != `123` {
		t.Fatalf("explicit CA changed saved preference to %s", object[caFileConfigKey])
	}
}

func TestExecuteConfigWritePersistsValidatedCA(t *testing.T) {
	withMockMasterKey(t)
	fixture := newCATestServer(t, []string{"127.0.0.1"})
	caPath := writeTestCAPEM(t, fixture, "ca.pem")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := ExecuteConfigWrite(Options{URL: fixture.server.URL, Pass: "secret", CAFile: caPath}, path, io.Discard); err != nil {
		t.Fatalf("ExecuteConfigWrite failed: %v", err)
	}
	if got, err := ReadCAFile(path); err != nil || got != caPath {
		t.Fatalf("saved CA = %q, %v", got, err)
	}
	if err := ExecuteConfigWrite(Options{URL: fixture.server.URL, Pass: "secret", CAFile: filepath.Join(t.TempDir(), "missing.pem")}, filepath.Join(t.TempDir(), "bad.json"), io.Discard); err == nil {
		t.Fatal("missing CA file unexpectedly saved")
	}
}
