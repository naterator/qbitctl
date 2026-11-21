package client

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (a *App) setTransferLimit(hash, limitStr, endpoint, label string) error {
	value, err := parseLimit(limitStr)
	if err != nil {
		return codedErrf(ExitBadArgs, "Invalid %s: %s", label, limitStr)
	}

	params := url.Values{
		"hashes": {hash},
		"limit":  {strconv.FormatInt(value, 10)},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, endpoint, params); err != nil {
		return codedErrf(ExitSetFail, "Failed to set %s: %v", label, err)
	}

	return nil
}

func (a *App) SetUploadLimit(hash, limitStr string) error {
	return a.setTransferLimit(hash, limitStr, "/api/v2/torrents/setUploadLimit", "upload limit")
}

func (a *App) SetDownloadLimit(hash, limitStr string) error {
	return a.setTransferLimit(hash, limitStr, "/api/v2/torrents/setDownloadLimit", "download limit")
}

func (a *App) setShareLimits(hash string, torrent TorrentInfo) error {
	// qBittorrent 5.3 requires mode and action. Older releases ignore these
	// fields. Preserve the current policy when editing only one numeric limit.
	params := url.Values{
		"hashes":                   {hash},
		"ratioLimit":               {fmt.Sprintf("%.6f", torrent.RatioLimit)},
		"seedingTimeLimit":         {strconv.FormatInt(torrent.SeedingTimeLimit, 10)},
		"inactiveSeedingTimeLimit": {strconv.FormatInt(torrent.InactiveSeedingTL, 10)},
		"shareLimitsMode":          {cmp.Or(torrent.ShareLimitsMode, "Default")},
		"shareLimitAction":         {cmp.Or(torrent.ShareLimitAction, "Default")},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/setShareLimits", params); err != nil {
		return codedErrf(ExitSetFail, "Failed to set share limits: %v", err)
	}

	return nil
}

func (a *App) SetSeedtimeLimit(hash, seedtimeStr string) error {
	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return err
	}

	seconds, err := parseSeedtimeLimit(seedtimeStr)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v '%s'", err, seedtimeStr)
	}
	minutes, err := seedtimeLimitSecondsToMinutes(seconds)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v '%s'", err, seedtimeStr)
	}

	torrent.SeedingTimeLimit = minutes
	return a.setShareLimits(hash, torrent)
}

func (a *App) SetRatioLimit(hash, ratioStr string) error {
	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return err
	}

	ratio, err := strconv.ParseFloat(ratioStr, 64)
	if err != nil || ratio < 0 {
		return codedErrf(ExitBadArgs, "Invalid ratio value: %s", ratioStr)
	}

	torrent.RatioLimit = ratio
	return a.setShareLimits(hash, torrent)
}

func (a *App) SetCategory(hash, category string) error {
	if category == "" {
		params := url.Values{
			"hashes":   {hash},
			"category": {""},
		}
		if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/setCategory", params); err != nil {
			return codedErrf(ExitSetFail, "Failed to clear category: %v", err)
		}
		return nil
	}

	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/torrents/categories", nil)
	if err != nil {
		return codedErrf(ExitFetchFail, "Failed to fetch categories: %v", err)
	}

	var categories map[string]json.RawMessage
	if err := json.Unmarshal(body, &categories); err != nil {
		return codedErrf(ExitFetchFail, "Failed to parse categories")
	}

	exists := false
	for name := range categories {
		if strings.EqualFold(name, category) {
			exists = true
			break
		}
	}

	if !exists {
		createParams := url.Values{"category": {category}}
		if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/createCategory", createParams); err != nil {
			return codedErrf(ExitSetFail, "Failed to create category '%s': %v", category, err)
		}
	}

	params := url.Values{
		"hashes":   {hash},
		"category": {category},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/setCategory", params); err != nil {
		return codedErrf(ExitSetFail, "Failed to set category '%s': %v", category, err)
	}

	return nil
}

func (a *App) SetTags(hash, tags string) error {
	params := url.Values{
		"hashes": {hash},
		"tags":   {tags},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/setTags", params); err == nil {
		return nil
	} else if !isUnsupportedSetTagsError(err) {
		return codedErrf(ExitSetFail, "Failed to set tags to '%s': %v", tags, err)
	}

	// qBittorrent 5.0.0 does not expose setTags. Reconstruct the replacement
	// operation with the add/remove endpoints available in that release. Newer
	// servers take the native path above and avoid the extra info request.
	if err := a.replaceTagsLegacy(hash, tags); err != nil {
		return codedErrf(ExitSetFail, "Failed to set tags to '%s': %v", tags, err)
	}

	return nil
}

