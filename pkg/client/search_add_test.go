package client

import (
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
)

// searchAddTransport supplies the search-result endpoints that are not part of
// the general qBittorrent fixture. All other requests go through the fixture,
// including login and app-version requests, so the test exercises one client
// session from authentication through add.
type searchAddTransport struct {
	metadataStatuses []int
	addStatus        int
	addBody          string
	onMetadata       func()

	paths         []string
	cookieHeaders map[string][]string
	metadataRaw   []string
	metadataForms []url.Values
	addForms      []url.Values
	legacyForms   []url.Values
	err           error
}

func (s *searchAddTransport) roundTrip(delegate http.RoundTripper, req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	s.paths = append(s.paths, path)
	if cookie := req.Header.Get("Cookie"); cookie != "" {
		if s.cookieHeaders == nil {
			s.cookieHeaders = make(map[string][]string)
		}
		s.cookieHeaders[path] = append(s.cookieHeaders[path], cookie)
	}

	switch path {
	case "/api/v2/torrents/fetchMetadata":
		body, err := io.ReadAll(req.Body)
		if closeErr := req.Body.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			s.err = err
			return searchAddResponse(req, http.StatusInternalServerError, ""), nil
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			s.err = err
			return searchAddResponse(req, http.StatusInternalServerError, ""), nil
		}
		s.metadataRaw = append(s.metadataRaw, string(body))
		s.metadataForms = append(s.metadataForms, form)
		if s.onMetadata != nil {
			onMetadata := s.onMetadata
			s.onMetadata = nil
			onMetadata()
		}
		status := http.StatusOK
		if len(s.metadataStatuses) > 0 {
			status = s.metadataStatuses[0]
			s.metadataStatuses = s.metadataStatuses[1:]
		}
		return searchAddResponse(req, status, ""), nil

	case "/api/v2/torrents/add":
		form, err := parseSearchAddMultipart(req)
		if err != nil {
			s.err = err
			return searchAddResponse(req, http.StatusInternalServerError, ""), nil
		}
		s.addForms = append(s.addForms, form)
		status := s.addStatus
		if status == 0 {
			status = http.StatusOK
		}
		body := s.addBody
		if body == "" {
			body = `{"success_count":1}`
		}
		return searchAddResponse(req, status, body), nil

	case "/api/v2/search/downloadTorrent":
		body, err := io.ReadAll(req.Body)
		if closeErr := req.Body.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			s.err = err
			return searchAddResponse(req, http.StatusInternalServerError, ""), nil
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			s.err = err
			return searchAddResponse(req, http.StatusInternalServerError, ""), nil
		}
		s.legacyForms = append(s.legacyForms, form)
		return searchAddResponse(req, http.StatusOK, "Ok."), nil
	}

	return delegate.RoundTrip(req)
}

func searchAddResponse(req *http.Request, status int, body string) *http.Response {
	header := make(http.Header)
	if strings.HasPrefix(body, "{") {
		header.Set("Content-Type", "application/json")
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func parseSearchAddMultipart(req *http.Request) (url.Values, error) {
	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	if mediaType != "multipart/form-data" {
		return nil, fmt.Errorf("content type = %q, want multipart/form-data", mediaType)
	}
	reader := multipart.NewReader(req.Body, params["boundary"])
	values := url.Values{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return values, nil
		}
		if err != nil {
			return nil, err
		}
		value, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return nil, err
		}
		if part.FormName() != "" {
			values.Add(part.FormName(), string(value))
		}
	}
}

func setupSearchAddTest(t *testing.T, appVersion string, metadataStatuses []int) (*App, *qbTestServer, *searchAddTransport, int64, string, string) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	server := newQBTestServer(t)
	server.AppVersion = appVersion
	app := server.newApp()
	transport := &searchAddTransport{metadataStatuses: append([]int(nil), metadataStatuses...)}
	delegate := app.client.httpClient.Transport
	app.client.httpClient.Transport = qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return transport.roundTrip(delegate, req)
	})

	if err := app.client.loginContext(context.Background()); err != nil {
		t.Fatalf("login: %v", err)
	}
	t.Cleanup(app.Logout)

	const (
		source = "https://search.example/plugin?title=literal+plus&encoded=%2B&raw=%25"
		plugin = "jackett"
	)
	results := []SearchResult{{downloadURL: source, pluginName: plugin}}
	if err := app.CacheSearchResults(results); err != nil {
		t.Fatalf("cache search result: %v", err)
	}
	if results[0].ID == 0 {
		t.Fatal("cached search result did not receive an ID")
	}
	return app, server, transport, results[0].ID, source, plugin
}

