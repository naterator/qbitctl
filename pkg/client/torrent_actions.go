package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// qBittorrent 5.0's request parser caps the complete POST body at 64 MiB.
// Leave room for multipart headers and fields while keeping local reads
// bounded before they enter the request body.
const (
	maxTorrentRequestSize int64 = 64 << 20
	maxTorrentFileSize          = maxTorrentRequestSize - (1 << 20)
)

var errTorrentFileTooLarge = errors.New("torrent file exceeds the 63 MiB upload limit")

// AddTorrentOptions controls the settings qBittorrent applies while adding a
// torrent. Empty strings, nil limits, and false Set flags use qBittorrent's
// defaults. A stopped torrent is opt-in so the zero value retains the normal
// auto-start behavior.
type AddTorrentOptions struct {
	Category string
	SavePath string
	Tags     string
	Stopped  bool

	// ContentLayout controls qBittorrent's placement of files inside the
	// download. Accepted values are Original, Subfolder, and NoSubfolder.
	ContentLayout string
	// DownloadPath is the separate incomplete/download path. When set,
	// qBittorrent receives useDownloadPath=true and auto management is disabled.
	DownloadPath string

	// SkipCheckingSet distinguishes an explicitly selected false value from an
	// omitted value. qBittorrent 5.3 renamed skip_checking to seedMode; the
	// client selects the correct wire key from the verified server version.
	SkipChecking    bool
	SkipCheckingSet bool
	StopCondition   string

	SequentialDownload        bool
	SequentialDownloadSet     bool
	FirstLastPiecePriority    bool
	FirstLastPiecePrioritySet bool

	// Share time limits are qBittorrent API minutes. -1 means unlimited and -2
	// means the global setting. Pointers preserve an omitted value.
	ShareRatioLimit               *float64
	ShareSeedingTimeLimit         *int64
	ShareInactiveSeedingTimeLimit *int64
	ShareLimitsMode               string
	ShareLimitAction              string
}

func (options AddTorrentOptions) hasOverrides() bool {
	return strings.TrimSpace(options.Category) != "" ||
		strings.TrimSpace(options.SavePath) != "" ||
		strings.TrimSpace(options.Tags) != "" ||
		options.Stopped ||
		options.hasAdvancedOverrides()
}

type torrentFileUpload struct {
	path          string
	size          int64
	prefix        []byte
	suffix        []byte
	contentType   string
	contentLength int64
}

func (a *App) qbtAction(hash, endpoint string, params url.Values) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("hashes", hash)
	if _, err := a.client.requestContext(a.context(), http.MethodPost, endpoint, params); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Request failed for %s: %v", endpoint, err)
	}
	return nil
}

func (a *App) PauseTorrent(hash string) error {
	return a.qbtAction(hash, "/api/v2/torrents/stop", nil)
}

func (a *App) StartTorrent(hash string) error {
	return a.qbtAction(hash, "/api/v2/torrents/start", nil)
}

func (a *App) ForceStartTorrent(hash string) error {
	return a.qbtAction(hash, "/api/v2/torrents/setForceStart", url.Values{"value": {"true"}})
}

func (a *App) MoveTorrent(hash, path string) error {
	if path == "" {
		return codedErrf(ExitBadArgs, "Invalid move destination")
	}
	return a.qbtAction(hash, "/api/v2/torrents/setLocation", url.Values{"location": {path}})
}

func (a *App) StopAndRemoveTorrent(hash string, deleteFiles bool) error {
	return a.qbtAction(hash, "/api/v2/torrents/delete", url.Values{"deleteFiles": {strconv.FormatBool(deleteFiles)}})
}

func (a *App) AddTorrent(source string) error {
	return a.AddTorrentWithOptions(source, AddTorrentOptions{})
}

// AddTorrentWithOptions adds a local torrent file, magnet link, or torrent URL
// with the requested qBittorrent transfer options.
func (a *App) AddTorrentWithOptions(source string, options AddTorrentOptions) error {
	if source == "" {
		return codedErrf(ExitBadArgs, "Torrent file path or magnet link required")
	}
	normalized, err := NormalizeAddTorrentOptions(options)
	if err != nil {
		return codedErrf(ExitBadArgs, "Invalid add option: %v", err)
	}
	if err := a.validateAddTorrentOptions(normalized); err != nil {
		return err
	}
	if looksLikeTorrentSourceURL(source) {
		return a.addTorrentURL(source, normalized, "")
	}
	return a.addTorrentFile(source, normalized)
}

