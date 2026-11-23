package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
	qbt "github.com/naterator/qbitctl/pkg/client"
)

type qbTestServer struct {
	*qbtest.Server
	Torrents     []TorrentInfo
	Trackers     []TrackerEntry
	TransferInfo *qbt.TransferInfo
}

func newQBTestServer(t *testing.T) *qbTestServer {
	t.Helper()
	return &qbTestServer{Server: qbtest.New(t)}
}

func (s *qbTestServer) creds() Credentials {
	creds := s.Creds()
	return Credentials{
		URL:  creds.URL,
		User: creds.User,
		Pass: creds.Pass,
	}
}

func withQBServerClientFactory(t *testing.T, s *qbTestServer) {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(configDir, "cache"))
	t.Chdir(configDir)
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{
			Jar:       jar,
			Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) { return s.roundTrip(req), nil }),
		}
	})
	t.Cleanup(restore)
}

func (s *qbTestServer) roundTrip(req *http.Request) *http.Response {
	s.Server.Torrents = s.Torrents
	s.Server.Trackers = s.Trackers
	if s.TransferInfo != nil {
		s.Server.TransferInfo = s.TransferInfo
	}
	resp := s.Server.RoundTrip(req)
	if torrents, ok := s.Server.Torrents.([]TorrentInfo); ok {
		s.Torrents = torrents
	}
	return resp
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
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "qbitctl "+qbitctlVersion {
		t.Fatalf("stdout = %q", stdout.String())
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
	if !strings.Contains(out.String(), "update") {
		t.Fatalf("help output missing update command: %q", out.String())
	}
	if !strings.Contains(out.String(), "add") {
		t.Fatalf("help output missing add command: %q", out.String())
	}
}

func TestConfigWriteCommandSavesConfig(t *testing.T) {
	t.Setenv("QBITCTL_MASTER_KEY", "mock-secret-for-testing")
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
	server.Torrents = []TorrentInfo{{
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
		if got := server.Forms["/api/v2/torrents/add"][0].Get("urls"); got != magnet {
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
		if len(server.AddFiles) != 1 {
			t.Fatalf("addFiles = %d, want 1", len(server.AddFiles))
		}
		if server.AddFiles[0].Filename != "ubuntu.torrent" {
			t.Fatalf("uploaded filename = %q", server.AddFiles[0].Filename)
		}
		if string(server.AddFiles[0].Content) != "dummy torrent payload" {
			t.Fatalf("uploaded content = %q", string(server.AddFiles[0].Content))
		}
	})
}

func TestCLIListAndShowStructuredOutput(t *testing.T) {
	server := newQBTestServer(t)
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server.Torrents = []TorrentInfo{{
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

	t.Run("list selected fields", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"list", "--fields", "hash,name,progress",
		})

		stdout, _ := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		})
		if !strings.Contains(stdout, "hash\tname\tprogress") || !strings.Contains(stdout, fullHash[:8]+"\tUbuntu ISO\t0.999900") {
			t.Fatalf("list --fields output = %q", stdout)
		}
	})

	t.Run("show selected fields without header", func(t *testing.T) {
		withQBServerClientFactory(t, server)
		cmd := newRootCmd()
		cmd.SetArgs([]string{
			"--url", server.creds().URL,
			"--user", "admin",
			"--pass", "secret",
			"show", fullHash[:8],
			"--fields", "name,state",
			"--no-header",
		})

		stdout, _ := captureOutput(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
		})
		if strings.TrimSpace(stdout) != "Ubuntu ISO\tstalledUP" {
			t.Fatalf("show --fields --no-header output = %q", stdout)
		}
	})
}

func TestInvalidArgumentCountsFailBeforeAuth(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	for _, args := range [][]string{
		{"show"},
		{"get", "name"},
		{"set", "category", "linux"},
		{"start"},
		{"pause"},
		{"force-start"},
		{"move", "/downloads/linux"},
		{"remove"},
		{"delete"},
		{"list", "unexpected"},
		{"version", "unexpected"},
		{"config", "unexpected"},
		{"config", "write", "unexpected"},
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
		if server.LoginCalls != 0 || server.InfoCalls != 0 {
			t.Fatalf("args %v authenticated before argument validation: logins=%d info=%d", args, server.LoginCalls, server.InfoCalls)
		}
	}
}

