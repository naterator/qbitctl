package client

import (
	"bytes"
	"context"
	"errors"
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

var httpClientFactory = func(jar http.CookieJar) *http.Client {
	return &http.Client{
		// Per-request contexts below provide operation-specific budgets. A
		// whole-client timeout would cap large queue reads and uploads before
		// those contexts have a chance to take effect.
		Timeout: 0,
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
	client, err := newClientWithTimeout(creds, stderr, 0)
	if err != nil {
		// The zero-value default is constant and validated here, so this can
		// only indicate an internal programming error. Keep the historic helper
		// signature for package tests and callers.
		panic(err)
	}
	return client
}

func newClientWithTimeout(creds Credentials, stderr io.Writer, override time.Duration) (*Client, error) {
	timeouts, err := httpTimeoutsFor(override)
	if err != nil {
		return nil, err
	}
	jar, _ := cookiejar.New(nil)
	httpClient := httpClientFactory(jar)
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	// A testing factory may return a client with the old whole-request cap.
	// Remove only that cap while preserving its transport and redirect policy.
	httpClient.Timeout = 0
	// Test transports and callers may return a client without carrying over
	// the jar supplied to the factory.  A client without a jar cannot retain
	// the server-issued session cookie between requests.
	if httpClient.Jar == nil {
		httpClient.Jar = jar
	}
	return &Client{
		creds:        creds,
		httpClient:   httpClient,
		stderr:       stderr,
		httpTimeouts: timeouts,
	}, nil
}

func (c *Client) endpointURL(endpoint string) string {
	return strings.TrimRight(c.creds.URL, "/") + endpoint
}

func (c *Client) doRawRequestContext(ctx context.Context, method, requestURL string, params url.Values) ([]byte, *http.Response, error) {
	ctx, cancel := contextWithHTTPTimeout(ctx, c.httpTimeouts.requestTimeout(requestURL, params))
	defer cancel()
	return c.doRawRequestContextWithClient(ctx, method, requestURL, params, c.httpClient)
}

func (c *Client) doRawRequestContextWithoutCookies(ctx context.Context, method, requestURL string, params url.Values) ([]byte, *http.Response, error) {
	if c.httpClient == nil {
		return nil, nil, fmt.Errorf("http client is nil")
	}
	client := *c.httpClient
	client.Jar = nil
	ctx, cancel := contextWithHTTPTimeout(ctx, c.httpTimeouts.requestTimeout(requestURL, params))
	defer cancel()
	return c.doRawRequestContextWithClient(ctx, method, requestURL, params, &client)
}

func (c *Client) doRawRequestContextWithClient(ctx context.Context, method, requestURL string, params url.Values, client *http.Client) ([]byte, *http.Response, error) {
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

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	payload, err := readResponseBody(resp)
	return payload, resp, err
}

func (c *Client) doBodyRequestContext(ctx context.Context, method, endpoint string, body []byte, contentType string) ([]byte, *http.Response, error) {
	ctx, cancel := contextWithHTTPTimeout(ctx, c.httpTimeouts.upload)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.endpointURL(endpoint), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	setOriginHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	payload, err := readResponseBody(resp)
	if err != nil {
		return nil, resp, err
	}

	return payload, resp, nil
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	reader := io.Reader(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		reader = io.LimitReader(resp.Body, maxHTTPErrorBodyBytes)
	}
	return io.ReadAll(reader)
}

func setOriginHeaders(req *http.Request) {
	origin := req.URL.Scheme + "://" + req.URL.Host
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", origin+"/")
}

func (c *Client) loginContext(ctx context.Context) error {
	ctx, cancel := contextWithHTTPTimeout(ctx, c.httpTimeouts.login)
	defer cancel()
	params := url.Values{
		"username": {c.creds.User},
		"password": {c.creds.Pass},
	}

	if c.httpClient == nil {
		return fmt.Errorf("login failed: http client is nil")
	}
	loginClient := c.loginHTTPClient()
	body, resp, err := c.doRawRequestContextWithClient(ctx, http.MethodPost, c.endpointURL(loginEndpoint), params, loginClient)
	// A response's headers (including Set-Cookie) are processed before its
	// body. Preserve a session received with a successful login even when the
	// body cannot be read, so the caller's deferred cleanup still works.
	if err != nil {
		if resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if session := c.sessionFromResponse(resp); session != nil {
				c.session.Store(session)
			}
		}
		return err
	}
	if resp == nil {
		return fmt.Errorf("login failed: empty response")
	}
	status := resp.StatusCode
	trimmedBody := strings.TrimSpace(string(body))
	// qBittorrent 5.2+ returns 204 with an empty body and a session cookie;
	// older releases return 200 with "Ok." (or "Fails." for invalid
	// credentials).  Do not accept an arbitrary 200 response from a proxy as
	// a successful login.
	switch status {
	case http.StatusOK:
		if strings.EqualFold(trimmedBody, "Fails.") {
			return fmt.Errorf("login failed")
		}
		if !strings.EqualFold(trimmedBody, "Ok.") {
			return fmt.Errorf("login failed: unexpected response body")
		}
	case http.StatusNoContent:
		if trimmedBody != "" {
			return fmt.Errorf("login failed: unexpected response body")
		}
	default:
		message := sanitizeLoginHTTPBody(resp, body, c.creds.Pass)
		if message == "" {
			return fmt.Errorf("login failed, http status: %d", status)
		}
		return fmt.Errorf("login failed, http status: %d (%s)", status, message)
	}

	// Use the server reached by login so redirects can still be cleaned up.
	// A cookie is considered valid only when it was issued by this response
	// and is present in the jar for the corresponding logout endpoint.  This
	// avoids mistaking a stale cookie or an unrelated proxy cookie for a new
	// authenticated session.
	if session := c.sessionFromResponse(resp); session != nil {
		c.session.Store(session)
		return nil
	}

	// qBittorrent can be configured to bypass WebUI authentication for a
	// trusted client.  Such a deployment may accept the login request without
	// issuing a cookie.  Treat that as success only after a protected API call
	// succeeds; otherwise a login page or reverse proxy returning a misleading
	// 200/204 would be accepted as authenticated.
	if err := c.verifyAuthBypass(ctx, resp, loginClient); err != nil {
		return fmt.Errorf("login failed: server did not issue a session cookie: %w", err)
	}
	c.session.Store(nil)
	return nil
}

// loginHTTPClient keeps the configured transport, cookie jar, timeout, and
// redirect policy while binding the login flow to its original origin.  A
// 307/308 redirect preserves the POST body, so allowing such a redirect to a
// different origin would disclose credentials to the redirect target.
func (c *Client) loginHTTPClient() *http.Client {
	client := *c.httpClient
	configuredCheckRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && !sameCredentialOrigin(via[len(via)-1].URL.String(), req.URL.String()) {
			return errors.New("refusing cross-origin login redirect")
		}
		if configuredCheckRedirect != nil {
			return configuredCheckRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &client
}

func (c *Client) logout() {
	session := c.session.Load()
	if session == nil {
		return
	}
	// The jar starts empty and belongs to this client. Without a session
	// cookie there is nothing to release.
	if !c.hasSessionCookies(session.logoutURL, session.cookies) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
	defer cancel()
	_, _, _ = c.doRawRequestContext(ctx, http.MethodPost, session.logoutURL.String(), nil)
}

func (c *Client) hasSessionCookies(endpoint *url.URL, want []sessionCookie) bool {
	if endpoint == nil || c.httpClient == nil || c.httpClient.Jar == nil || len(want) == 0 {
		return false
	}
	allowed := make(map[sessionCookie]struct{}, len(want))
	for _, cookie := range want {
		allowed[cookie] = struct{}{}
	}
	for _, cookie := range c.httpClient.Jar.Cookies(endpoint) {
		if _, ok := allowed[sessionCookie{name: cookie.Name, value: cookie.Value}]; ok {
			return true
		}
	}
	return false
}

func (c *Client) logoutURLForResponse(resp *http.Response) *url.URL {
	if resp != nil && resp.Request != nil && resp.Request.URL != nil && strings.HasSuffix(resp.Request.URL.Path, loginEndpoint) {
		logoutURL := resp.Request.URL.ResolveReference(&url.URL{Path: "logout"})
		return logoutURL
	}
	logoutURL, err := url.Parse(c.endpointURL(logoutEndpoint))
	if err != nil {
		return nil
	}
	return logoutURL
}

func (c *Client) sessionFromResponse(resp *http.Response) *clientSession {
	if resp == nil {
		return nil
	}
	logoutURL := c.logoutURLForResponse(resp)
	var cookies []sessionCookie
	for _, cookie := range resp.Cookies() {
		if cookie.Value != "" && cookie.MaxAge >= 0 {
			cookies = append(cookies, sessionCookie{name: cookie.Name, value: cookie.Value})
		}
	}
	if logoutURL == nil || !c.hasSessionCookies(logoutURL, cookies) {
		return nil
	}
	return &clientSession{logoutURL: logoutURL, cookies: cookies}
}

func (c *Client) verifyAuthBypass(ctx context.Context, loginResp *http.Response, loginClient *http.Client) error {
	verifyURL := c.endpointURL("/api/v2/app/version")
	if loginResp != nil && loginResp.Request != nil && loginResp.Request.URL != nil {
		loginURL := loginResp.Request.URL
		if strings.HasSuffix(loginURL.Path, loginEndpoint) {
			verifyURL = loginURL.ResolveReference(&url.URL{Path: "../app/version"}).String()
		}
	}
	client := *loginClient
	client.Jar = nil
	body, resp, err := c.doRawRequestContextWithClient(ctx, http.MethodGet, verifyURL, nil, &client)
	if err != nil {
		return err
	}
	if resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp == nil {
			return fmt.Errorf("verification returned no response")
		}
		return fmt.Errorf("verification returned http status %d", resp.StatusCode)
	}
	version := strings.TrimSpace(string(body))
	if !isDottedVersion(version) {
		return fmt.Errorf("verification returned an invalid qBittorrent version")
	}

	return nil
}

func (c *Client) requestContext(ctx context.Context, method, endpoint string, params url.Values) ([]byte, error) {
	body, _, err := c.requestWithStatusContext(ctx, method, endpoint, params)
	return body, err
}

func (c *Client) requestWithStatusContext(ctx context.Context, method, endpoint string, params url.Values) ([]byte, int, error) {
	requestURL := c.endpointURL(endpoint)
	body, resp, err := c.doRawRequestContext(ctx, method, requestURL, params)
	if err != nil {
		return nil, 0, err
	}

	if resp.StatusCode == http.StatusForbidden {
		fmt.Fprintf(c.stderr, "[INFO] Session expired. Re-authenticating...\n")
		if err := c.loginContext(ctx); err != nil {
			return nil, 0, fmt.Errorf("re-login failed: %w", err)
		}
		body, resp, err = c.doRawRequestContext(ctx, method, requestURL, params)
		if err != nil {
			return nil, 0, err
		}
	}

	if status := resp.StatusCode; status < 200 || status >= 300 {
		return nil, status, c.requestError(resp, body)
	}

	return body, resp.StatusCode, nil
}

func (c *Client) requestBodyContext(ctx context.Context, method, endpoint string, body []byte, contentType string) ([]byte, error) {
	return c.requestWithBodyContext(ctx, func() ([]byte, *http.Response, error) {
		return c.doBodyRequestContext(ctx, method, endpoint, body, contentType)
	})
}

func (c *Client) requestWithBodyContext(ctx context.Context, send func() ([]byte, *http.Response, error)) ([]byte, error) {
	payload, resp, err := send()
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusForbidden {
		fmt.Fprintf(c.stderr, "[INFO] Session expired. Re-authenticating...\n")
		if err := c.loginContext(ctx); err != nil {
			return nil, fmt.Errorf("re-login failed: %w", err)
		}
		payload, resp, err = send()
		if err != nil {
			return nil, err
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.requestError(resp, payload)
	}

	return payload, nil
}

func (c *Client) requestError(resp *http.Response, body []byte) error {
	if c.invalidateCachedCompatibility != nil {
		switch resp.StatusCode {
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
			c.invalidateCachedCompatibility()
		}
	}
	return newHTTPError(resp, body)
}
