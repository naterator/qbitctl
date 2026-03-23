package client

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

func (a *App) qbtAction(hash, endpoint string, params url.Values) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("hashes", hash)
	if _, err := a.client.requestContext(a.context(), http.MethodPost, endpoint, params); err != nil {
		return codedErrf(ExitActionFail, "Request failed for %s: %v", endpoint, err)
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
	if err := a.PauseTorrent(hash); err != nil {
		return err
	}
	return a.qbtAction(hash, "/api/v2/torrents/delete", url.Values{"deleteFiles": {strconv.FormatBool(deleteFiles)}})
}

func (a *App) AddTorrent(source string) error {
	if source == "" {
		return codedErrf(ExitBadArgs, "Torrent file path or magnet link required")
	}
	if looksLikeTorrentSourceURL(source) {
		return a.addTorrentURL(source)
	}
	return a.addTorrentFile(source)
}

func (a *App) addTorrentURL(source string) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("urls", source); err != nil {
		return codedErrf(ExitActionFail, "Failed to build add request")
	}
	if err := writer.Close(); err != nil {
		return codedErrf(ExitActionFail, "Failed to finalize add request")
	}

	if _, err := a.client.requestBodyContext(a.context(), http.MethodPost, "/api/v2/torrents/add", body.Bytes(), writer.FormDataContentType()); err != nil {
		return codedErrf(ExitActionFail, "Failed to add torrent from URL or magnet link: %v", err)
	}
	return nil
}

func (a *App) addTorrentFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return codedErrf(ExitFile, "Failed to open torrent file '%s': %v", path, err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("torrents", filepath.Base(path))
	if err != nil {
		return codedErrf(ExitActionFail, "Failed to build add request")
	}
	if _, err := io.Copy(part, file); err != nil {
		return codedErrf(ExitFile, "Failed to read torrent file '%s': %v", path, err)
	}
	if err := writer.Close(); err != nil {
		return codedErrf(ExitActionFail, "Failed to finalize add request")
	}

	if _, err := a.client.requestBodyContext(a.context(), http.MethodPost, "/api/v2/torrents/add", body.Bytes(), writer.FormDataContentType()); err != nil {
		return codedErrf(ExitActionFail, "Failed to add torrent file '%s': %v", path, err)
	}
	return nil
}
