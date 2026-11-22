package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// FetchMainData returns the raw incremental sync response for the TUI queue.
// Keeping the payload untyped lets the TUI preserve fields that are added by
// newer qBittorrent versions while still decoding into TorrentInfo locally.
func (a *App) FetchMainData(ctx context.Context, rid int64) ([]byte, error) {
	if ctx == nil {
		ctx = a.context()
	}
	params := url.Values{"rid": {strconv.FormatInt(rid, 10)}}
	return a.client.requestContext(ctx, http.MethodGet, "/api/v2/sync/maindata", params)
}
