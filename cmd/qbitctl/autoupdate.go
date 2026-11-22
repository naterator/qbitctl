package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const githubLatestReleaseURL = githubReleaseAPI + "/releases/latest"

const (
	defaultMetadataTimeout  = 30 * time.Second
	defaultTrustRootTimeout = 60 * time.Second
	defaultStallTimeout     = 60 * time.Second
	defaultDownloadTimeout  = 15 * time.Minute
	maxReleaseMetadataBytes = 8 << 20
	maxChecksumBytes        = 64 << 10
)

var (
	errDownloadStalled  = errors.New("no data received")
	errDownloadExceeded = errors.New("download deadline exceeded")
)

type releaseUpdater interface {
	Run(ctx context.Context, currentVersion string, dryRun bool, stdout io.Writer) error
}

type githubReleaseUpdater struct {
	client                 *http.Client
	latestReleaseURL       string
	executablePath         func() (string, error)
	verifyAsset            func(context.Context, string, string) error
	newAttestationVerifier func(context.Context, *http.Client) (attestationVerifier, error)
	renameFile             func(string, string) error
	goos                   string
	goarch                 string
	metadataTimeout        time.Duration
	trustRootTimeout       time.Duration
	stallTimeout           time.Duration
	downloadTimeout        time.Duration
}

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var makeReleaseUpdater = newDefaultReleaseUpdater

func newDefaultReleaseUpdater() releaseUpdater {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		transport = &http.Transport{}
	}
	transport = transport.Clone()
	transport.ResponseHeaderTimeout = defaultMetadataTimeout
	return &githubReleaseUpdater{
		client: &http.Client{
			Transport: transport,
		},
		latestReleaseURL: githubLatestReleaseURL,
		executablePath:   os.Executable,
		goos:             runtime.GOOS,
		goarch:           runtime.GOARCH,
	}
}

func (u *githubReleaseUpdater) Run(ctx context.Context, currentVersion string, dryRun bool, stdout io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdout == nil {
		stdout = io.Discard
	}

	release, err := u.fetchLatestRelease(ctx)
	if err != nil {
		return err
	}

	latestVersion, err := normalizeSemver(release.TagName)
	if err != nil {
		return fmt.Errorf("latest release tag %q is not valid semver: %w", release.TagName, err)
	}

	currentVersionText := currentVersion
	if currentVersionText == "" {
		currentVersionText = "unknown"
	}
	currentVersionNormalized, currentVersionKnown := normalizeSemverLoose(currentVersion)
	if currentVersionKnown {
		switch compareSemver(currentVersionNormalized, latestVersion) {
		case 1:
			fmt.Fprintf(stdout, "qbitctl %s is newer than published release %s; skipping\n", currentVersionText, latestVersion)
			return nil
		case 0:
			fmt.Fprintf(stdout, "qbitctl %s is already up to date\n", currentVersionText)
			return nil
		}
	}

	binaryAsset, checksumAsset, err := release.downloadForRuntime(u.goos, u.goarch)
	if err != nil {
		return err
	}

	exePath, err := u.executablePath()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	if resolvedPath, resolveErr := filepath.EvalSymlinks(exePath); resolveErr == nil {
		exePath = resolvedPath
	}

	expectedChecksum, err := u.fetchChecksum(ctx, checksumAsset.BrowserDownloadURL, binaryAsset.Name)
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Fprintf(stdout, "Update available: %s -> %s\n", currentVersionText, latestVersion)
		return nil
	}

	fmt.Fprintf(stdout, "Updating qbitctl from %s to %s\n", currentVersionText, latestVersion)
	// Use the exact published tag when verifying the release attestation.
	if err := u.downloadAndReplace(ctx, exePath, release.TagName, binaryAsset.Name, binaryAsset.BrowserDownloadURL, expectedChecksum); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Updated qbitctl to %s\n", latestVersion)
	return nil
}