// AddSearchResult submits a cached search result to qBittorrent.
func (a *App) AddSearchResult(id int64) error {
	return a.AddSearchResultWithOptions(id, AddTorrentOptions{})
}

// AddSearchResultWithOptions submits a cached search result to qBittorrent,
// applying the same add-time options as a direct torrent add. qBittorrent
// versions before 5.2 only expose the legacy search download endpoint, which
// cannot accept those options.
func (a *App) AddSearchResultWithOptions(id int64, options AddTorrentOptions) error {
	target, err := a.cachedSearchResult(id)
	if err != nil {
		return err
	}
	normalized, err := NormalizeAddTorrentOptions(options)
	if err != nil {
		return codedErrf(ExitBadArgs, "Invalid add option: %v", err)
	}
	if target.Plugin == "" || !(strings.HasPrefix(target.URL, "http:") || strings.HasPrefix(target.URL, "https:")) {
		if err := a.validateAddTorrentOptions(normalized); err != nil {
			return err
		}
		return a.addTorrentURL(target.URL, normalized, "")
	}
	version, err := a.fetchAppVersion()
	if err != nil {
		return err
	}
	if a.compatibilitySource == compatibilitySourceDisk && compareDottedVersion(version, "5.2.0") < 0 {
		// A cached legacy version can outlive a server upgrade. Check before
		// choosing the legacy download path or rejecting add-time options.
		if err := a.refreshCompatibility(); err != nil {
			return err
		}
		version = a.appVersion
	}
	if a.compatibilitySource == compatibilitySourceDisk && (normalized.SkipCheckingSet || normalized.ShareLimitsMode != "") {
		if err := a.refreshCompatibility(); err != nil {
			return err
		}
		version = a.appVersion
	}
	if err := validateAddTorrentOptionsForVersion(normalized, version); err != nil {
		return err
	}
	if compareDottedVersion(version, "5.2.0") >= 0 {
		// The newer Web UI cancels its pending plugin download when the
		// session logs out. Fetch metadata first so add completes in-session.
		if err := a.waitForSearchMetadata(target); err != nil {
			return codedErrWrapf(ExitActionFail, err, "Failed to fetch metadata for search result %d: %v", id, err)
		}
		return a.addTorrentURLForVersion(target.URL, normalized, target.Plugin, version)
	}
	if normalized.hasOverrides() {
		return codedErrf(ExitActionFail, "search-result add options require qBittorrent 5.2.0 or newer")
	}
	_, err = a.client.requestContext(a.context(), http.MethodPost, "/api/v2/search/downloadTorrent", url.Values{
		"torrentUrl": {target.URL},
		"pluginName": {target.Plugin},
	})
	if err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to submit search result %d: %v", id, err)
	}
	return nil
}

func (a *App) waitForSearchMetadata(target cachedSearchTarget) error {
	ctx, cancel := context.WithTimeout(a.context(), 60*time.Second)
	defer cancel()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	// The metadata endpoint percent-decodes source after parsing the form.
	source := strings.ReplaceAll(url.QueryEscape(target.URL), "+", "%20")
	params := url.Values{"source": {source}, "downloader": {target.Plugin}}
	for {
		_, status, err := a.client.requestWithStatusContext(ctx, http.MethodPost, "/api/v2/torrents/fetchMetadata", params)
		if err != nil {
			return err
		}
		switch status {
		case http.StatusOK:
			return nil
		case http.StatusAccepted:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-poll.C:
			}
		default:
			return fmt.Errorf("unexpected metadata HTTP status %d", status)
		}
	}
}

func (a *App) addTorrentURL(source string, options AddTorrentOptions, downloader string) error {
	return a.addTorrentURLForVersion(source, options, downloader, a.appVersion)
}

func (a *App) addTorrentURLForVersion(source string, options AddTorrentOptions, downloader, version string) error {
	var body strings.Builder
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("urls", source); err != nil {
		return codedErrf(ExitActionFail, "Failed to build add request")
	}
	if downloader != "" {
		if err := writer.WriteField("downloader", downloader); err != nil {
			return codedErrf(ExitActionFail, "Failed to build add request")
		}
	}
	if err := writeTorrentAddOptions(writer, options, version); err != nil {
		return codedErrf(ExitActionFail, "Failed to build add request")
	}
	if err := writer.Close(); err != nil {
		return codedErrf(ExitActionFail, "Failed to finalize add request")
	}

	response, err := a.client.requestBodyContext(a.context(), http.MethodPost, "/api/v2/torrents/add", []byte(body.String()), writer.FormDataContentType())
	if err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to add torrent from URL or magnet link: %v", err)
	}
	if torrentAddRejected(response) {
		return codedErrf(ExitActionFail, "Failed to add torrent from URL or magnet link: qBittorrent rejected the request")
	}
	if downloader != "" {
		var result struct {
			SuccessCount int `json:"success_count"`
		}
		if json.Unmarshal(response, &result) != nil || result.SuccessCount != 1 {
			return codedErrf(ExitActionFail, "qBittorrent did not confirm adding the search result")
		}
	}
	return nil
}

