package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const (
	loginEndpoint  = "/api/v2/auth/login"
	logoutEndpoint = "/api/v2/auth/logout"
	logoutTimeout  = 2 * time.Second
)

type httpStatusError int

func (err httpStatusError) Error() string {
	return fmt.Sprintf("http error: %d", int(err))
}

var httpClientFactory = func(jar http.CookieJar) *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Jar:     jar,
	}
}

func SetHTTPClientFactoryForTesting(factory func(http.CookieJar) *http.Client) func() {
	prevFactory := httpClientFactory
	httpClientFactory = factory
	return func() {
		httpClientFactory = prevFactory
	}
}

func newClient(creds Credentials, stderr io.Writer) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		creds:      creds,
		httpClient: httpClientFactory(jar),
		stderr:     stderr,
	}
}

func (c *Client) endpointURL(endpoint string) string {
	return strings.TrimRight(c.creds.URL, "/") + endpoint
}

func (c *Client) doRawRequestContext(ctx context.Context, method, requestURL string, params url.Values) ([]byte, *http.Response, error) {
	var body io.Reader

	if method == http.MethodGet {
		if len(params) > 0 {
			requestURL += "?" + params.Encode()
		}
	} else if len(params) > 0 {
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, nil, err
	}

	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	setOriginHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	return payload, resp, err
}

func (c *Client) doBodyRequestContext(ctx context.Context, method, endpoint string, body []byte, contentType string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpointURL(endpoint), bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	setOriginHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return payload, resp.StatusCode, nil
}

func setOriginHeaders(req *http.Request) {
	origin := req.URL.Scheme + "://" + req.URL.Host
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", origin+"/")
}

func (c *Client) loginContext(ctx context.Context) error {
	params := url.Values{
		"username": {c.creds.User},
		"password": {c.creds.Pass},
	}

	body, resp, err := c.doRawRequestContext(ctx, http.MethodPost, c.endpointURL(loginEndpoint), params)
	// Use the server reached by login so redirects can still be cleaned up,
	// even if reading the login response body was interrupted.
	if resp != nil && resp.Request != nil && strings.HasSuffix(resp.Request.URL.Path, loginEndpoint) {
		logoutURL := resp.Request.URL.ResolveReference(&url.URL{Path: "logout"})
		if c.hasSessionCookie(logoutURL) {
			c.logoutURL.Store(logoutURL)
		}
	}
	if err != nil {
		return err
	}
	status := resp.StatusCode
	// qBittorrent 5.2+ returns 204 with a session cookie; older releases
	// return 200 with "Ok." (or "Fails." for invalid credentials).
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("login failed, http status: %d", status)
	}
	if strings.EqualFold(strings.TrimSpace(string(body)), "Fails.") {
		return fmt.Errorf("login failed")
	}
	return nil
}

func (c *Client) hasSessionCookie(endpoint *url.URL) bool {
	for _, cookie := range c.httpClient.Jar.Cookies(endpoint) {
		if cookie.Value != "" && (cookie.Name == "SID" || strings.HasPrefix(cookie.Name, "QBT_SID_")) {
			return true
		}
	}
	return false
}

func (c *Client) logout() {
	endpoint := c.logoutURL.Load()
	if endpoint == nil {
		var err error
		endpoint, err = url.Parse(c.endpointURL(logoutEndpoint))
		if err != nil {
			return
		}
	}
	// The jar starts empty and belongs to this client. Without a session
	// cookie there is nothing to release.
	if !c.hasSessionCookie(endpoint) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
	defer cancel()
	_, _, _ = c.doRawRequestContext(ctx, http.MethodPost, endpoint.String(), nil)
}

func (c *Client) requestContext(ctx context.Context, method, endpoint string, params url.Values) ([]byte, error) {
	requestURL := c.endpointURL(endpoint)
	body, resp, err := c.doRawRequestContext(ctx, method, requestURL, params)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusForbidden {
		fmt.Fprintf(c.stderr, "[INFO] Session expired. Re-authenticating...\n")
		if err := c.loginContext(ctx); err != nil {
			return nil, fmt.Errorf("re-login failed: %w", err)
		}
		body, resp, err = c.doRawRequestContext(ctx, method, requestURL, params)
		if err != nil {
			return nil, err
		}
	}

	if status := resp.StatusCode; status < 200 || status >= 300 {
		return nil, httpStatusError(status)
	}

	return body, nil
}

func (c *Client) requestBodyContext(ctx context.Context, method, endpoint string, body []byte, contentType string) ([]byte, error) {
	payload, status, err := c.doBodyRequestContext(ctx, method, endpoint, body, contentType)
	if err != nil {
		return nil, err
	}

	if status == http.StatusForbidden {
		fmt.Fprintf(c.stderr, "[INFO] Session expired. Re-authenticating...\n")
		if err := c.loginContext(ctx); err != nil {
			return nil, fmt.Errorf("re-login failed: %w", err)
		}
		payload, status, err = c.doBodyRequestContext(ctx, method, endpoint, body, contentType)
		if err != nil {
			return nil, err
		}
	}

	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("http error: %d", status)
	}

	return payload, nil
}
