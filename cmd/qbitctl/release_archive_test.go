package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type zipEntry struct {
	name string
	body []byte
}

func zipBytes(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTempAsset(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCompressedAssetName(t *testing.T) {
	if got := compressedAssetName("linux", "amd64"); got != "qbitctl-linux-amd64.gz" {
		t.Fatalf("linux asset = %q", got)
	}
	if got := compressedAssetName("windows", "arm64"); got != "qbitctl-windows-arm64.zip" {
		t.Fatalf("windows asset = %q", got)
	}
}

func TestDownloadForRuntimeSelection(t *testing.T) {
	asset := func(name string) githubReleaseAsset {
		return githubReleaseAsset{Name: name, BrowserDownloadURL: "https://example.test/" + name}
	}
	tests := []struct {
		name         string
		goos         string
		assets       []string
		wantBinary   string
		wantChecksum string
	}{
		{
			name:         "prefers gzip with its checksum",
			goos:         "linux",
			assets:       []string{"qbitctl-linux-amd64", "qbitctl-linux-amd64.sha256", "qbitctl-linux-amd64.gz", "qbitctl-linux-amd64.gz.sha256", "qbitctl-linux-amd64.xz", "qbitctl-linux-amd64.xz.sha256"},
			wantBinary:   "qbitctl-linux-amd64.gz",
			wantChecksum: "qbitctl-linux-amd64.gz.sha256",
		},
		{
			name:         "falls back to raw without a compressed checksum",
			goos:         "linux",
			assets:       []string{"qbitctl-linux-amd64", "qbitctl-linux-amd64.sha256", "qbitctl-linux-amd64.gz"},
			wantBinary:   "qbitctl-linux-amd64",
			wantChecksum: "qbitctl-linux-amd64.sha256",
		},
		{
			name:         "falls back to raw for releases before compressed assets",
			goos:         "linux",
			assets:       []string{"qbitctl-linux-amd64", "qbitctl-linux-amd64.sha256"},
			wantBinary:   "qbitctl-linux-amd64",
			wantChecksum: "qbitctl-linux-amd64.sha256",
		},
		{
			name:         "prefers zip on windows",
			goos:         "windows",
			assets:       []string{"qbitctl-windows-amd64.exe", "qbitctl-windows-amd64.exe.sha256", "qbitctl-windows-amd64.zip", "qbitctl-windows-amd64.zip.sha256"},
			wantBinary:   "qbitctl-windows-amd64.zip",
			wantChecksum: "qbitctl-windows-amd64.zip.sha256",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var release githubRelease
			for _, name := range tt.assets {
				release.Assets = append(release.Assets, asset(name))
			}
			binary, checksum, err := release.downloadForRuntime(tt.goos, "amd64")
			if err != nil {
				t.Fatalf("downloadForRuntime returned error: %v", err)
			}
			if binary.Name != tt.wantBinary || checksum.Name != tt.wantChecksum {
				t.Fatalf("selected %q / %q, want %q / %q", binary.Name, checksum.Name, tt.wantBinary, tt.wantChecksum)
			}
		})
	}
}

func TestExtractReleaseBinaryRawIsUnchanged(t *testing.T) {
	path := writeTempAsset(t, "qbitctl-linux-amd64", []byte("raw"))
	got, err := extractReleaseBinary(path, "qbitctl-linux-amd64", "linux")
	if err != nil || got != path {
		t.Fatalf("extractReleaseBinary = %q, %v; want the original path", got, err)
	}
}

func TestExtractGzipBinary(t *testing.T) {
	binary := []byte("gzip-binary-content")
	path := writeTempAsset(t, "qbitctl-linux-amd64.gz", gzipBytes(t, binary))
	got, err := extractReleaseBinary(path, "qbitctl-linux-amd64.gz", "linux")
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if data, _ := os.ReadFile(got); !bytes.Equal(data, binary) {
		t.Fatalf("extracted %q, want %q", data, binary)
	}
}

func TestExtractGzipBinaryRejectsInvalidArchives(t *testing.T) {
	valid := gzipBytes(t, []byte("gzip-binary-content"))
	corrupt := append([]byte(nil), valid...)
	corrupt[len(corrupt)-8] ^= 0xff // break the trailing CRC-32

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "not gzip", data: []byte("plain bytes"), want: "open gzip archive"},
		{name: "bad checksum", data: corrupt, want: "checksum"},
		{name: "empty binary", data: gzipBytes(t, nil), want: "empty binary"},
		{name: "truncated", data: valid[:len(valid)/2], want: "decompress gzip archive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempAsset(t, "qbitctl-linux-amd64.gz", tt.data)
			_, err := extractReleaseBinary(path, "qbitctl-linux-amd64.gz", "linux")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestExtractBinaryEnforcesSizeLimit(t *testing.T) {
	previous := maxExtractedBinaryBytes
	maxExtractedBinaryBytes = 8
	t.Cleanup(func() { maxExtractedBinaryBytes = previous })

	large := bytes.Repeat([]byte("x"), 64)
	gz := writeTempAsset(t, "qbitctl-linux-amd64.gz", gzipBytes(t, large))
	if _, err := extractReleaseBinary(gz, "qbitctl-linux-amd64.gz", "linux"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("gzip error = %v, want a size limit error", err)
	}
	zipped := writeTempAsset(t, "qbitctl-windows-amd64.zip", zipBytes(t, zipEntry{"qbitctl.exe", large}))
	if _, err := extractReleaseBinary(zipped, "qbitctl-windows-amd64.zip", "windows"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("zip error = %v, want a size limit error", err)
	}
}

func TestExtractZipBinary(t *testing.T) {
	binary := []byte("windows-binary-content")
	path := writeTempAsset(t, "qbitctl-windows-amd64.zip", zipBytes(t, zipEntry{"qbitctl.exe", binary}))
	got, err := extractReleaseBinary(path, "qbitctl-windows-amd64.zip", "windows")
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if data, _ := os.ReadFile(got); !bytes.Equal(data, binary) {
		t.Fatalf("extracted %q, want %q", data, binary)
	}
	if filepath.Dir(got) != filepath.Dir(path) {
		t.Fatalf("extracted to %q, want the archive's directory", got)
	}
}

func TestExtractZipBinaryRejectsUnexpectedContents(t *testing.T) {
	binary := []byte("binary")
	tests := []struct {
		name    string
		entries []zipEntry
	}{
		{name: "wrong name", entries: []zipEntry{{"other.exe", binary}}},
		{name: "path traversal", entries: []zipEntry{{"../qbitctl.exe", binary}}},
		{name: "nested path", entries: []zipEntry{{"bin/qbitctl.exe", binary}}},
		{name: "extra entry", entries: []zipEntry{{"qbitctl.exe", binary}, {"README.txt", []byte("x")}}},
		{name: "no entries", entries: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempAsset(t, "qbitctl-windows-amd64.zip", zipBytes(t, tt.entries...))
			_, err := extractReleaseBinary(path, "qbitctl-windows-amd64.zip", "windows")
			if err == nil || !strings.Contains(err.Error(), "must contain only qbitctl.exe") {
				t.Fatalf("error = %v, want an unexpected contents error", err)
			}
		})
	}
	path := writeTempAsset(t, "qbitctl-windows-amd64.zip", []byte("not a zip"))
	if _, err := extractReleaseBinary(path, "qbitctl-windows-amd64.zip", "windows"); err == nil || !strings.Contains(err.Error(), "open zip archive") {
		t.Fatalf("error = %v, want an open error", err)
	}
}

// TestGitHubReleaseUpdaterInstallsCompressedAsset exercises the complete
// update path with the compressed asset for this platform: the updater must
// download and verify the archive itself, then install the unpacked binary.
func TestGitHubReleaseUpdaterInstallsCompressedAsset(t *testing.T) {
	exeName := "qbitctl"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	exePath := filepath.Join(t.TempDir(), exeName)
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	binary := []byte("new-binary-from-archive")
	archiveName := compressedAssetName(runtime.GOOS, runtime.GOARCH)
	var archive []byte
	if runtime.GOOS == "windows" {
		archive = zipBytes(t, zipEntry{"qbitctl.exe", binary})
	} else {
		archive = gzipBytes(t, binary)
	}
	sum := sha256.Sum256(archive)
	checksumBody := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archiveName)
	rawNames, _ := releaseAssetCandidates(runtime.GOOS, runtime.GOARCH)

	var rawDownloads int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "9.9.9",
				Assets: []githubReleaseAsset{
					{Name: rawNames[0], BrowserDownloadURL: server.URL + "/download/" + rawNames[0]},
					{Name: rawNames[0] + ".sha256", BrowserDownloadURL: server.URL + "/download/" + rawNames[0] + ".sha256"},
					{Name: archiveName, BrowserDownloadURL: server.URL + "/download/" + archiveName},
					{Name: archiveName + ".sha256", BrowserDownloadURL: server.URL + "/download/" + archiveName + ".sha256"},
				},
			})
		case "/download/" + archiveName:
			_, _ = w.Write(archive)
		case "/download/" + archiveName + ".sha256":
			_, _ = io.WriteString(w, checksumBody)
		default:
			rawDownloads++
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var verifiedName string
	updater := &githubReleaseUpdater{
		client:           server.Client(),
		latestReleaseURL: server.URL + "/latest",
		executablePath:   func() (string, error) { return exePath, nil },
		verifyAsset: func(_ context.Context, _, filePath string) error {
			verifiedName = filepath.Base(filePath)
			return nil
		},
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
	}
	if err := updater.Run(context.Background(), "1.0.0", false, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if rawDownloads != 0 {
		t.Fatalf("updater requested %d unexpected paths, including the raw binary", rawDownloads)
	}
	if verifiedName != archiveName {
		t.Fatalf("attestation verified %q, want the downloaded archive %q", verifiedName, archiveName)
	}
	if got, _ := os.ReadFile(exePath); !bytes.Equal(got, binary) {
		t.Fatalf("installed %q, want the unpacked binary %q", got, binary)
	}
	entries, _ := os.ReadDir(filepath.Dir(exePath))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".qbitctl-update-") {
			t.Fatalf("temporary update directory %s was left behind", entry.Name())
		}
	}
}