func (a *App) addTorrentFile(path string, options AddTorrentOptions) error {
	ctx := a.context()
	if err := ctx.Err(); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to add torrent file '%s': %v", path, err)
	}
	upload, err := newTorrentFileUpload(path, options, a.appVersion)
	if err != nil {
		return codedErrf(ExitFile, "Failed to read torrent file '%s': %v", path, err)
	}

	response, err := a.client.requestWithBodyContext(ctx, func() ([]byte, *http.Response, error) {
		return a.client.doMultipartStreamRequestContext(ctx, "/api/v2/torrents/add", upload)
	})
	if err != nil {
		var fileErr *torrentFileError
		if errors.As(err, &fileErr) {
			return codedErrf(ExitFile, "Failed to read torrent file '%s': %v", path, fileErr.err)
		}
		return codedErrWrapf(ExitActionFail, err, "Failed to add torrent file '%s': %v", path, err)
	}
	if torrentAddRejected(response) {
		return codedErrf(ExitActionFail, "Failed to add torrent file '%s': qBittorrent rejected the request", path)
	}
	return nil
}

func writeTorrentAddOptions(writer *multipart.Writer, options AddTorrentOptions, version string) error {
	if strings.TrimSpace(options.Category) != "" {
		if err := writer.WriteField("category", options.Category); err != nil {
			return err
		}
	}
	if strings.TrimSpace(options.SavePath) != "" {
		if err := writer.WriteField("savepath", options.SavePath); err != nil {
			return err
		}
	}
	if strings.TrimSpace(options.DownloadPath) != "" {
		if err := writer.WriteField("downloadPath", options.DownloadPath); err != nil {
			return err
		}
		if err := writer.WriteField("useDownloadPath", "true"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(options.SavePath) != "" || strings.TrimSpace(options.DownloadPath) != "" {
		// qBittorrent's auto-managed category path can otherwise override an
		// explicitly requested destination on supported 5.x servers.
		if err := writer.WriteField("autoTMM", "false"); err != nil {
			return err
		}
	}
	if options.ContentLayout != "" {
		if err := writer.WriteField("contentLayout", options.ContentLayout); err != nil {
			return err
		}
	}
	if strings.TrimSpace(options.Tags) != "" {
		if err := writer.WriteField("tags", options.Tags); err != nil {
			return err
		}
	}
	if options.Stopped {
		if err := writer.WriteField("stopped", "true"); err != nil {
			return err
		}
	}
	if options.SkipCheckingSet {
		field := "skip_checking"
		if compareDottedVersion(version, "5.3.0") >= 0 {
			// qBittorrent 5.3 renamed this add parameter to seedMode while
			// retaining the same boolean behavior.
			field = "seedMode"
		}
		if err := writer.WriteField(field, formatBool(options.SkipChecking)); err != nil {
			return err
		}
	}
	if options.StopCondition != "" {
		if err := writer.WriteField("stopCondition", options.StopCondition); err != nil {
			return err
		}
	}
	if options.SequentialDownloadSet {
		if err := writer.WriteField("sequentialDownload", formatBool(options.SequentialDownload)); err != nil {
			return err
		}
	}
	if options.FirstLastPiecePrioritySet {
		if err := writer.WriteField("firstLastPiecePrio", formatBool(options.FirstLastPiecePriority)); err != nil {
			return err
		}
	}
	if options.ShareRatioLimit != nil {
		if err := writer.WriteField("ratioLimit", strconv.FormatFloat(*options.ShareRatioLimit, 'f', 6, 64)); err != nil {
			return err
		}
	}
	if options.ShareSeedingTimeLimit != nil {
		if err := writer.WriteField("seedingTimeLimit", strconv.FormatInt(*options.ShareSeedingTimeLimit, 10)); err != nil {
			return err
		}
	}
	if options.ShareInactiveSeedingTimeLimit != nil {
		if err := writer.WriteField("inactiveSeedingTimeLimit", strconv.FormatInt(*options.ShareInactiveSeedingTimeLimit, 10)); err != nil {
			return err
		}
	}
	if options.ShareLimitsMode != "" {
		if err := writer.WriteField("shareLimitsMode", options.ShareLimitsMode); err != nil {
			return err
		}
	}
	if options.ShareLimitAction != "" {
		if err := writer.WriteField("shareLimitAction", options.ShareLimitAction); err != nil {
			return err
		}
	}
	return nil
}

type torrentFileError struct {
	err error
}

func (e *torrentFileError) Error() string { return e.err.Error() }
func (e *torrentFileError) Unwrap() error { return e.err }

func torrentFileInfo(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("torrent path is not a regular file")
	}
	if info.Size() > maxTorrentFileSize {
		return nil, errTorrentFileTooLarge
	}
	return info, nil
}

func newTorrentFileUpload(path string, options AddTorrentOptions, version string) (*torrentFileUpload, error) {
	info, err := torrentFileInfo(path)
	if err != nil {
		return nil, err
	}

	var framing bytes.Buffer
	writer := multipart.NewWriter(&framing)
	if err := writeTorrentAddOptions(writer, options, version); err != nil {
		return nil, err
	}
	if _, err := writer.CreateFormFile("torrents", filepath.Base(path)); err != nil {
		return nil, err
	}
	split := framing.Len()
	if err := writer.Close(); err != nil {
		return nil, err
	}
	contentLength := int64(framing.Len()) + info.Size()
	if contentLength > maxTorrentRequestSize {
		return nil, errors.New("multipart upload exceeds the 64 MiB request limit")
	}
	return &torrentFileUpload{
		path:          path,
		size:          info.Size(),
		prefix:        framing.Bytes()[:split],
		suffix:        framing.Bytes()[split:],
		contentType:   writer.FormDataContentType(),
		contentLength: contentLength,
	}, nil
}

func (u *torrentFileUpload) write(ctx context.Context, dst io.Writer) error {
	file, err := os.Open(u.path)
	if err != nil {
		return &torrentFileError{err: err}
	}
	defer file.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = file.Close() })
	defer stopClose()

	info, err := file.Stat()
	if err != nil {
		return &torrentFileError{err: err}
	}
	if !info.Mode().IsRegular() || info.Size() != u.size {
		return &torrentFileError{err: errors.New("torrent file changed before upload")}
	}
	if _, err := dst.Write(u.prefix); err != nil {
		return err
	}
	if _, err := io.CopyN(dst, file, u.size); err != nil {
		return &torrentFileError{err: err}
	}
	_, err = dst.Write(u.suffix)
	return err
}