func TestAddSearchResultRechecksCachedLegacyVersion(t *testing.T) {
	app, _, transport, id, _, _ := setupSearchAddTest(t, "5.3.0", []int{http.StatusOK})
	app.appVersion, app.apiVersion = "5.0.0", "2.11.0"
	app.compatibilitySource = compatibilitySourceDisk
	if err := app.AddSearchResultWithOptions(id, AddTorrentOptions{Category: "linux"}); err != nil {
		t.Fatal(err)
	}
	if app.appVersion != "5.3.0" || len(transport.metadataForms) != 1 ||
		len(transport.addForms) != 1 || transport.addForms[0].Get("category") != "linux" {
		t.Fatalf("upgrade was not detected: version=%q requests=%v", app.appVersion, transport.paths)
	}
}

func TestAddSearchResultUsesMetadataBeforeAdd(t *testing.T) {
	app, server, transport, id, source, plugin := setupSearchAddTest(t, "5.3.0", []int{http.StatusAccepted, http.StatusOK})

	if err := app.AddSearchResult(id); err != nil {
		t.Fatalf("AddSearchResult: %v", err)
	}
	if transport.err != nil {
		t.Fatalf("mock transport: %v", transport.err)
	}
	wantPaths := []string{
		"/api/v2/auth/login",
		"/api/v2/app/version",
		"/api/v2/torrents/fetchMetadata",
		"/api/v2/torrents/fetchMetadata",
		"/api/v2/torrents/add",
	}
	if strings.Join(transport.paths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("request paths = %v, want %v", transport.paths, wantPaths)
	}
	if server.LoginCalls != 1 {
		t.Fatalf("login calls = %d, want one shared session", server.LoginCalls)
	}
	for _, path := range wantPaths[1:] {
		for _, cookie := range transport.cookieHeaders[path] {
			if !strings.Contains(cookie, "SID=test-session") {
				t.Fatalf("%s cookie = %q, want shared login session", path, cookie)
			}
		}
	}
	if len(transport.metadataForms) != 2 {
		t.Fatalf("metadata requests = %d, want pending and ready", len(transport.metadataForms))
	}
	expectedSource := strings.ReplaceAll(url.QueryEscape(source), "+", "%20")
	expectedMetadata := (url.Values{"downloader": {plugin}, "source": {expectedSource}}).Encode()
	for i, form := range transport.metadataForms {
		if got := form.Get("source"); got != expectedSource {
			t.Errorf("metadata request %d source = %q, want %q", i, got, expectedSource)
		}
		if got := form.Get("downloader"); got != plugin {
			t.Errorf("metadata request %d downloader = %q, want %q", i, got, plugin)
		}
		if transport.metadataRaw[i] != expectedMetadata {
			t.Errorf("metadata request %d body = %q, want exact %q", i, transport.metadataRaw[i], expectedMetadata)
		}
	}
	if len(transport.addForms) != 1 {
		t.Fatalf("add requests = %d, want one after metadata is ready", len(transport.addForms))
	}
	addForm := transport.addForms[0]
	if got := addForm.Get("urls"); got != source {
		t.Errorf("add urls = %q, want original URL %q", got, source)
	}
	if got := addForm.Get("downloader"); got != plugin {
		t.Errorf("add downloader = %q, want %q", got, plugin)
	}
	if len(transport.legacyForms) != 0 {
		t.Fatalf("legacy search download requests = %d, want zero", len(transport.legacyForms))
	}
}