func TestTargetedCommandsAcceptPositionalHash(t *testing.T) {
	server := newQBTestServer(t)
	fullHash := "0123456789abcdef0123456789abcdef01234567"
	server.Torrents = []TorrentInfo{{Name: "Ubuntu ISO", Hash: fullHash}}

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
		if got := server.Forms["/api/v2/torrents/start"][0].Get("hashes"); got != fullHash {
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
		if got := server.Forms["/api/v2/torrents/setLocation"][0].Get("hashes"); got != fullHash {
			t.Fatalf("move hashes = %q, want %q", got, fullHash)
		}
		if got := server.Forms["/api/v2/torrents/setLocation"][0].Get("location"); got != "/downloads/linux" {
			t.Fatalf("move location = %q, want /downloads/linux", got)
		}
	})
}

func TestListFiltersUseTorrentInfoQuery(t *testing.T) {
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Linux ISO", Hash: "0123456789abcdef0123456789abcdef01234567"}}
	withQBServerClientFactory(t, server)

	var filter, category, tag string
	restore := qbt.SetHTTPClientFactoryForTesting(func(jar http.CookieJar) *http.Client {
		return &http.Client{
			Jar: jar,
			Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/v2/torrents/info" {
					query := req.URL.Query()
					filter, category, tag = query.Get("filter"), query.Get("category"), query.Get("tag")
				}
				return server.roundTrip(req), nil
			}),
		}
	})
	t.Cleanup(restore)

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--url", server.creds().URL,
		"--user", "admin",
		"--pass", "secret",
		"list", "--server-json", "--state", "stopped", "--category", "linux", "--tag", "release",
	})
	if _, stderr := captureOutput(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
	}); strings.TrimSpace(stderr) != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if filter != "stopped" || category != "linux" || tag != "release" {
		t.Fatalf("torrent info filter = %q, category = %q, tag = %q", filter, category, tag)
	}
}

func TestSetAndMoveResolveMultipleHashesWithOneLookupEach(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Alpha", Hash: hash1}, {Name: "Beta", Hash: hash2}}
	withQBServerClientFactory(t, server)

	setCmd := newRootCmd()
	setCmd.SetArgs([]string{
		"--url", server.creds().URL,
		"--user", "admin",
		"--pass", "secret",
		"set", "tags", "release", hash1[:8], hash2[:8],
	})
	if _, stderr := captureOutput(t, func() {
		if err := setCmd.Execute(); err != nil {
			t.Fatalf("set Execute returned error: %v", err)
		}
	}); strings.TrimSpace(stderr) != "" {
		t.Fatalf("unexpected set stderr: %q", stderr)
	}
	if server.InfoCalls != 1 {
		t.Fatalf("set torrent lookups = %d, want 1", server.InfoCalls)
	}
	if got := server.Forms["/api/v2/torrents/setTags"][0].Get("hashes"); got != hash1+"|"+hash2 {
		t.Fatalf("set hashes = %q, want %q", got, hash1+"|"+hash2)
	}

	moveCmd := newRootCmd()
	moveCmd.SetArgs([]string{
		"--url", server.creds().URL,
		"--user", "admin",
		"--pass", "secret",
		"move", "/downloads/linux", hash1[:8], hash2[:8],
	})
	if _, stderr := captureOutput(t, func() {
		if err := moveCmd.Execute(); err != nil {
			t.Fatalf("move Execute returned error: %v", err)
		}
	}); strings.TrimSpace(stderr) != "" {
		t.Fatalf("unexpected move stderr: %q", stderr)
	}
	if server.InfoCalls != 2 {
		t.Fatalf("move torrent lookups = %d total, want 2", server.InfoCalls)
	}
	moveForms := server.Forms["/api/v2/torrents/setLocation"]
	if len(moveForms) != 1 || moveForms[0].Get("hashes") != hash1+"|"+hash2 {
		t.Fatalf("move hashes = %#v, want one joined request", moveForms)
	}
	if got := moveForms[0].Get("location"); got != "/downloads/linux" {
		t.Fatalf("move location = %q, want /downloads/linux", got)
	}
}

