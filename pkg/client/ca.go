package client

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
)

const caFileConfigKey = "ca_file"

func normalizeSavedCAFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve CA file %q: %w", path, err)
	}
	return absolute, nil
}

// ReadCAFile reads the optional root-level PEM CA bundle path. An absent or
// empty setting leaves the normal system trust store in use.
func ReadCAFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("config path is empty")
	}
	object, err := configfile.Read(path)
	if err != nil {
		return "", err
	}
	value, ok := configValue(object, caFileConfigKey)
	if !ok {
		return "", nil
	}
	var configured string
	if err := json.Unmarshal(value, &configured); err != nil {
		return "", fmt.Errorf("decode config field %q: %w", caFileConfigKey, err)
	}
	return strings.TrimSpace(configured), nil
}

// caFileForConnection resolves a saved CA bundle for the selected profile.
// When the caller explicitly changes the server origin, a bundle saved for
// the old endpoint must not silently follow that override. The CA field is
// intentionally read only after that origin check so an explicit override can
// also recover from a malformed stale saved value.
func caFileForConnection(path, profile, overrideURL string) (string, error) {
	root, err := configfile.Read(path)
	if err != nil {
		return "", err
	}
	_, selected, _, err := selectProfileObject(root, profile)
	if err != nil {
		// Setup may be creating a new named profile. It has no saved trust
		// setting yet; ordinary authenticated profile selection has already
		// rejected unknown names before this helper is called.
		if strings.TrimSpace(profile) != "" && strings.Contains(err.Error(), "is not configured") {
			return "", nil
		}
		return "", err
	}
	if strings.TrimSpace(overrideURL) != "" {
		savedURL, urlErr := profileString(selected, "url")
		if urlErr != nil {
			return "", urlErr
		}
		if strings.TrimSpace(savedURL) != "" && !samePreferenceOrigin(savedURL, overrideURL) {
			return "", nil
		}
	}
	caFile, err := profileString(selected, caFileConfigKey)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(caFile), nil
}

// SetCAFile atomically changes the optional root-level PEM CA bundle path
// while preserving credentials and unrelated configuration. An empty path
// clears the setting.
func SetCAFile(path, caFile string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	var err error
	caFile, err = normalizeSavedCAFile(caFile)
	if err != nil {
		return err
	}
	return configfile.Update(path, func(object configfile.Object) (bool, error) {
		if caFile == "" {
			for existing := range object {
				if strings.EqualFold(existing, caFileConfigKey) {
					delete(object, existing)
				}
			}
			return true, nil
		}
		encoded, err := json.Marshal(caFile)
		if err != nil {
			return false, fmt.Errorf("encode config field %q: %w", caFileConfigKey, err)
		}
		setConfigValue(object, caFileConfigKey, encoded)
		return true, nil
	})
}

// loadCAPool validates a PEM CA bundle and extends the host's system roots.
// It intentionally does not replace the system pool: a configured private CA
// should add trust for qBittorrent without removing ordinary public roots.
func loadCAPool(path string) (*x509.CertPool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CA file %q: %w", path, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if ok := pool.AppendCertsFromPEM(data); !ok {
		return nil, fmt.Errorf("CA file %q does not contain a valid PEM certificate", path)
	}
	return pool, nil
}

// configureHTTPClientCA returns a client with a private transport clone whose
// trust roots include the configured bundle. The caller's and package-global
// transports are never mutated. Unsupported custom transports fail closed
// rather than silently ignoring an explicit trust setting.
func configureHTTPClientCA(httpClient *http.Client, caFile string) (*http.Client, error) {
	caFile = strings.TrimSpace(caFile)
	if caFile == "" {
		return httpClient, nil
	}
	pool, err := loadCAPool(caFile)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		return nil, errors.New("http client is nil")
	}

	var transport *http.Transport
	switch configured := httpClient.Transport.(type) {
	case nil:
		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("cannot configure CA bundle on the default HTTP transport")
		}
		transport = base.Clone()
	case *http.Transport:
		transport = configured.Clone()
	default:
		return nil, errors.New("cannot configure CA bundle on a custom HTTP transport")
	}

	tlsConfig := transport.TLSClientConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	} else {
		tlsConfig = tlsConfig.Clone()
	}
	if tlsConfig.InsecureSkipVerify {
		return nil, errors.New("cannot use CA bundle with insecure TLS transport")
	}
	// Never enable InsecureSkipVerify here. The default is false and hostname
	// verification remains active for private roots.
	tlsConfig.RootCAs = pool
	transport.TLSClientConfig = tlsConfig

	configuredClient := *httpClient
	configuredClient.Transport = transport
	return &configuredClient, nil
}
