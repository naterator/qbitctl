package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/naterator/qbitctl/internal/configfile"
)

// qBittorrent can take substantially longer to serialize an unfiltered queue
// than it does to answer a small action request. Keep those budgets separate
// while retaining one user-facing override.
const (
	defaultHTTPTimeout        = 15 * time.Second
	defaultHTTPListTimeout    = 60 * time.Second
	defaultHTTPUploadTimeout  = 2 * time.Minute
	defaultHTTPLoginTimeout   = 15 * time.Second
	defaultHTTPVersionTimeout = 15 * time.Second
)

const httpTimeoutConfigKey = "http_timeout"

type httpTimeouts struct {
	request time.Duration
	list    time.Duration
	upload  time.Duration
	login   time.Duration
	version time.Duration
}

func defaultHTTPTimeouts() httpTimeouts {
	return httpTimeouts{
		request: defaultHTTPTimeout,
		list:    defaultHTTPListTimeout,
		upload:  defaultHTTPUploadTimeout,
		login:   defaultHTTPLoginTimeout,
		version: defaultHTTPVersionTimeout,
	}
}

// httpTimeoutsFor returns operation-specific defaults or applies one explicit
// user override to every qBittorrent request. The override is deliberately
// applied to login/version requests too: it is an explicit request to wait
// longer, whereas the short login/version budgets are only defaults.
func httpTimeoutsFor(override time.Duration) (httpTimeouts, error) {
	if override < 0 {
		return httpTimeouts{}, fmt.Errorf("http timeout must not be negative")
	}
	if override == 0 {
		return defaultHTTPTimeouts(), nil
	}
	return httpTimeouts{
		request: override,
		list:    override,
		upload:  override,
		login:   override,
		version: override,
	}, nil
}

// contextWithHTTPTimeout layers a request budget on the caller's context.
// context.WithTimeout preserves cancellation and any earlier parent deadline.
func contextWithHTTPTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (t httpTimeouts) requestTimeout(requestURL string, params url.Values) time.Duration {
	parsed, err := url.Parse(requestURL)
	if err == nil {
		path := parsed.Path
		switch {
		case strings.HasSuffix(path, "/api/v2/app/version"),
			strings.HasSuffix(path, "/api/v2/app/webapiVersion"):
			return t.version
		case strings.HasSuffix(path, "/api/v2/torrents/info") && strings.TrimSpace(params.Get("hashes")) == "":
			return t.list
		}
	}
	return t.request
}

// ReadHTTPTimeout reads the optional root-level http_timeout preference. A
// missing file or key means no override and therefore operation defaults.
func ReadHTTPTimeout(path string) (time.Duration, error) {
	if strings.TrimSpace(path) == "" {
		return 0, errors.New("config path is empty")
	}
	object, err := configfile.Read(path)
	if err != nil {
		return 0, err
	}
	value, ok := configValue(object, httpTimeoutConfigKey)
	if !ok {
		return 0, nil
	}
	var raw string
	if err := json.Unmarshal(value, &raw); err != nil {
		return 0, fmt.Errorf("decode config field %q: %w", httpTimeoutConfigKey, err)
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || timeout < 0 {
		if err == nil {
			err = errors.New("duration must not be negative")
		}
		return 0, fmt.Errorf("decode config field %q: invalid duration %q: %w", httpTimeoutConfigKey, raw, err)
	}
	return timeout, nil
}

// SetHTTPTimeout atomically updates the optional root-level http_timeout
// preference while preserving credentials and unrelated config fields. A zero
// duration clears the override and restores operation-specific defaults.
func SetHTTPTimeout(path string, timeout time.Duration) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	if timeout < 0 {
		return errors.New("http timeout must not be negative")
	}
	encoded, err := json.Marshal(timeout.String())
	if err != nil {
		return fmt.Errorf("encode config field %q: %w", httpTimeoutConfigKey, err)
	}
	return configfile.Update(path, func(object configfile.Object) (bool, error) {
		setConfigValue(object, httpTimeoutConfigKey, encoded)
		return true, nil
	})
}
