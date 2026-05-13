package qbtest

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

type TestingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

type Credentials struct {
	URL  string
	User string
	Pass string
}

type UploadedTorrent struct {
	Filename string
	Content  []byte
}

type TransferInfo struct {
	DLInfoSpeed      int64  `json:"dl_info_speed"`
	UPInfoSpeed      int64  `json:"up_info_speed"`
	DLInfoData       int64  `json:"dl_info_data"`
	UPInfoData       int64  `json:"up_info_data"`
	DLRateLimit      int64  `json:"dl_rate_limit"`
	UPRateLimit      int64  `json:"up_rate_limit"`
	DHTNodes         int64  `json:"dht_nodes"`
	ConnectionStatus string `json:"connection_status"`
}

type Server struct {
	t TestingT

	LoginCalls        int
	InfoCalls         int
	StatusQueue       map[string][]int
	InfoRawBody       string
	TrackersRawBody   string
	CategoriesRawBody string
	Torrents          any
	Trackers          any
	Categories        map[string]map[string]any
	Forms             map[string][]url.Values
	AddFiles          []UploadedTorrent
	CallOrder         []string
	SkipSeqToggle     bool
	AppVersion        string
	APIVersion        string
	TransferInfo      any
}

func New(t TestingT) *Server {
	t.Helper()
	return &Server{
		t:           t,
		StatusQueue: map[string][]int{},
		Categories:  map[string]map[string]any{},
		Forms:       map[string][]url.Values{},
	}
}

func (s *Server) Creds() Credentials {
	return Credentials{
		URL:  "http://qbt.test",
		User: "admin",
		Pass: "secret",
	}
}

func (s *Server) nextStatus(path string) int {
	queue := s.StatusQueue[path]
	if len(queue) == 0 {
		return http.StatusOK
	}
	status := queue[0]
	s.StatusQueue[path] = queue[1:]
	return status
}

func CloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func (s *Server) recordForm(path string, r *http.Request) {
	s.CallOrder = append(s.CallOrder, path)
	if err := r.ParseForm(); err != nil {
		s.t.Fatalf("ParseForm failed for %s: %v", path, err)
	}
	s.Forms[path] = append(s.Forms[path], CloneValues(r.Form))
}

func (s *Server) recordMultipartForm(path string, r *http.Request) {
	s.CallOrder = append(s.CallOrder, path)
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
	s.Forms[path] = append(s.Forms[path], CloneValues(r.MultipartForm.Value))
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
		s.AddFiles = append(s.AddFiles, UploadedTorrent{
			Filename: header.Filename,
			Content:  content,
		})
	}
}

func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
	if status := s.nextStatus(r.URL.Path); status != http.StatusOK {
		w.WriteHeader(status)
		return
	}

	switch r.URL.Path {
	case "/api/v2/auth/login":
		s.LoginCalls++
		_, _ = io.WriteString(w, "Ok.")
	case "/api/v2/torrents/info":
		s.InfoCalls++
		s.handleTorrentInfo(w, r)
	case "/api/v2/torrents/trackers":
		if s.TrackersRawBody != "" {
			_, _ = io.WriteString(w, s.TrackersRawBody)
			return
		}
		_ = json.NewEncoder(w).Encode(s.Trackers)
	case "/api/v2/torrents/categories":
		if s.CategoriesRawBody != "" {
			_, _ = io.WriteString(w, s.CategoriesRawBody)
			return
		}
		_ = json.NewEncoder(w).Encode(s.Categories)
	case "/api/v2/torrents/createCategory":
		s.recordForm(r.URL.Path, r)
		category := r.Form.Get("category")
		s.Categories[category] = map[string]any{}
	case "/api/v2/torrents/add":
		s.recordMultipartForm(r.URL.Path, r)
	case "/api/v2/torrents/setCategory",
		"/api/v2/torrents/setTags",
		"/api/v2/torrents/setUploadLimit",
		"/api/v2/torrents/setDownloadLimit",
		"/api/v2/torrents/setShareLimits",
		"/api/v2/torrents/setSuperSeeding",
		"/api/v2/torrents/setAutoManagement",
		"/api/v2/torrents/stop",
		"/api/v2/torrents/delete",
		"/api/v2/torrents/start",
		"/api/v2/torrents/setForceStart",
		"/api/v2/torrents/setLocation":
		s.recordForm(r.URL.Path, r)
	case "/api/v2/torrents/toggleSequentialDownload":
		s.recordForm(r.URL.Path, r)
		if !s.SkipSeqToggle {
			s.toggleSequentialDownload(r.Form.Get("hashes"))
		}
	case "/api/v2/app/version":
		v := s.AppVersion
		if v == "" {
			v = "v5.0.0"
		}
		_, _ = io.WriteString(w, v)
	case "/api/v2/app/webapiVersion":
		v := s.APIVersion
		if v == "" {
			v = "2.11.0"
		}
		_, _ = io.WriteString(w, v)
	case "/api/v2/transfer/info":
		info := s.TransferInfo
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

func (s *Server) handleTorrentInfo(w http.ResponseWriter, r *http.Request) {
	if s.InfoRawBody != "" {
		_, _ = io.WriteString(w, s.InfoRawBody)
		return
	}

	hashes := r.URL.Query().Get("hashes")
	if hashes == "" {
		_ = json.NewEncoder(w).Encode(s.Torrents)
		return
	}

	filtered := filterByHash(s.Torrents, hashes)
	_ = json.NewEncoder(w).Encode(filtered.Interface())
}

func filterByHash(torrents any, hash string) reflect.Value {
	value := reflect.ValueOf(torrents)
	if !value.IsValid() || value.Kind() != reflect.Slice {
		return reflect.MakeSlice(reflect.TypeOf([]any{}), 0, 0)
	}

	filtered := reflect.MakeSlice(value.Type(), 0, 1)
	for i := 0; i < value.Len(); i++ {
		item := value.Index(i)
		field := item.FieldByName("Hash")
		if field.IsValid() && field.Kind() == reflect.String && strings.EqualFold(field.String(), hash) {
			filtered = reflect.Append(filtered, item)
		}
	}
	return filtered
}

func (s *Server) toggleSequentialDownload(hash string) {
	value := reflect.ValueOf(s.Torrents)
	if !value.IsValid() || value.Kind() != reflect.Slice {
		return
	}
	for i := 0; i < value.Len(); i++ {
		item := value.Index(i)
		hashField := item.FieldByName("Hash")
		seqField := item.FieldByName("SequentialDL")
		if hashField.IsValid() && seqField.IsValid() && seqField.CanSet() && hashField.Kind() == reflect.String && seqField.Kind() == reflect.Bool && strings.EqualFold(hashField.String(), hash) {
			seqField.SetBool(!seqField.Bool())
		}
	}
}

type RoundTripFunc func(*http.Request) (*http.Response, error)

func (f RoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
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

func (s *Server) RoundTrip(req *http.Request) *http.Response {
	rec := newResponseRecorder()
	s.Handle(rec, req)
	return rec.Result()
}
