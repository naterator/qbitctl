package client

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type qbTestServer struct {
	t                 *testing.T
	loginCalls        int
	infoCalls         int
	statusQueue       map[string][]int
	infoRawBody       string
	trackersRawBody   string
	categoriesRawBody string
	torrents          []TorrentInfo
	trackers          []TrackerEntry
	categories        map[string]map[string]any
	forms             map[string][]url.Values
	addFiles          []uploadedTorrent
	callOrder         []string
	skipSeqToggle     bool
	appVersion        string
	apiVersion        string
	transferInfo      *TransferInfo
}

type uploadedTorrent struct {
	Filename string
	Content  []byte
}

func newQBTestServer(t *testing.T) *qbTestServer {
	t.Helper()
	return &qbTestServer{
		t:           t,
		statusQueue: map[string][]int{},
		categories:  map[string]map[string]any{},
		forms:       map[string][]url.Values{},
	}
}

func (s *qbTestServer) creds() Credentials {
	return Credentials{
		URL:  "http://qbt.test",
		User: "admin",
		Pass: "secret",
	}
}

func (s *qbTestServer) newApp() *App {
	creds := s.creds()
	withQBServerClientFactory(s.t, s)
	app := &App{
		creds: creds,
	}
	newAppDefaults(app)
	app.client = newClient(creds, app.Stderr)
	return app
}

func (s *qbTestServer) nextStatus(path string) int {
	queue := s.statusQueue[path]
	if len(queue) == 0 {
		return http.StatusOK
	}
	status := queue[0]
	s.statusQueue[path] = queue[1:]
	return status
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func (s *qbTestServer) recordForm(path string, r *http.Request) {
	s.callOrder = append(s.callOrder, path)
	if err := r.ParseForm(); err != nil {
		s.t.Fatalf("ParseForm failed for %s: %v", path, err)
	}
	s.forms[path] = append(s.forms[path], cloneValues(r.Form))
}

func (s *qbTestServer) recordMultipartForm(path string, r *http.Request) {
	s.callOrder = append(s.callOrder, path)
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		s.t.Fatalf("ParseMediaType failed for %s: %v", path, err)
	}
	if mediaType != "multipart/form-data" {
		s.t.Fatalf("Content-Type for %s = %q, want multipart/form-data", path, mediaType)
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		s.t.Fatalf("ParseMultipartForm failed for %s: %v", path, err)
	}
	s.forms[path] = append(s.forms[path], cloneValues(r.MultipartForm.Value))
	for _, header := range r.MultipartForm.File["torrents"] {
		file, err := header.Open()
		if err != nil {
			s.t.Fatalf("open multipart file failed for %s: %v", path, err)
		}
		content, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			s.t.Fatalf("read multipart file failed for %s: %v", path, err)
		}
		s.addFiles = append(s.addFiles, uploadedTorrent{
			Filename: header.Filename,
			Content:  content,
		})
	}
}

func (s *qbTestServer) handle(w http.ResponseWriter, r *http.Request) {
	if status := s.nextStatus(r.URL.Path); status != http.StatusOK {
		w.WriteHeader(status)
		return
	}

	switch r.URL.Path {
	case "/api/v2/auth/login":
		s.loginCalls++
		_, _ = io.WriteString(w, "Ok.")
	case "/api/v2/torrents/info":
		s.infoCalls++
		if s.infoRawBody != "" {
			_, _ = io.WriteString(w, s.infoRawBody)
			return
		}
		hashes := r.URL.Query().Get("hashes")
		if hashes == "" {
			_ = json.NewEncoder(w).Encode(s.torrents)
			return
		}
		filtered := make([]TorrentInfo, 0, 1)
		for _, torrent := range s.torrents {
			if strings.EqualFold(torrent.Hash, hashes) {
				filtered = append(filtered, torrent)
			}
		}
		_ = json.NewEncoder(w).Encode(filtered)
	case "/api/v2/torrents/trackers":
		if s.trackersRawBody != "" {
			_, _ = io.WriteString(w, s.trackersRawBody)
			return
		}
		_ = json.NewEncoder(w).Encode(s.trackers)
	case "/api/v2/torrents/categories":
		if s.categoriesRawBody != "" {
			_, _ = io.WriteString(w, s.categoriesRawBody)
			return
		}
		_ = json.NewEncoder(w).Encode(s.categories)
	case "/api/v2/torrents/createCategory":
		s.recordForm(r.URL.Path, r)
		category := r.Form.Get("category")
		s.categories[category] = map[string]any{}
	case "/api/v2/torrents/add":
		s.recordMultipartForm(r.URL.Path, r)
	case "/api/v2/torrents/setCategory":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/setTags":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/setUploadLimit":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/setDownloadLimit":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/setShareLimits":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/setSuperSeeding":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/setAutoManagement":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/toggleSequentialDownload":
		s.recordForm(r.URL.Path, r)
		if s.skipSeqToggle {
			return
		}
		hash := r.Form.Get("hashes")
		for i := range s.torrents {
			if strings.EqualFold(s.torrents[i].Hash, hash) {
				s.torrents[i].SequentialDL = !s.torrents[i].SequentialDL
			}
		}
	case "/api/v2/torrents/stop":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/delete":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/start":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/setForceStart":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/setLocation":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/app/version":
		v := s.appVersion
		if v == "" {
			v = "v5.0.0"
		}
		_, _ = io.WriteString(w, v)
	case "/api/v2/app/webapiVersion":
		v := s.apiVersion
		if v == "" {
			v = "2.11.0"
		}
		_, _ = io.WriteString(w, v)
	case "/api/v2/transfer/info":
		info := s.transferInfo
		if info == nil {
			info = &TransferInfo{
				DLInfoSpeed:      1024000,
				UPInfoSpeed:      512000,
				DLInfoData:       1073741824,
				UPInfoData:       536870912,
				ConnectionStatus: "connected",
				DHTNodes:         42,
			}
		}
		_ = json.NewEncoder(w).Encode(info)
	default:
		s.t.Fatalf("unexpected path: %s", r.URL.Path)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type responseRecorder struct {
	header http.Header
	body   strings.Builder
	code   int
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{
		header: make(http.Header),
		code:   http.StatusOK,
	}
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	return r.body.Write(data)
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.code = statusCode
}

func (r *responseRecorder) Result() *http.Response {
	return &http.Response{
		StatusCode: r.code,
		Header:     r.header.Clone(),
		Body:       io.NopCloser(strings.NewReader(r.body.String())),
	}
}

func (s *qbTestServer) roundTrip(req *http.Request) *http.Response {
	rec := newResponseRecorder()
	s.handle(rec, req)
	return rec.Result()
}

func withQBServerClientFactory(t *testing.T, s *qbTestServer) {
	t.Helper()
	prevFactory := httpClientFactory
	httpClientFactory = func(jar http.CookieJar) *http.Client {
		return &http.Client{
			Jar:       jar,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) { return s.roundTrip(req), nil }),
		}
	}
	t.Cleanup(func() {
		httpClientFactory = prevFactory
	})
}