func (u *githubReleaseUpdater) fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	body, err := u.getMetadataBody(ctx, u.latestReleaseURL, maxReleaseMetadataBytes)
	if err != nil {
		return githubRelease{}, fmt.Errorf("fetch latest release metadata: %w", err)
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return githubRelease{}, fmt.Errorf("decode latest release metadata: %w", err)
	}
	if release.TagName == "" {
		return githubRelease{}, fmt.Errorf("latest release metadata did not include a tag name")
	}
	return release, nil
}

func (u *githubReleaseUpdater) fetchChecksum(ctx context.Context, checksumURL, binaryAssetName string) (string, error) {
	body, err := u.getMetadataBody(ctx, checksumURL, maxChecksumBytes)
	if err != nil {
		return "", fmt.Errorf("download checksum asset %q: %w", binaryAssetName+".sha256", err)
	}

	checksum, err := parseSHA256File(string(body), binaryAssetName)
	if err != nil {
		return "", fmt.Errorf("parse checksum asset %q: %w", binaryAssetName+".sha256", err)
	}
	return checksum, nil
}

func (u *githubReleaseUpdater) downloadAndReplace(ctx context.Context, exePath, releaseTag, assetName, assetURL, expectedChecksum string) error {
	info, err := os.Stat(exePath)
	if err != nil {
		return fmt.Errorf("stat current executable %q: %w", exePath, err)
	}

	dir := filepath.Dir(exePath)
	tempDir, err := os.MkdirTemp(dir, ".qbitctl-update-*")
	if err != nil {
		return fmt.Errorf("create temporary download directory: %w", err)
	}
	tempPath := filepath.Join(tempDir, assetName)

	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.RemoveAll(tempDir)
		}
	}()

	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("create temporary download file: %w", err)
	}
	defer tempFile.Close()

	downloadCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	downloadDuration := u.downloadTimeout
	if downloadDuration <= 0 {
		downloadDuration = defaultDownloadTimeout
	}
	stallDuration := u.stallTimeout
	if stallDuration <= 0 {
		stallDuration = defaultStallTimeout
	}
	overallTimer := time.AfterFunc(downloadDuration, func() {
		cancel(errDownloadExceeded)
	})
	defer overallTimer.Stop()

	resp, err := u.get(downloadCtx, assetURL)
	if err != nil {
		return fmt.Errorf("download release asset: %w", downloadError(downloadCtx, err, stallDuration, downloadDuration))
	}
	defer resp.Body.Close()

	hash := sha256.New()
	reader := newStallReader(resp.Body, stallDuration, cancel)
	defer reader.Close()
	if _, err := io.Copy(io.MultiWriter(tempFile, hash), reader); err != nil {
		return fmt.Errorf("write downloaded release asset: %w", downloadError(downloadCtx, err, stallDuration, downloadDuration))
	}
	reader.Close()
	overallTimer.Stop()
	if cause := context.Cause(downloadCtx); cause != nil {
		return fmt.Errorf("write downloaded release asset: %w", downloadError(downloadCtx, cause, stallDuration, downloadDuration))
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("flush downloaded release asset: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close downloaded release asset: %w", err)
	}

	actualChecksum := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actualChecksum, expectedChecksum) {
		return fmt.Errorf("checksum mismatch for downloaded binary: got %s want %s", actualChecksum, expectedChecksum)
	}

	var verificationErr error
	if u.verifyAsset == nil {
		verificationErr = u.verifyReleaseAttestation(ctx, releaseTag, assetName, actualChecksum)
	} else {
		verificationErr = u.verifyAsset(ctx, releaseTag, tempPath)
	}
	if verificationErr != nil {
		return fmt.Errorf("verify release asset attestation: %w", verificationErr)
	}

	// Decompress only after the downloaded archive passed both the checksum
	// and the attestation, so no unverified bytes are ever unpacked.
	installPath, err := extractReleaseBinary(tempPath, assetName, u.goos)
	if err != nil {
		return fmt.Errorf("extract release asset %q: %w", assetName, err)
	}

	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o755
	}
	if err := os.Chmod(installPath, mode); err != nil {
		return fmt.Errorf("mark downloaded binary executable: %w", err)
	}
	if cause := context.Cause(ctx); cause != nil {
		return fmt.Errorf("replace downloaded release asset: %w", cause)
	}
	renameFile := u.renameFile
	if renameFile == nil {
		renameFile = os.Rename
	}

	// On Windows the OS locks a running executable, preventing overwrite.
	// Rename the old binary out of the way first, then move the new one in.
	if strings.EqualFold(u.goos, "windows") {
		oldPath := exePath + ".old"
		_ = os.Remove(oldPath)
		if err := renameFile(exePath, oldPath); err != nil {
			return fmt.Errorf("rename current executable out of the way: %w", err)
		}
		if err := renameFile(installPath, exePath); err != nil {
			if rollbackErr := renameFile(oldPath, exePath); rollbackErr != nil {
				return fmt.Errorf("replace current executable %q: %w (rollback failed: %w)", exePath, err, rollbackErr)
			}
			return fmt.Errorf("replace current executable %q: %w (previous executable restored)", exePath, err)
		}
	} else if err := renameFile(installPath, exePath); err != nil {
		return fmt.Errorf("replace current executable %q: %w", exePath, err)
	}
	_ = os.RemoveAll(tempDir)
	removeTemp = false
	return nil
}