func TestBatchStartMultipleHashes(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Alpha", Hash: hash1}, {Name: "Beta", Hash: hash2}}

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
	got := server.Forms["/api/v2/torrents/start"][0].Get("hashes")
	if got != hash1+"|"+hash2 {
		t.Fatalf("start hashes = %q, want %q", got, hash1+"|"+hash2)
	}
}

func TestUnknownFullHashRejectedBeforeMutation(t *testing.T) {
	known := "0123456789abcdef0123456789abcdef01234567"
	unknown := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Known", Hash: known}}
	withQBServerClientFactory(t, server)

	for _, args := range [][]string{{"start", unknown}, {"start", known[:8], unknown}} {
		cmd := newRootCmd()
		cmd.SetArgs(append([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret"}, args...))
		captureOutput(t, func() {
			var exit exitError
			if err := cmd.Execute(); !errors.As(err, &exit) || exit.code != exitBadArgs {
				t.Fatalf("start %q = %v, want exit %d", args, err, exitBadArgs)
			}
		})
	}
	if len(server.Forms["/api/v2/torrents/start"]) != 0 {
		t.Fatal("unknown full hash sent a start request")
	}
}

func TestBatchRemoveMultipleHashes(t *testing.T) {
	hash1 := "0123456789abcdef0123456789abcdef01234567"
	hash2 := "fedcba9876543210fedcba9876543210fedcba98"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Alpha", Hash: hash1}, {Name: "Beta", Hash: hash2}}

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
	// Deleting should be one request; qBittorrent stops the torrent as part of
	// the delete operation when needed.
	deleteHashes := server.Forms["/api/v2/torrents/delete"][0].Get("hashes")
	wantHashes := hash1 + "|" + hash2
	if deleteHashes != wantHashes {
		t.Fatalf("delete hashes = %q, want %q", deleteHashes, wantHashes)
	}
	if len(server.Forms["/api/v2/torrents/stop"]) != 0 {
		t.Fatal("remove unexpectedly stopped torrents before deleting")
	}
}

func TestDeleteRejectsBlankSelectorsWithOneTorrent(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Only torrent", Hash: hash}}
	withQBServerClientFactory(t, server)

	for _, args := range [][]string{{""}, {hash[:8], " \t"}} {
		cmd := newRootCmd()
		cmd.SetArgs(append([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "delete"}, args...))
		captureOutput(t, func() {
			var exit exitError
			if err := cmd.Execute(); !errors.As(err, &exit) || exit.code != exitBadArgs {
				t.Fatalf("delete %q = %v, want exit %d", args, err, exitBadArgs)
			}
		})
	}
	if server.InfoCalls != 0 || len(server.Forms["/api/v2/torrents/stop"]) != 0 || len(server.Forms["/api/v2/torrents/delete"]) != 0 {
		t.Fatal("delete with a blank selector looked up or modified torrents")
	}
}

func TestConfigPromptsUseCobraStreamsAndSelectedPath(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	t.Setenv("QBITCTL_MASTER_KEY", "config-prompt-test-key")
	path := filepath.Join(t.TempDir(), "alternate.json")
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", path, "config"})
	cmd.SetIn(strings.NewReader(server.creds().URL + "\nadmin\nsecret\n"))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config failed: %v: %s", err, stderr.String())
	}
	if server.LoginCalls != 1 || !strings.Contains(stdout.String(), "Saved config to "+path) || strings.Contains(stdout.String(), "Save path") {
		t.Fatalf("unexpected setup: login calls %d, stdout %q", server.LoginCalls, stdout.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticatedOutputUsesCobraStreams(t *testing.T) {
	server := newQBTestServer(t)
	server.Torrents = []TorrentInfo{{Name: "Mock ISO", Hash: strings.Repeat("a", 40)}}
	withQBServerClientFactory(t, server)
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "list", "--json"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Mock ISO") {
		t.Fatalf("list: error = %v, stdout = %q, stderr = %q", err, stdout.String(), stderr.String())
	}
}

func TestAddSearchIDWithoutCacheFailsBeforeLogin(t *testing.T) {
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	cmd := newRootCmd()
	cmd.SetArgs([]string{"add", "--search-id", "123456789012", "--stopped"})
	if err := cmd.Execute(); err == nil || server.LoginCalls != 0 {
		t.Fatalf("cached add with direct options: error = %v, login calls = %d", err, server.LoginCalls)
	}

	cmd = newRootCmd()
	cmd.SetArgs([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret", "add", "magnet:?xt=urn:btih:abc", "--category", "Linux", "--save-path", "/downloads/iso", "--stopped"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	forms := server.Forms["/api/v2/torrents/add"]
	if len(forms) != 1 || forms[0].Get("category") != "Linux" || forms[0].Get("savepath") != "/downloads/iso" || forms[0].Get("stopped") != "true" {
		t.Fatalf("add requests = %v", forms)
	}
}

func TestAddAdvancedOptions(t *testing.T) {
	server := newQBTestServer(t)
	server.AppVersion = "v5.3.0"
	withQBServerClientFactory(t, server)
	root := newRootCmd()
	root.SetArgs([]string{
		"--url", server.creds().URL, "--user", "admin", "--pass", "secret",
		"add", "magnet:?xt=urn:btih:abc",
		"--content-layout", "subfolder",
		"--download-path", "/incomplete",
		"--skip-checking=false",
		"--stop-condition", "metadata",
		"--sequential",
		"--first-last-piece-priority",
		"--ratio-limit", "1.5",
		"--seeding-time-limit", "90m",
		"--inactive-seeding-time-limit", "-1",
		"--share-limits-mode", "match-any",
		"--share-limit-action", "stop",
	})
	if _, stderr := captureOutput(t, func() {
		if err := root.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("advanced add returned error: %v", err)
		}
	}); stderr != "" {
		t.Fatalf("advanced add wrote stderr: %q", stderr)
	}
	forms := server.Forms["/api/v2/torrents/add"]
	if len(forms) != 1 {
		t.Fatalf("add requests = %d, want 1", len(forms))
	}
	form := forms[0]
	for field, want := range map[string]string{
		"contentLayout":            "Subfolder",
		"downloadPath":             "/incomplete",
		"useDownloadPath":          "true",
		"autoTMM":                  "false",
		"seedMode":                 "false",
		"stopCondition":            "MetadataReceived",
		"sequentialDownload":       "true",
		"firstLastPiecePrio":       "true",
		"ratioLimit":               "1.500000",
		"seedingTimeLimit":         "90",
		"inactiveSeedingTimeLimit": "-1",
		"shareLimitsMode":          "MatchAny",
		"shareLimitAction":         "Stop",
	} {
		if got := form.Get(field); got != want {
			t.Errorf("advanced add form %s = %q, want %q", field, got, want)
		}
	}
	if got := form.Get("skip_checking"); got != "" {
		t.Fatalf("5.3 request sent removed skip_checking field %q", got)
	}
}

func TestResolveHashWithNoHashInOptions(t *testing.T) {
	// Options doesn't require hash, but ResolveHash will fail if it's missing.
	server := newQBTestServer(t)
	withQBServerClientFactory(t, server)
	app, err := qbt.NewClient(&CLIOptions{
		URL:  server.creds().URL,
		User: "admin",
		Pass: "secret",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	defer app.Logout()
	_, err = app.ResolveHash("")
	if err == nil {
		t.Fatal("ResolveHash unexpectedly succeeded without hash")
	}
	var coded *qbt.CodedError
	if !errors.As(err, &coded) || coded.Code != exitBadArgs {
		t.Fatalf("ResolveHash err = %v", err)
	}
}
