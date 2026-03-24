package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/naterator/qbitctl/pkg/client"
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
	transferInfo      *qbt.TransferInfo
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
			info = &qbt.TransferInfo{
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
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{
			Jar:       jar,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) { return s.roundTrip(req), nil }),
		}
	})
	t.Cleanup(restore)
}

func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe failed: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe failed: %v", err)
	}

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout = stdoutW
	os.Stderr = stderrW
	t.Cleanup(func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	})

	stdoutCh := make(chan string, 1)
	stderrCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(stdoutR)
		stdoutCh <- string(data)
	}()
	go func() {
		data, _ := io.ReadAll(stderrR)
		stderrCh <- string(data)
	}()

	fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	return <-stdoutCh, <-stderrCh
}

func TestCanonicalFieldMappings(t *testing.T) {
	getTests := map[string]string{
		"hash":                "hash",
		"upload-limit":        "up-limit",
		"download-path":       "dl-path",
		"status":              "state",
		"trackers":            "tracker-list",
		"auto-tmm":            "autotmm",
		"sequential-download": "seqdl",
	}
	for input, want := range getTests {
		if got := qbt.CanonicalGetField(input); got != want {
			t.Fatalf("qbt.CanonicalGetField(%q) = %q, want %q", input, got, want)
		}
	}

	setTests := map[string]string{
		"upload-limit":        "up-limit",
		"download-limit":      "dl-limit",
		"sequential-download": "seqdl",
		"auto-tmm":            "autotmm",
	}
	for input, want := range setTests {
		if got := qbt.CanonicalSetField(input); got != want {
			t.Fatalf("qbt.CanonicalSetField(%q) = %q, want %q", input, got, want)
		}
	}

}