func (u *githubReleaseUpdater) metadataDuration() time.Duration {
	if u.metadataTimeout > 0 {
		return u.metadataTimeout
	}
	return defaultMetadataTimeout
}

func (u *githubReleaseUpdater) trustRootDuration() time.Duration {
	if u.trustRootTimeout > 0 {
		return u.trustRootTimeout
	}
	return defaultTrustRootTimeout
}

func (u *githubReleaseUpdater) getMetadataBody(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	metadataCtx, cancel := context.WithTimeout(ctx, u.metadataDuration())
	defer cancel()
	resp, err := u.get(metadataCtx, endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return body, nil
}

func downloadError(ctx context.Context, err error, stallDuration, downloadDuration time.Duration) error {
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, errDownloadStalled):
		return fmt.Errorf("no data received for %s: %w", stallDuration, errDownloadStalled)
	case errors.Is(cause, errDownloadExceeded):
		return fmt.Errorf("exceeded %s: %w", downloadDuration, errDownloadExceeded)
	default:
		return err
	}
}

type stallReader struct {
	reader     io.Reader
	cancel     context.CancelCauseFunc
	timer      *time.Timer
	timeout    time.Duration
	generation uint64
	closed     bool
	mu         sync.Mutex
}

func newStallReader(reader io.Reader, timeout time.Duration, cancel context.CancelCauseFunc) *stallReader {
	s := &stallReader{reader: reader, cancel: cancel, timeout: timeout}
	s.mu.Lock()
	s.scheduleLocked()
	s.mu.Unlock()
	return s
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.reader.Read(p)
	if n > 0 {
		s.mu.Lock()
		s.generation++
		if s.timer != nil {
			s.timer.Stop()
		}
		s.scheduleLocked()
		s.mu.Unlock()
	}
	return n, err
}

func (s *stallReader) Close() {
	s.mu.Lock()
	s.closed = true
	s.generation++
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
}

func (s *stallReader) scheduleLocked() {
	generation := s.generation
	s.timer = time.AfterFunc(s.timeout, func() {
		s.mu.Lock()
		if !s.closed && s.generation == generation {
			s.cancel(errDownloadStalled)
		}
		s.mu.Unlock()
	})
}

func (u *githubReleaseUpdater) get(ctx context.Context, url string) (*http.Response, error) {
	client := u.client
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "qbitctl/"+qbitctlVersion)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return nil, fmt.Errorf("GET %s returned %s: %s", url, resp.Status, message)
	}
	return resp, nil
}

