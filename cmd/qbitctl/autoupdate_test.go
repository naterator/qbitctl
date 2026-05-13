package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type stubReleaseUpdater struct {
	runCalls       int
	currentVersion string
	err            error
}

func (s *stubReleaseUpdater) Run(ctx context.Context, currentVersion string, dryRun bool, stdout io.Writer) error {
	s.runCalls++
	s.currentVersion = currentVersion
	if stdout != nil {
		_, _ = io.WriteString(stdout, "stub updater invoked\n")
	}
	return s.err
}

func withReleaseUpdaterStub(t *testing.T, updater releaseUpdater) {
	t.Helper()
	previous := makeReleaseUpdater
	makeReleaseUpdater = func() releaseUpdater { return updater }
	t.Cleanup(func() {
		makeReleaseUpdater = previous
	})
}

func TestAutoupdateCommandRunsUpdater(t *testing.T) {
	stub := &stubReleaseUpdater{}
	withReleaseUpdaterStub(t, stub)

	cmd := newRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"selfupdate"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if stub.runCalls != 1 {
		t.Fatalf("stub updater runCalls = %d, want 1", stub.runCalls)
	}
	if stub.currentVersion != qbitctlVersion {
		t.Fatalf("stub updater currentVersion = %q, want %q", stub.currentVersion, qbitctlVersion)
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "stub updater invoked") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestAutoupdateCommandReturnsActionFailOnUpdaterError(t *testing.T) {
	stub := &stubReleaseUpdater{err: errors.New("update failed")}
	withReleaseUpdaterStub(t, stub)

	cmd := newRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"selfupdate"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute unexpectedly succeeded")
	}
	var codeErr exitError
	if !errors.As(err, &codeErr) || codeErr.code != exitActionFail {
		t.Fatalf("Execute error = %v, want exitActionFail", err)
	}
	if !strings.Contains(stderr.String(), "[ERROR] update failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestGitHubReleaseUpdaterReplacesExecutable(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "qbitctl")
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	binaryCandidates, checksumCandidates := releaseAssetCandidates(runtime.GOOS, runtime.GOARCH)
	binaryName := binaryCandidates[0]
	checksumName := checksumCandidates[0]
	binaryBody := []byte("new-binary-content")
	sum := sha256.Sum256(binaryBody)
	checksumBody := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), binaryName)

	var metadataCalls int
	var binaryCalls int
	var checksumCalls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/naterator/qbitctl/releases/latest":
			metadataCalls++
			if got := r.Header.Get("User-Agent"); got != "qbitctl/"+qbitctlVersion {
				t.Fatalf("User-Agent = %q", got)
			}
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v9.9.9",
				Assets: []githubReleaseAsset{
					{Name: binaryName, BrowserDownloadURL: server.URL + "/download/" + binaryName},
					{Name: checksumName, BrowserDownloadURL: server.URL + "/download/" + checksumName},
				},
			})
		case "/download/" + binaryName:
			binaryCalls++
			_, _ = w.Write(binaryBody)
		case "/download/" + checksumName:
			checksumCalls++
			_, _ = io.WriteString(w, checksumBody)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	updater := &githubReleaseUpdater{
		client:           server.Client(),
		latestReleaseURL: server.URL + "/repos/naterator/qbitctl/releases/latest",
		executablePath: func() (string, error) {
			return exePath, nil
		},
		verifyAsset: func(ctx context.Context, releaseTag, filePath string) error {
			if releaseTag != "v9.9.9" {
				t.Fatalf("verify releaseTag = %q, want v9.9.9", releaseTag)
			}
			if filepath.Base(filePath) != binaryName {
				t.Fatalf("verify filePath = %q, want basename %q", filePath, binaryName)
			}
			return nil
		},
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
	}

	var stdout bytes.Buffer
	if err := updater.Run(context.Background(), "1.0.0", false, &stdout); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(got) != string(binaryBody) {
		t.Fatalf("updated executable content = %q, want %q", string(got), string(binaryBody))
	}
	info, err := os.Stat(exePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("updated executable mode = %#o, want 0755", info.Mode().Perm())
	}
	if metadataCalls != 1 || binaryCalls != 1 || checksumCalls != 1 {
		t.Fatalf("calls = metadata:%d binary:%d checksum:%d", metadataCalls, binaryCalls, checksumCalls)
	}
	if !strings.Contains(stdout.String(), "Updating qbitctl from 1.0.0 to v9.9.9") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Updated qbitctl to v9.9.9") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestGitHubReleaseUpdaterSkipsCurrentVersion(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "qbitctl")
	if err := os.WriteFile(exePath, []byte("existing-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var metadataCalls int
	var downloadCalls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/naterator/qbitctl/releases/latest":
			metadataCalls++
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v1.4.1",
				Assets:  []githubReleaseAsset{},
			})
		default:
			downloadCalls++
			t.Fatalf("unexpected download path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	updater := &githubReleaseUpdater{
		client:           server.Client(),
		latestReleaseURL: server.URL + "/repos/naterator/qbitctl/releases/latest",
		executablePath: func() (string, error) {
			return exePath, nil
		},
		verifyAsset: func(ctx context.Context, releaseTag, filePath string) error {
			t.Fatal("verifyAsset unexpectedly called for current version")
			return nil
		},
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
	}

	var stdout bytes.Buffer
	if err := updater.Run(context.Background(), "1.4.1", false, &stdout); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(got) != "existing-binary" {
		t.Fatalf("executable content = %q, want existing-binary", string(got))
	}
	if metadataCalls != 1 || downloadCalls != 0 {
		t.Fatalf("calls = metadata:%d download:%d", metadataCalls, downloadCalls)
	}
	if !strings.Contains(stdout.String(), "already up to date") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestGitHubReleaseUpdaterRejectsChecksumMismatch(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "qbitctl")
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	binaryCandidates, checksumCandidates := releaseAssetCandidates(runtime.GOOS, runtime.GOARCH)
	binaryName := binaryCandidates[0]
	checksumName := checksumCandidates[0]

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/naterator/qbitctl/releases/latest":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v9.9.9",
				Assets: []githubReleaseAsset{
					{Name: binaryName, BrowserDownloadURL: server.URL + "/download/" + binaryName},
					{Name: checksumName, BrowserDownloadURL: server.URL + "/download/" + checksumName},
				},
			})
		case "/download/" + binaryName:
			_, _ = io.WriteString(w, "tampered-binary")
		case "/download/" + checksumName:
			_, _ = io.WriteString(w, strings.Repeat("a", 64)+"  "+binaryName+"\n")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	updater := &githubReleaseUpdater{
		client:           server.Client(),
		latestReleaseURL: server.URL + "/repos/naterator/qbitctl/releases/latest",
		executablePath: func() (string, error) {
			return exePath, nil
		},
		verifyAsset: func(ctx context.Context, releaseTag, filePath string) error {
			t.Fatal("verifyAsset unexpectedly called after checksum mismatch")
			return nil
		},
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
	}

	err := updater.Run(context.Background(), "1.0.0", false, io.Discard)
	if err == nil {
		t.Fatal("Run unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Run error = %v", err)
	}

	got, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatalf("ReadFile returned error: %v", readErr)
	}
	if string(got) != "old-binary" {
		t.Fatalf("executable content = %q, want old-binary", string(got))
	}
}