func TestRootCommandVersion(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"version"})

	stdout, stderr := captureOutput(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
	})
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if strings.TrimSpace(stdout) != "qbitctl "+qbitctlVersion {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestRootCommandHelpSucceeds(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(out.String(), "View:") {
		t.Fatalf("help output missing View group: %q", out.String())
	}
	if !strings.Contains(out.String(), "config") {
		t.Fatalf("help output missing config command: %q", out.String())
	}
	if !strings.Contains(out.String(), "completion") {
		t.Fatalf("help output missing completion command: %q", out.String())
	}
	if !strings.Contains(out.String(), "selfupdate") {
		t.Fatalf("help output missing autoupdate command: %q", out.String())
	}
	if !strings.Contains(out.String(), "add") {
		t.Fatalf("help output missing add command: %q", out.String())
	}
}

func TestConfigWriteCommandSavesConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.json")

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--url", "https://qb.example.com/base/",
		"--user", "admin",
		"--pass", "secret",
		"config", "write",
		"--output", configPath,
	})

	if _, stderr := captureOutput(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
	}); !strings.Contains(stderr, "Saved config") {
		t.Fatalf("stderr = %q, want saved-config message", stderr)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var disk struct {
		URL      string `json:"url"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(content, &disk); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if disk.URL != "https://qb.example.com/base" || disk.User != "admin" {
		t.Fatalf("disk config = %#v", disk)
	}
	if !strings.HasPrefix(disk.Password, "enc:v") {
		t.Fatalf("password = %q, want encrypted value", disk.Password)
	}
}

func TestCLIListJSONAndGetName(t *testing.T) {
	server := newQBTestServer(t)
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server.torrents = []TorrentInfo{{
		Name:  "Ubuntu ISO",
		Hash:  fullHash,
		State: "stalledUP",
	}}

	t.Run("list json", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"list", "--server-json",
		})

		stdout, _ := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		})
		if !strings.Contains(stdout, `"Ubuntu ISO"`) {
			t.Fatalf("list --json output = %q", stdout)
		}
	})

	t.Run("get name resolves short hash", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"get", "name", fullHash[:8],
		})

		stdout, _ := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		})
		if strings.TrimSpace(stdout) != "Ubuntu ISO" {
			t.Fatalf("get name output = %q", stdout)
		}
	})

	t.Run("get hash", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"get", "hash", fullHash[:8],
		})

		stdout, _ := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		})
		if strings.TrimSpace(stdout) != fullHash[:8] {
			t.Fatalf("get hash output = %q", stdout)
		}
	})

	t.Run("show accepts positional hash", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"show", fullHash[:8],
		})

		stdout, _ := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		})
		if !strings.Contains(stdout, "Name...") || !strings.Contains(stdout, "| Ubuntu ISO") {
			t.Fatalf("show positional hash output = %q", stdout)
		}
	})
}

func TestCLIAddMagnetAndTorrentFile(t *testing.T) {
	server := newQBTestServer(t)

	t.Run("magnet", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Ubuntu"
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"add", magnet,
		})

		if _, stderr := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		}); strings.TrimSpace(stderr) != "" {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
		if got := server.forms["/api/v2/torrents/add"][0].Get("urls"); got != magnet {
			t.Fatalf("magnet urls = %q, want %q", got, magnet)
		}
	})

	t.Run("torrent file", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		path := filepath.Join(t.TempDir(), "ubuntu.torrent")
		if err := os.WriteFile(path, []byte("dummy torrent payload"), 0o600); err != nil {
			t.Fatalf("write torrent file failed: %v", err)
		}

		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"add", path,
		})

		if _, stderr := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		}); strings.TrimSpace(stderr) != "" {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
		if len(server.addFiles) != 1 {
			t.Fatalf("addFiles = %d, want 1", len(server.addFiles))
		}
		if server.addFiles[0].Filename != "ubuntu.torrent" {
			t.Fatalf("uploaded filename = %q", server.addFiles[0].Filename)
		}
		if string(server.addFiles[0].Content) != "dummy torrent payload" {
			t.Fatalf("uploaded content = %q", string(server.addFiles[0].Content))
		}
	})
}

func TestCLIListAndShowStructuredOutput(t *testing.T) {
	server := newQBTestServer(t)
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server.torrents = []TorrentInfo{{
		Name:     "Ubuntu ISO",
		Hash:     fullHash,
		State:    "stalledUP",
		Progress: 0.9999,
	}}

	t.Run("show template", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"show", fullHash[:8],
			"--template", `{{field "hash" .}} {{field "name" .}} {{field "progress" .}}`,
		})

		stdout, _ := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		})
		if strings.TrimSpace(stdout) != fullHash[:8]+" Ubuntu ISO 0.999900" {
			t.Fatalf("show --template output = %q", stdout)
		}
	})
}

func TestCommandsRequiringHashReturnExitBadArgs(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	for _, args := range [][]string{
		{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "show"},
		{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "get", "name"},
		{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "set", "category", "linux"},
		{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "start"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)
		var err error
		_, _ = captureOutput(t, func() {
			err = cmd.Execute()
		})
		if err == nil {
			t.Fatalf("args %v unexpectedly succeeded", args)
		}
		var exitErr exitError
		if !errors.As(err, &exitErr) || exitErr.code != exitBadArgs {
			t.Fatalf("args %v err = %v", args, err)
		}
	}
}

func TestTargetedCommandsAcceptPositionalHash(t *testing.T) {
	server := newQBTestServer(t)
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server.torrents = []TorrentInfo{{Name: "Ubuntu ISO", Hash: fullHash}}

	t.Run("start positional hash", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"start", fullHash[:8],
		})

		if _, stderr := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		}); strings.TrimSpace(stderr) != "" {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
		if got := server.forms["/api/v2/torrents/start"][0].Get("hashes"); got != fullHash {
			t.Fatalf("start hashes = %q, want %q", got, fullHash)
		}
	})

	t.Run("move positional hash after path", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"move", "/downloads/linux", fullHash[:8],
		})

		if _, stderr := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		}); strings.TrimSpace(stderr) != "" {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
		if got := server.forms["/api/v2/torrents/setLocation"][0].Get("hashes"); got != fullHash {
			t.Fatalf("move hashes = %q, want %q", got, fullHash)
		}
		if got := server.forms["/api/v2/torrents/setLocation"][0].Get("location"); got != "/downloads/linux" {
			t.Fatalf("move location = %q, want /downloads/linux", got)
		}
	})
}

func TestBatchStartMultipleHashes(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{{Name: "Alpha", Hash: hash1}, {Name: "Beta", Hash: hash2}}

	withQBServerClientFactory(t, server)
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--url", server.creds().URL,
		"--user", "admin",
		"--pass", "secret",
		"start", hash1[:8], hash2[:8],
	})

	if _, stderr := captureOutput(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
	}); strings.TrimSpace(stderr) != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	got := server.forms["/api/v2/torrents/start"][0].Get("hashes")
	if got != hash1+"|"+hash2 {
		t.Fatalf("start hashes = %q, want %q", got, hash1+"|"+hash2)
	}
}

func TestBatchRemoveMultipleHashes(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"
	server := newQBTestServer(t)
	server.torrents = []TorrentInfo{{Name: "Alpha", Hash: hash1}, {Name: "Beta", Hash: hash2}}

	withQBServerClientFactory(t, server)
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--url", server.creds().URL,
		"--user", "admin",
		"--pass", "secret",
		"remove", hash1[:8], hash2[:8],
	})

	if _, stderr := captureOutput(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
	}); strings.TrimSpace(stderr) != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	// Should have paused and then deleted both
	stopHashes := server.forms["/api/v2/torrents/stop"][0].Get("hashes")
	deleteHashes := server.forms["/api/v2/torrents/delete"][0].Get("hashes")
	wantHashes := hash1 + "|" + hash2
	if stopHashes != wantHashes {
		t.Fatalf("stop hashes = %q, want %q", stopHashes, wantHashes)
	}
	if deleteHashes != wantHashes {
		t.Fatalf("delete hashes = %q, want %q", deleteHashes, wantHashes)
	}
}

func TestCollectHashInputs(t *testing.T) {
	t.Run("positional args take precedence", func(t *testing.T) {
		opts := &CLIOptions{Hash: "from-flag"}
		got := collectHashInputs(opts, []string{"arg1", "arg2"})
		if len(got) != 2 || got[0] != "arg1" || got[1] != "arg2" {
			t.Fatalf("collectHashInputs = %v, want [arg1 arg2]", got)
		}
	})

	t.Run("falls back to flag", func(t *testing.T) {
		opts := &CLIOptions{Hash: "from-flag"}
		got := collectHashInputs(opts, nil)
		if len(got) != 1 || got[0] != "from-flag" {
			t.Fatalf("collectHashInputs = %v, want [from-flag]", got)
		}
	})

	t.Run("returns nil when empty", func(t *testing.T) {
		opts := &CLIOptions{}
		got := collectHashInputs(opts, nil)
		if got != nil {
			t.Fatalf("collectHashInputs = %v, want nil", got)
		}
	})
}

func TestNewAuthenticatedAppRequiresHashInOptions(t *testing.T) {
	// Options doesn't require hash, but ResolveHash will fail if it's missing.
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	app, err := newAuthenticatedApp(&CLIOptions{
		URL:  server.creds().URL,
		User: "admin",
		Pass: "secret",
	})
	if err != nil {
		t.Fatalf("newAuthenticatedApp returned error: %v", err)
	}
	_, err = app.ResolveHash("")
	if err == nil {
		t.Fatal("ResolveHash unexpectedly succeeded without hash")
	}
	var coded *qbt.CodedError
	if !errors.As(err, &coded) || coded.Code != exitBadArgs {
		t.Fatalf("ResolveHash err = %v", err)
	}
}
