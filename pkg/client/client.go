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

func (c *Client) doRawRequestContext(ctx context.Context, method, endpoint string, params url.Values) ([]byte, int, error) {
	var body io.Reader
	requestURL := c.endpointURL(endpoint)

	if method == http.MethodGet {
		if len(params) > 0 {
			requestURL += "?" + params.Encode()
		}
	} else if len(params) > 0 {
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, 0, err
	}

	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

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

func (c *Client) doBodyRequestContext(ctx context.Context, method, endpoint string, body []byte, contentType string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpointURL(endpoint), bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

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

func (c *Client) loginContext(ctx context.Context) error {
	params := url.Values{
		"username": {c.creds.User},
		"password": {c.creds.Pass},
	}

	body, status, err := c.doRawRequestContext(ctx, http.MethodPost, "/api/v2/auth/login", params)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("login failed, http status: %d", status)
	}
	if strings.EqualFold(strings.TrimSpace(string(body)), "Fails.") {
		return fmt.Errorf("login failed")
	}
	return nil
}

func (c *Client) login() error {
	return c.loginContext(context.Background())
}

func (c *Client) requestContext(ctx context.Context, method, endpoint string, params url.Values) ([]byte, error) {
	body, status, err := c.doRawRequestContext(ctx, method, endpoint, params)
	if err != nil {
		return nil, err
	}

	if status == http.StatusForbidden {
		fmt.Fprintf(c.stderr, "[INFO] Session expired. Re-authenticating...\n")
		if err := c.loginContext(ctx); err != nil {
			return nil, fmt.Errorf("re-login failed: %w", err)
		}
		body, status, err = c.doRawRequestContext(ctx, method, endpoint, params)
		if err != nil {
			return nil, err
		}
	}

	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("http error: %d", status)
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