func (r githubRelease) assetsForRuntime(goos, goarch string) (githubReleaseAsset, githubReleaseAsset, error) {
	binaryCandidates, checksumCandidates := releaseAssetCandidates(goos, goarch)
	binaryAsset, ok := findReleaseAsset(r.Assets, binaryCandidates...)
	if !ok {
		return githubReleaseAsset{}, githubReleaseAsset{}, fmt.Errorf("latest release does not include a binary for %s/%s", goos, goarch)
	}
	checksumAsset, ok := findReleaseAsset(r.Assets, checksumCandidates...)
	if !ok {
		return githubReleaseAsset{}, githubReleaseAsset{}, fmt.Errorf("latest release does not include a checksum for %s", binaryAsset.Name)
	}
	return binaryAsset, checksumAsset, nil
}

func releaseAssetCandidates(goos, goarch string) ([]string, []string) {
	base := fmt.Sprintf("qbitctl-%s-%s", goos, goarch)
	binaryCandidates := []string{base}
	if goos == "windows" {
		binaryCandidates = append([]string{base + ".exe"}, binaryCandidates...)
	}

	seen := make(map[string]struct{}, len(binaryCandidates))
	checksumCandidates := make([]string, 0, len(binaryCandidates))
	for _, name := range binaryCandidates {
		checksumName := name + ".sha256"
		if _, ok := seen[checksumName]; ok {
			continue
		}
		seen[checksumName] = struct{}{}
		checksumCandidates = append(checksumCandidates, checksumName)
	}
	return binaryCandidates, checksumCandidates
}

func findReleaseAsset(assets []githubReleaseAsset, names ...string) (githubReleaseAsset, bool) {
	for _, name := range names {
		for _, asset := range assets {
			if asset.Name == name {
				return asset, true
			}
		}
	}
	return githubReleaseAsset{}, false
}

func parseSHA256File(content, assetName string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	unnamedChecksums := make([]string, 0, 1)

	for scanner.Scan() {
		checksum, namedAsset, ok := parseSHA256Line(scanner.Text())
		if !ok {
			continue
		}
		if namedAsset == "" {
			unnamedChecksums = append(unnamedChecksums, checksum)
			continue
		}
		if namedAsset == assetName {
			return checksum, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if len(unnamedChecksums) == 1 {
		return unnamedChecksums[0], nil
	}
	if len(unnamedChecksums) > 1 {
		return "", fmt.Errorf("checksum file contained multiple unnamed digests")
	}
	return "", fmt.Errorf("checksum for %s not found", assetName)
}

func parseSHA256Line(line string) (string, string, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) > 0 && isHexSHA256(fields[0]) {
		name := ""
		if len(fields) > 1 {
			name = strings.TrimPrefix(fields[len(fields)-1], "*")
		}
		return strings.ToLower(fields[0]), name, true
	}

	if idx := strings.LastIndex(line, "="); idx >= 0 {
		candidate := strings.TrimSpace(line[idx+1:])
		if isHexSHA256(candidate) {
			return strings.ToLower(candidate), "", true
		}
	}

	return "", "", false
}

func isHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func normalizeSemverLoose(version string) (string, bool) {
	normalized, err := normalizeSemver(version)
	if err != nil {
		return "", false
	}
	return normalized, true
}

func normalizeSemver(version string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("version is empty")
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	parts := strings.Split(version[1:], ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("version must use vMAJOR.MINOR.PATCH")
	}
	for _, part := range parts {
		if part == "" {
			return "", fmt.Errorf("version must use vMAJOR.MINOR.PATCH")
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return "", fmt.Errorf("version must use vMAJOR.MINOR.PATCH")
			}
		}
	}
	return version, nil
}

func compareSemver(a, b string) int {
	aParts := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bParts := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < 3; i++ {
		an, _ := strconv.Atoi(aParts[i])
		bn, _ := strconv.Atoi(bParts[i])
		switch {
		case an > bn:
			return 1
		case an < bn:
			return -1
		}
	}
	return 0
}