func isUnsupportedSetTagsError(err error) bool {
	return errors.Is(err, httpStatusError(http.StatusNotFound)) ||
		errors.Is(err, httpStatusError(http.StatusMethodNotAllowed))
}

func (a *App) replaceTagsLegacy(hash, tags string) error {
	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return fmt.Errorf("fetch current tags: %w", err)
	}

	current := splitTags(torrent.Tags)
	desired := splitTags(tags)
	currentSet := make(map[string]struct{}, len(current))
	desiredSet := make(map[string]struct{}, len(desired))
	for _, tag := range current {
		currentSet[tag] = struct{}{}
	}
	for _, tag := range desired {
		desiredSet[tag] = struct{}{}
	}

	toRemove := make([]string, 0, len(current))
	for _, tag := range current {
		if _, keep := desiredSet[tag]; !keep {
			toRemove = append(toRemove, tag)
		}
	}
	toAdd := make([]string, 0, len(desired))
	for _, tag := range desired {
		if _, alreadyPresent := currentSet[tag]; !alreadyPresent {
			toAdd = append(toAdd, tag)
		}
	}

	if len(toAdd) > 0 {
		if err := a.updateTagsLegacy(hash, toAdd, "/api/v2/torrents/addTags"); err != nil {
			return fmt.Errorf("add tags: %w", err)
		}
	}
	if len(toRemove) > 0 {
		if err := a.updateTagsLegacy(hash, toRemove, "/api/v2/torrents/removeTags"); err != nil {
			return fmt.Errorf("remove tags: %w", err)
		}
	}

	return nil
}

func (a *App) updateTagsLegacy(hash string, tags []string, endpoint string) error {
	params := url.Values{
		"hashes": {hash},
		"tags":   {strings.Join(tags, ",")},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, endpoint, params); err != nil {
		return err
	}
	return nil
}

func splitTags(tags string) []string {
	parts := strings.Split(tags, ",")
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, tag := range parts {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}

func (a *App) setToggle(hash, value, endpoint, label string, params url.Values) error {
	desired, err := parseToggleValue(value)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v", err)
	}

	if params == nil {
		params = url.Values{}
	}
	params.Set("hashes", hash)
	params.Set("value", strconv.FormatBool(desired))

	if _, err := a.client.requestContext(a.context(), http.MethodPost, endpoint, params); err != nil {
		return codedErrf(ExitSetFail, "Failed to set %s: %v", label, err)
	}

	return nil
}

func (a *App) SetSuperseed(hash, value string) error {
	return a.setToggle(hash, value, "/api/v2/torrents/setSuperSeeding", "superseed", nil)
}

func (a *App) SetAutoTMM(hash, value string) error {
	desired, err := parseToggleValue(value)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v", err)
	}

	params := url.Values{
		"hashes": {hash},
		"enable": {strconv.FormatBool(desired)},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/setAutoManagement", params); err != nil {
		return codedErrf(ExitSetFail, "Failed to set autotmm: %v", err)
	}

	return nil
}

func (a *App) SetSequentialDownload(hash, value string) error {
	desired, err := parseToggleValue(value)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v", err)
	}

	torrent, err := a.GetTorrentInfo(hash)
	if err != nil {
		return err
	}
	if torrent.SequentialDL == desired {
		return nil
	}

	params := url.Values{"hashes": {hash}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/toggleSequentialDownload", params); err != nil {
		return codedErrf(ExitSetFail, "Failed to toggle sequential download: %v", err)
	}

	return nil
}

func (a *App) SetField(hash, field, value string) error {
	switch field {
	case "category":
		return a.SetCategory(hash, value)
	case "tags":
		return a.SetTags(hash, value)
	case "up-limit":
		return a.SetUploadLimit(hash, value)
	case "dl-limit":
		return a.SetDownloadLimit(hash, value)
	case "ratio-limit":
		return a.SetRatioLimit(hash, value)
	case "seedtime-limit":
		return a.SetSeedtimeLimit(hash, value)
	case "seqdl":
		return a.SetSequentialDownload(hash, value)
	case "autotmm":
		return a.SetAutoTMM(hash, value)
	case "superseed":
		return a.SetSuperseed(hash, value)
	default:
		return codedErrf(ExitBadArgs, "Unknown field: %s", field)
	}
}
