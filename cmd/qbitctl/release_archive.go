package main

import (
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxExtractedBinaryBytes bounds decompression so a malformed or hostile
// archive cannot fill the disk. Release binaries are about 15 MiB.
var maxExtractedBinaryBytes int64 = 256 << 20

// compressedAssetName is the compressed release asset the updater prefers:
// a zip on Windows and gzip elsewhere, since both decompress with Go's
// standard library. Releases also publish .xz assets for the installer.
func compressedAssetName(goos, goarch string) string {
	base := fmt.Sprintf("qbitctl-%s-%s", goos, goarch)
	if goos == "windows" {
		return base + ".zip"
	}
	return base + ".gz"
}

// downloadForRuntime prefers the compressed asset when the release also
// publishes its checksum, and otherwise falls back to the raw binary that
// every release, including those before compressed assets, provides.
func (r githubRelease) downloadForRuntime(goos, goarch string) (githubReleaseAsset, githubReleaseAsset, error) {
	name := compressedAssetName(goos, goarch)
	if archive, ok := findReleaseAsset(r.Assets, name); ok {
		if checksum, ok := findReleaseAsset(r.Assets, name+".sha256"); ok {
			return archive, checksum, nil
		}
	}
	return r.assetsForRuntime(goos, goarch)
}

// extractReleaseBinary returns the path of the executable to install. A raw
// binary asset is returned unchanged. The archive must already have passed
// checksum and attestation verification.
func extractReleaseBinary(assetPath, assetName, goos string) (string, error) {
	switch {
	case strings.HasSuffix(assetName, ".gz"):
		return extractGzipBinary(assetPath)
	case strings.HasSuffix(assetName, ".zip"):
		return extractZipBinary(assetPath, goos)
	default:
		return assetPath, nil
	}
}

func extractGzipBinary(path string) (string, error) {
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	reader, err := gzip.NewReader(in)
	if err != nil {
		return "", fmt.Errorf("open gzip archive: %w", err)
	}
	defer reader.Close()
	// Reading to EOF makes the gzip reader verify the stream's CRC-32.
	target := strings.TrimSuffix(path, ".gz") + ".extracted"
	if err := writeExtractedBinary(target, reader); err != nil {
		return "", fmt.Errorf("decompress gzip archive: %w", err)
	}
	return target, nil
}

func extractZipBinary(path, goos string) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open zip archive: %w", err)
	}
	defer archive.Close()

	want := "qbitctl"
	if goos == "windows" {
		want = "qbitctl.exe"
	}
	// Accept exactly the one expected file, so an archive cannot smuggle in
	// a different executable or a path outside the temporary directory.
	if len(archive.File) != 1 || archive.File[0].Name != want || !archive.File[0].Mode().IsRegular() {
		return "", fmt.Errorf("zip archive must contain only %s", want)
	}
	entry := archive.File[0]
	if entry.UncompressedSize64 > uint64(maxExtractedBinaryBytes) {
		return "", fmt.Errorf("zip entry %s exceeds %d bytes", want, maxExtractedBinaryBytes)
	}
	reader, err := entry.Open()
	if err != nil {
		return "", fmt.Errorf("open zip entry %s: %w", want, err)
	}
	defer reader.Close()
	// The zip reader verifies the entry's CRC-32 when it reaches EOF.
	target := filepath.Join(filepath.Dir(path), want+".extracted")
	if err := writeExtractedBinary(target, reader); err != nil {
		return "", fmt.Errorf("decompress zip entry %s: %w", want, err)
	}
	return target, nil
}

func writeExtractedBinary(target string, reader io.Reader) error {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(reader, maxExtractedBinaryBytes+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	switch {
	case copyErr != nil:
		return copyErr
	case written > maxExtractedBinaryBytes:
		return fmt.Errorf("decompressed binary exceeds %d bytes", maxExtractedBinaryBytes)
	case written == 0:
		return errors.New("archive contains an empty binary")
	case syncErr != nil:
		return syncErr
	default:
		return closeErr
	}
}