// TestExtractReleaseBinaryReadsReleaseToolArchives checks archives written by
// the same gzip and zip invocations as scripts/build.sh, not only by Go.
func TestExtractReleaseBinaryReadsReleaseToolArchives(t *testing.T) {
	for _, tool := range []string{"gzip", "zip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	dir := t.TempDir()
	binary := bytes.Repeat([]byte("release-binary-"), 4096)
	raw := filepath.Join(dir, "qbitctl-linux-amd64")
	if err := os.WriteFile(raw, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	gz := raw + ".gz"
	out, err := exec.Command("sh", "-c", `gzip -9 -n -c "$1" > "$2"`, "sh", raw, gz).CombinedOutput()
	if err != nil {
		t.Fatalf("gzip: %v: %s", err, out)
	}
	if got, err := extractReleaseBinary(gz, filepath.Base(gz), "linux"); err != nil {
		t.Fatalf("extract gzip: %v", err)
	} else if data, _ := os.ReadFile(got); !bytes.Equal(data, binary) {
		t.Fatal("gzip round trip changed the binary")
	}

	stage := t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "qbitctl.exe"), binary, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "qbitctl-windows-amd64.zip")
	cmd := exec.Command("zip", "-q", "-X", "-9", archive, "qbitctl.exe")
	cmd.Dir = stage
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("zip: %v: %s", err, out)
	}
	if got, err := extractReleaseBinary(archive, filepath.Base(archive), "windows"); err != nil {
		t.Fatalf("extract zip: %v", err)
	} else if data, _ := os.ReadFile(got); !bytes.Equal(data, binary) {
		t.Fatal("zip round trip changed the binary")
	}
}