func TestGitHubReleaseUpdaterRejectsAttestationFailure(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "qbitctl")
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	binaryCandidates, checksumCandidates := releaseAssetCandidates(runtime.GOOS, runtime.GOARCH)
	binaryName := binaryCandidates[0]
	checksumName := checksumCandidates[0]
	binaryBody := []byte("new-binary-content")
	sum := sha256.Sum256(binaryBody)
	checksumBody := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), binaryName)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/naterator/qbitctl/releases/latest":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v9.9.9",
				Assets: []githubReleaseAsset{
					{Name: binaryName, BrowserDownloadURL: server.URL + "/download/" + binaryName},
					{Name: checksumName, BrowserDownloadURL: server.URL + "/download/" + checksumName},
				},
			})
		case "/download/" + binaryName:
			_, _ = w.Write(binaryBody)
		case "/download/" + checksumName:
			_, _ = io.WriteString(w, checksumBody)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	updater := &githubReleaseUpdater{
		client:           server.Client(),
		latestReleaseURL: server.URL + "/repos/naterator/qbitctl/releases/latest",
		executablePath: func() (string, error) {
			return exePath, nil
		},
		verifyAsset: func(ctx context.Context, releaseTag, filePath string) error {
			return errors.New("missing attestation")
		},
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
	}

	err := updater.Run(context.Background(), "1.0.0", false, io.Discard)
	if err == nil {
		t.Fatal("Run unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "verify release asset attestation") {
		t.Fatalf("Run error = %v", err)
	}

	got, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatalf("ReadFile returned error: %v", readErr)
	}
	if string(got) != "old-binary" {
		t.Fatalf("executable content = %q, want old-binary", string(got))
	}
}
