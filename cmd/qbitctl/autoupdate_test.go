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
	dryRun         bool
	err            error
}

func (s *stubReleaseUpdater) Run(ctx context.Context, currentVersion string, dryRun bool, stdout io.Writer) error {
	s.runCalls++
	s.currentVersion = currentVersion
	s.dryRun = dryRun
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

func TestUpdateCommandRunsUpdater(t *testing.T) {
	stub := &stubReleaseUpdater{}
	withReleaseUpdaterStub(t, stub)

	cmd := newRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"update"})

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

func TestUpdateCommandReturnsActionFailOnUpdaterError(t *testing.T) {
	stub := &stubReleaseUpdater{err: errors.New("update failed")}
	withReleaseUpdaterStub(t, stub)

	cmd := newRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"update"})

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

func TestUpdateCommandNamesAndDryRun(t *testing.T) {
	for _, name := range []string{"update", "upd", "up", "upgrade"} {
		t.Run(name, func(t *testing.T) {
			stub := &stubReleaseUpdater{}
			withReleaseUpdaterStub(t, stub)
			cmd := newRootCmd()
			cmd.SetOut(io.Discard)
			cmd.SetArgs([]string{name, "--dry-run"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if stub.runCalls != 1 || !stub.dryRun {
				t.Fatalf("updater calls = %d, dry run = %t", stub.runCalls, stub.dryRun)
			}
		})
	}
}

func TestGitHubReleaseUpdaterReplacesExecutable(t *testing.T) {
	for _, tag := range []string{"v9.9.9", "9.9.9"} {
		t.Run(tag, func(t *testing.T) {
			exeName := "qbitctl"
			if runtime.GOOS == "windows" {
				exeName += ".exe"
			}
			exePath := filepath.Join(t.TempDir(), exeName)
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
						TagName: tag,
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
					if releaseTag != tag {
						t.Fatalf("verify releaseTag = %q, want exact published tag %q", releaseTag, tag)
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
			if runtime.GOOS == "windows" {
				backup, err := os.ReadFile(exePath + ".old")
				if err != nil || string(backup) != "old-binary" {
					t.Fatalf("previous executable backup = %q, error = %v", backup, err)
				}
			} else {
				info, err := os.Stat(exePath)
				if err != nil {
					t.Fatalf("Stat returned error: %v", err)
				}
				if info.Mode().Perm() != 0o755 {
					t.Fatalf("updated executable mode = %#o, want 0755", info.Mode().Perm())
				}
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
		})
	}
}

func TestGitHubReleaseUpdaterWindowsReplacementFailureRestoresExecutable(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "qbitctl.exe")
	oldBody := []byte("old-binary")
	newBody := []byte("new-binary-content")
	if err := os.WriteFile(exePath, oldBody, 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	sum := sha256.Sum256(newBody)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download/qbitctl-windows-amd64.exe" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write(newBody)
	}))
	defer server.Close()

	replacementErr := errors.New("injected replacement failure")
	var renameCalls int
	updater := &githubReleaseUpdater{
		client: server.Client(),
		verifyAsset: func(context.Context, string, string) error {
			return nil
		},
		renameFile: func(oldPath, newPath string) error {
			renameCalls++
			if renameCalls == 2 {
				return replacementErr
			}
			return os.Rename(oldPath, newPath)
		},
		goos:   "windows",
		goarch: "amd64",
	}

	err := updater.downloadAndReplace(
		context.Background(),
		exePath,
		"v9.9.9",
		"qbitctl-windows-amd64.exe",
		server.URL+"/download/qbitctl-windows-amd64.exe",
		hex.EncodeToString(sum[:]),
	)
	if err == nil {
		t.Fatal("downloadAndReplace unexpectedly succeeded")
	}
	if !errors.Is(err, replacementErr) {
		t.Fatalf("downloadAndReplace error = %v, want replacement error", err)
	}
	if !strings.Contains(err.Error(), "previous executable restored") {
		t.Fatalf("downloadAndReplace error = %v, want restore diagnostic", err)
	}
	if renameCalls != 3 {
		t.Fatalf("rename calls = %d, want replacement plus rollback", renameCalls)
	}

	got, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatalf("ReadFile returned error: %v", readErr)
	}
	if string(got) != string(oldBody) {
		t.Fatalf("restored executable content = %q, want %q", string(got), string(oldBody))
	}
	if _, statErr := os.Stat(exePath + ".old"); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("old executable backup stat error = %v, want not-exist", statErr)
	}
}

func TestGitHubReleaseUpdaterWindowsRollbackFailureReportsBothErrors(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "qbitctl.exe")
	oldBody := []byte("old-binary")
	newBody := []byte("new-binary-content")
	if err := os.WriteFile(exePath, oldBody, 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	sum := sha256.Sum256(newBody)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(newBody)
	}))
	defer server.Close()

	replacementErr := errors.New("injected replacement failure")
	rollbackErr := errors.New("injected rollback failure")
	var renameCalls int
	updater := &githubReleaseUpdater{
		client: server.Client(),
		verifyAsset: func(context.Context, string, string) error {
			return nil
		},
		renameFile: func(oldPath, newPath string) error {
			renameCalls++
			switch renameCalls {
			case 2:
				return replacementErr
			case 3:
				return rollbackErr
			default:
				return os.Rename(oldPath, newPath)
			}
		},
		goos:   "windows",
		goarch: "amd64",
	}

	err := updater.downloadAndReplace(
		context.Background(),
		exePath,
		"v9.9.9",
		"qbitctl-windows-amd64.exe",
		server.URL+"/download/qbitctl-windows-amd64.exe",
		hex.EncodeToString(sum[:]),
	)
	if err == nil {
		t.Fatal("downloadAndReplace unexpectedly succeeded")
	}
	if !errors.Is(err, replacementErr) {
		t.Fatalf("downloadAndReplace error = %v, want replacement error", err)
	}
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("downloadAndReplace error = %v, want rollback error", err)
	}
	if !strings.Contains(err.Error(), "rollback failed") || !strings.Contains(err.Error(), rollbackErr.Error()) {
		t.Fatalf("downloadAndReplace error = %v, want rollback diagnostic", err)
	}
	if renameCalls != 3 {
		t.Fatalf("rename calls = %d, want replacement plus rollback", renameCalls)
	}
	if _, statErr := os.Stat(exePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("current executable stat error = %v, want not-exist after injected rollback failure", statErr)
	}
	if _, statErr := os.Stat(exePath + ".old"); statErr != nil {
		t.Fatalf("old executable backup stat error = %v, want retained backup", statErr)
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