func (c *Client) doMultipartStreamRequestContext(ctx context.Context, endpoint string, upload *torrentFileUpload) ([]byte, *http.Response, error) {
	ctx, timeoutCancel := contextWithHTTPTimeout(ctx, c.httpTimeouts.upload)
	defer timeoutCancel()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, pipeWriter := io.Pipe()
	defer reader.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpointURL(endpoint), reader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", upload.contentType)
	req.ContentLength = upload.contentLength
	setOriginHeaders(req)

	writeDone := make(chan error, 1)
	go func() {
		err := upload.write(ctx, pipeWriter)
		writeDone <- err
		_ = pipeWriter.CloseWithError(err)
	}()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		select {
		case writerErr := <-writeDone:
			var fileErr *torrentFileError
			if errors.As(writerErr, &fileErr) && !errors.Is(writerErr, io.ErrClosedPipe) {
				return nil, nil, fileErr
			}
		default:
		}
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// A server can reply before reading the upload. Close both sides on
		// return so a 403 can be retried with a fresh body.
		payload, readErr := io.ReadAll(io.LimitReader(resp.Body, maxHTTPErrorBodyBytes))
		if readErr != nil {
			return nil, resp, readErr
		}
		return payload, resp, nil
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, err
	}
	_ = reader.Close()
	select {
	case err := <-writeDone:
		return payload, resp, err
	case <-ctx.Done():
		return nil, resp, ctx.Err()
	}
}

// torrentAddRejected handles the legacy qBittorrent response. Older Web API
// versions report a rejected add as HTTP 200 with a "Fails." body, while
// newer versions may return an empty body for a successful add.
func torrentAddRejected(response []byte) bool {
	return strings.EqualFold(strings.TrimSpace(string(response)), "Fails.")
}
