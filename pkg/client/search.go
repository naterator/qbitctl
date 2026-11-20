package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	searchPollInterval   = 500 * time.Millisecond
	searchCleanupTimeout = 2 * time.Second
)

var errSearchTimeLimit = errors.New("search time limit reached")

// SearchResult describes a torrent returned by a qBittorrent search plugin.
type SearchResult struct {
	ID       int64  `json:"id,omitempty"`
	Name     string `json:"name"`
	Size     int64  `json:"size"` // Bytes, or -1 when unknown.
	Seeders  int    `json:"seeders"`
	Leechers int    `json:"leechers"`
	Source   string `json:"source"` // Hostname of the result page or plugin site.

	downloadURL string
	pluginName  string
}

// Search runs one job with the selected plugin and returns the results available
// when the job finishes or the time limit is reached.
// It attempts to delete the job before returning, including on cancellation.
func (a *App) Search(pattern, plugin string, timeout time.Duration) ([]SearchResult, error) {
	pattern = strings.TrimSpace(pattern)
	plugin = strings.TrimSpace(plugin)
	if pattern == "" || plugin == "" || timeout <= 0 {
		return nil, codedErrf(ExitBadArgs, "Search requires a pattern, a plugin, and a positive timeout")
	}
	ctx, cancel := context.WithTimeoutCause(a.context(), timeout, errSearchTimeLimit)
	defer cancel()

	id, err := a.startSearch(ctx, pattern, plugin)
	if err != nil {
		return nil, searchError(ctx, "start search", err)
	}
	params := url.Values{"id": {strconv.Itoa(id)}}
	defer a.deleteSearch(params)

	operation := "check search status"
	err = a.waitForSearch(ctx, id, params)
	var results []SearchResult
	if err == nil {
		operation = "fetch search results"
		results, err = a.searchResults(ctx, params, plugin)
	}
	if err == nil {
		return results, nil
	}
	if errors.Is(context.Cause(ctx), errSearchTimeLimit) {
		return a.partialSearchResults(params, plugin, timeout)
	}
	return nil, searchError(ctx, operation, err)
}

func (a *App) partialSearchResults(params url.Values, plugin string, timeout time.Duration) ([]SearchResult, error) {
	// Use a new deadline so the results can be read after the search timer
	// expires. The command context still allows Ctrl-C to interrupt the read.
	ctx, cancel := context.WithTimeout(a.context(), searchCleanupTimeout)
	defer cancel()
	results, err := a.searchResults(ctx, params, plugin)
	if ctx.Err() == context.Canceled {
		return nil, searchError(ctx, "fetch search results", err)
	}
	if err != nil {
		return nil, codedErrf(ExitFetchFail, "Search reached the %s limit and could not fetch results: %v", timeout, err)
	}
	fmt.Fprintf(a.Stderr, "[WARN] Search reached the %s limit. Returning the results found so far.\n", timeout)
	return results, nil
}

func (a *App) startSearch(ctx context.Context, pattern, plugin string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Once the POST is in flight, give it a short grace period to return the
	// job ID after cancellation. The ID is needed to delete the job safely.
	startCtx, cancelStart := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelStart()
	stop := context.AfterFunc(ctx, func() {
		select {
		case <-time.After(searchCleanupTimeout):
			cancelStart()
		case <-startCtx.Done():
		}
	})
	defer stop()

	body, err := a.client.requestContext(startCtx, http.MethodPost, "/api/v2/search/start", url.Values{
		"pattern": {pattern}, "plugins": {plugin}, "category": {"all"},
	})
	if err != nil {
		return 0, err
	}
	var job struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body, &job); err != nil || job.ID <= 0 {
		return 0, errors.New("invalid search job ID from qBittorrent")
	}
	return job.ID, nil
}

func (a *App) waitForSearch(ctx context.Context, id int, params url.Values) error {
	poll := time.NewTicker(searchPollInterval)
	defer poll.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		body, err := a.client.requestContext(ctx, http.MethodGet, "/api/v2/search/status", params)
		if err != nil {
			return err
		}
		var jobs []struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(body, &jobs); err != nil || len(jobs) != 1 || jobs[0].ID != id {
			return errors.New("invalid search status response from qBittorrent")
		}
		switch jobs[0].Status {
		case "Stopped":
			return nil
		case "Running":
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-poll.C:
			}
		default:
			return fmt.Errorf("unexpected search status %q", jobs[0].Status)
		}
	}
}

func (a *App) searchResults(ctx context.Context, params url.Values, plugin string) ([]SearchResult, error) {
	body, err := a.client.requestContext(ctx, http.MethodGet, "/api/v2/search/results", params)
	if err != nil {
		return nil, err
	}
	var response struct {
		Results []struct {
			Name        string `json:"fileName"`
			Size        int64  `json:"fileSize"`
			Seeders     int    `json:"nbSeeders"`
			Leechers    int    `json:"nbLeechers"`
			PageURL     string `json:"descrLink"`
			SiteURL     string `json:"siteUrl"`
			DownloadURL string `json:"fileUrl"`
			Plugin      string `json:"engineName"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Results == nil {
		return nil, errors.New("invalid search results response from qBittorrent")
	}
	results := make([]SearchResult, 0, len(response.Results))
	for _, item := range response.Results {
		engine := item.Plugin
		if engine == "" && !strings.Contains(plugin, "|") {
			switch strings.ToLower(plugin) {
			case "all", "enabled", "multi":
			default:
				engine = plugin
			}
		}
		download := strings.TrimSpace(item.DownloadURL)
		// Jackett uses its own home URL in diagnostic rows.
		if strings.TrimRight(download, "/") == strings.TrimRight(item.SiteURL, "/") || !isSearchDownloadURL(download) {
			download = ""
		}
		results = append(results, SearchResult{
			Name: item.Name, Size: item.Size, Seeders: item.Seeders, Leechers: item.Leechers,
			Source:      searchSource(item.PageURL, item.SiteURL),
			downloadURL: download, pluginName: engine,
		})
	}
	return results, nil
}

func isSearchDownloadURL(raw string) bool {
	target, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch target.Scheme {
	case "magnet":
		return target.Query().Get("xt") != ""
	case "http", "https", "ftp":
		return target.Hostname() != ""
	default:
		return false
	}
}

func searchSource(pageURL, siteURL string) string {
	for _, raw := range []string{pageURL, siteURL} {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") {
			return parsed.Hostname()
		}
	}
	return ""
}

func (a *App) deleteSearch(params url.Values) {
	ctx, cancel := context.WithTimeout(context.Background(), searchCleanupTimeout)
	defer cancel()
	_, err := a.client.requestContext(ctx, http.MethodPost, "/api/v2/search/delete", params)
	if err != nil && !errors.Is(err, httpStatusError(http.StatusNotFound)) {
		fmt.Fprintf(a.Stderr, "[WARN] Failed to delete search job %s: %v\n", params.Get("id"), err)
	}
}

func searchError(ctx context.Context, operation string, err error) error {
	switch ctx.Err() {
	case context.Canceled:
		return codedErrf(ExitFetchFail, "Search canceled")
	case context.DeadlineExceeded:
		return codedErrf(ExitFetchFail, "Search timed out")
	default:
		return codedErrf(ExitFetchFail, "Failed to %s: %v", operation, err)
	}
}
