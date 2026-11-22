package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type updateRoundTripper func(*http.Request) (*http.Response, error)

func (f updateRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchLatestReleaseUsesMetadataDeadline(t *testing.T) {
	updater := &githubReleaseUpdater{
		client: &http.Client{Transport: updateRoundTripper(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		})},
		latestReleaseURL: "https://example.test/releases/latest",
		metadataTimeout:  20 * time.Millisecond,
	}
	_, err := updater.fetchLatestRelease(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fetch latest release metadata") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fetchLatestRelease error = %v, want metadata deadline", err)
	}
}

func TestDefaultReleaseUpdaterUsesHeaderTimeoutWithoutBodyTimeout(t *testing.T) {
	updater, ok := newDefaultReleaseUpdater().(*githubReleaseUpdater)
	if !ok {
		t.Fatal("newDefaultReleaseUpdater did not return githubReleaseUpdater")
	}
	if updater.client.Timeout != 0 {
		t.Fatalf("client timeout = %s, want no whole-body timeout", updater.client.Timeout)
	}
	transport, ok := updater.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("updater client does not use an HTTP transport")
	}
	if transport.ResponseHeaderTimeout != defaultMetadataTimeout {
		t.Fatalf("response header timeout = %s, want %s", transport.ResponseHeaderTimeout, defaultMetadataTimeout)
	}
}

func TestFetchChecksumBoundsResponseBody(t *testing.T) {
	updater := &githubReleaseUpdater{
		client: &http.Client{Transport: updateRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxChecksumBytes+1))),
			}, nil
		})},
		metadataTimeout: time.Second,
	}
	_, err := updater.fetchChecksum(context.Background(), "https://example.test/checksum", "qbitctl-linux-amd64")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("fetchChecksum error = %v, want bounded-body error", err)
	}
}

func TestDownloadAndReplaceAllowsSlowSteadyBody(t *testing.T) {
	body := []byte("slow-but-steady-binary")
	chunks := splitUpdateBody(body, 4)
	updater, exePath, checksum := newBodyUpdater(t, chunks, 50*time.Millisecond)
	updater.stallTimeout = 300 * time.Millisecond
	updater.downloadTimeout = 2 * time.Second

	if err := updater.downloadAndReplace(context.Background(), exePath, "v1.0.0", "qbitctl-darwin-arm64", "https://example.test/download", checksum); err != nil {
		t.Fatalf("downloadAndReplace returned error: %v", err)
	}
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("downloaded body = %q, want %q", got, body)
	}
}

func TestDownloadAndReplaceReportsStallAndRemovesTemporaryFiles(t *testing.T) {
	body := []byte("partial-binary")
	updater, exePath, checksum := newBodyUpdaterWithDelays(t, [][]byte{body[:4], body[4:]}, []time.Duration{0, 200 * time.Millisecond})
	updater.stallTimeout = 40 * time.Millisecond
	updater.downloadTimeout = time.Second

	err := updater.downloadAndReplace(context.Background(), exePath, "v1.0.0", "qbitctl-darwin-arm64", "https://example.test/download", checksum)
	if err == nil || !strings.Contains(err.Error(), "no data received for 40ms") {
		t.Fatalf("downloadAndReplace error = %v, want stall diagnostic", err)
	}
	assertOriginalExecutableAndNoTempDir(t, exePath)
}

func TestDownloadAndReplaceReportsOverallDownloadDeadline(t *testing.T) {
	body := []byte("overall-deadline-binary")
	updater, exePath, checksum := newBodyUpdater(t, splitUpdateBody(body, 3), 50*time.Millisecond)
	updater.stallTimeout = 300 * time.Millisecond
	updater.downloadTimeout = 80 * time.Millisecond

	err := updater.downloadAndReplace(context.Background(), exePath, "v1.0.0", "qbitctl-darwin-arm64", "https://example.test/download", checksum)
	if err == nil || !strings.Contains(err.Error(), "exceeded 80ms") {
		t.Fatalf("downloadAndReplace error = %v, want overall deadline diagnostic", err)
	}
	assertOriginalExecutableAndNoTempDir(t, exePath)
}

func TestDownloadAndReplaceDoesNotReplaceAfterVerificationCancellation(t *testing.T) {
	body := []byte("verified-binary")
	updater, exePath, checksum := newBodyUpdater(t, [][]byte{body}, 0)
	cancelCtx, cancel := context.WithCancel(context.Background())
	updater.verifyAsset = func(context.Context, string, string) error {
		cancel()
		return nil
	}

	err := updater.downloadAndReplace(cancelCtx, exePath, "v1.0.0", "qbitctl-darwin-arm64", "https://example.test/download", checksum)
	if err == nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "replace downloaded release asset") {
		t.Fatalf("downloadAndReplace error = %v, want cancellation before replacement", err)
	}
	assertOriginalExecutableAndNoTempDir(t, exePath)
}

type updateBody struct {
	ctx    context.Context
	chunks [][]byte
	delays []time.Duration
	index  int
}

func (b *updateBody) Read(p []byte) (int, error) {
	if b.index >= len(b.chunks) {
		return 0, io.EOF
	}
	delay := time.Duration(0)
	if b.index < len(b.delays) {
		delay = b.delays[b.index]
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-b.ctx.Done():
			return 0, b.ctx.Err()
		}
	}
	chunk := b.chunks[b.index]
	b.index++
	return copy(p, chunk), nil
}

func (b *updateBody) Close() error { return nil }

func newBodyUpdater(t *testing.T, chunks [][]byte, delay time.Duration) (*githubReleaseUpdater, string, string) {
	delays := make([]time.Duration, len(chunks))
	for i := range delays {
		delays[i] = delay
	}
	return newBodyUpdaterWithDelays(t, chunks, delays)
}

func newBodyUpdaterWithDelays(t *testing.T, chunks [][]byte, delays []time.Duration) (*githubReleaseUpdater, string, string) {
	t.Helper()
	dir := t.TempDir()
	exePath := filepath.Join(dir, "qbitctl")
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	all := bytesFromChunks(chunks)
	sum := sha256.Sum256(all)
	updater := &githubReleaseUpdater{
		client: &http.Client{Transport: updateRoundTripper(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: &updateBody{ctx: req.Context(), chunks: chunks, delays: delays}, Request: req}, nil
		})},
		verifyAsset: func(context.Context, string, string) error { return nil },
		goos:        "darwin",
		goarch:      "arm64",
	}
	return updater, exePath, hex.EncodeToString(sum[:])
}

func splitUpdateBody(body []byte, parts int) [][]byte {
	chunks := make([][]byte, 0, parts)
	for i := 0; i < parts; i++ {
		start := len(body) * i / parts
		end := len(body) * (i + 1) / parts
		chunks = append(chunks, body[start:end])
	}
	return chunks
}

func bytesFromChunks(chunks [][]byte) []byte {
	var body []byte
	for _, chunk := range chunks {
		body = append(body, chunk...)
	}
	return body
}

func assertOriginalExecutableAndNoTempDir(t *testing.T, exePath string) {
	t.Helper()
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-binary" {
		t.Fatalf("executable content = %q, want old-binary", got)
	}
	entries, err := os.ReadDir(filepath.Dir(exePath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".qbitctl-update-") {
			t.Fatalf("temporary update directory remains: %s", entry.Name())
		}
	}
}