func TestAddSearchResultWithOptionsSendsOptions(t *testing.T) {
	app, _, transport, id, _, _ := setupSearchAddTest(t, "5.3.0", []int{http.StatusOK})
	options := AddTorrentOptions{
		Category: "linux",
		SavePath: "/downloads/linux",
		Tags:     "iso,linux",
		Stopped:  true,
	}
	if err := app.AddSearchResultWithOptions(id, options); err != nil {
		t.Fatalf("AddSearchResultWithOptions: %v", err)
	}
	if transport.err != nil {
		t.Fatalf("mock transport: %v", transport.err)
	}
	if len(transport.addForms) != 1 {
		t.Fatalf("add requests = %d, want one", len(transport.addForms))
	}
	form := transport.addForms[0]
	for field, want := range map[string]string{
		"category": "linux",
		"savepath": "/downloads/linux",
		"autoTMM":  "false",
		"tags":     "iso,linux",
		"stopped":  "true",
	} {
		if got := form.Get(field); got != want {
			t.Errorf("add form %s = %q, want %q", field, got, want)
		}
	}
}

func TestAddSearchResultUsesLegacyEndpointBeforeModernVersion(t *testing.T) {
	app, server, transport, id, source, plugin := setupSearchAddTest(t, "5.1.9", nil)

	if err := app.AddSearchResult(id); err != nil {
		t.Fatalf("AddSearchResult: %v", err)
	}
	wantPaths := []string{
		"/api/v2/auth/login",
		"/api/v2/app/version",
		"/api/v2/search/downloadTorrent",
	}
	if strings.Join(transport.paths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("request paths = %v, want %v", transport.paths, wantPaths)
	}
	if len(transport.legacyForms) != 1 {
		t.Fatalf("legacy requests = %d, want one", len(transport.legacyForms))
	}
	form := transport.legacyForms[0]
	if got := form.Get("torrentUrl"); got != source {
		t.Errorf("legacy torrentUrl = %q, want %q", got, source)
	}
	if got := form.Get("pluginName"); got != plugin {
		t.Errorf("legacy pluginName = %q, want %q", got, plugin)
	}
	if len(transport.metadataForms) != 0 || len(transport.addForms) != 0 {
		t.Fatalf("modern requests = metadata %d, add %d, want zero", len(transport.metadataForms), len(transport.addForms))
	}
	if server.LoginCalls != 1 {
		t.Fatalf("login calls = %d, want one shared session", server.LoginCalls)
	}
}

func TestAddSearchResultWithOptionsRejectsLegacyEndpoint(t *testing.T) {
	app, _, transport, id, _, _ := setupSearchAddTest(t, "5.1.9", nil)
	err := app.AddSearchResultWithOptions(id, AddTorrentOptions{Category: "linux"})
	if err == nil || !strings.Contains(err.Error(), "require qBittorrent 5.2.0") {
		t.Fatalf("AddSearchResultWithOptions error = %v, want legacy-version explanation", err)
	}
	if len(transport.legacyForms) != 0 || len(transport.addForms) != 0 {
		t.Fatalf("legacy/add requests = %d/%d, want zero after unsupported options", len(transport.legacyForms), len(transport.addForms))
	}
}

func TestAddSearchResultRejectsUnsuccessfulModernAdd(t *testing.T) {
	app, _, transport, id, _, _ := setupSearchAddTest(t, "5.3.0", []int{http.StatusOK})
	transport.addStatus = http.StatusAccepted
	transport.addBody = `{"success_count":0}`

	err := app.AddSearchResult(id)
	if err == nil {
		t.Fatal("AddSearchResult unexpectedly accepted an unsuccessful add response")
	}
	if !strings.Contains(err.Error(), "did not confirm") {
		t.Fatalf("AddSearchResult error = %v, want confirmation failure", err)
	}
	if len(transport.addForms) != 1 {
		t.Fatalf("add requests = %d, want one", len(transport.addForms))
	}
	if len(transport.legacyForms) != 0 {
		t.Fatalf("legacy requests = %d, want zero", len(transport.legacyForms))
	}
}

func TestAddSearchResultCancellationDoesNotAdd(t *testing.T) {
	app, _, transport, id, _, _ := setupSearchAddTest(t, "5.3.0", []int{http.StatusAccepted})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.Ctx = ctx
	transport.onMetadata = cancel

	err := app.AddSearchResult(id)
	if err == nil {
		t.Fatal("AddSearchResult unexpectedly succeeded after metadata cancellation")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("AddSearchResult cancellation error = %v", err)
	}
	if len(transport.addForms) != 0 {
		t.Fatalf("add requests = %d, want zero after cancellation", len(transport.addForms))
	}
	if len(transport.legacyForms) != 0 {
		t.Fatalf("legacy requests = %d, want zero after cancellation", len(transport.legacyForms))
	}
}
