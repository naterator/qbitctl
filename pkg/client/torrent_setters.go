package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// splitHashList parses the pipe-separated hash value accepted by qBittorrent.
// It preserves the special "all" selector and removes duplicate hashes.
func splitHashList(value string) []string {
	parts := strings.Split(value, "|")
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil
		}
		if part == "all" {
			if len(parts) != 1 {
				return nil
			}
			return []string{"all"}
		}
		part = strings.ToLower(part)
		if part == "all" {
			return nil
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

// torrentsForHashes fetches each selected torrent and rejects missing hashes
// before a state-changing request is sent.
func (a *App) torrentsForHashes(hashes []string) ([]TorrentInfo, error) {
	if len(hashes) == 0 {
		return nil, codedErrf(ExitBadArgs, "Hash required for this operation")
	}
	if len(hashes) == 1 && hashes[0] == "all" {
		return a.fetchTorrents(nil)
	}
	for _, hash := range hashes {
		if !validateHash(hash) {
			return nil, codedErrf(ExitBadArgs, "Invalid hash or prefix: '%s'", hash)
		}
	}
	if len(hashes) > 50 {
		torrents, err := a.fetchTorrents(nil)
		if err != nil {
			return nil, err
		}
		return orderTorrentsForHashes(torrents, hashes)
	}
	torrents, err := a.fetchTorrents(hashes)
	if err != nil {
		return nil, err
	}
	return orderTorrentsForHashes(torrents, hashes)
}

func orderTorrentsForHashes(torrents []TorrentInfo, hashes []string) ([]TorrentInfo, error) {
	byHash := make(map[string]TorrentInfo, len(torrents))
	for _, torrent := range torrents {
		byHash[strings.ToLower(torrent.Hash)] = torrent
	}
	ordered := make([]TorrentInfo, 0, len(hashes))
	for _, hash := range hashes {
		torrent, ok := byHash[hash]
		if !ok {
			return nil, codedErrf(ExitBadArgs, "No torrent matches hash: %s", hash)
		}
		ordered = append(ordered, torrent)
	}
	return ordered, nil
}

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
		return codedErrWrapf(ExitSetFail, err, "Failed to set %s: %v", label, err)
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
		"inactiveSeedingTimeLimit": {strconv.FormatInt(inactiveSeedingTimeLimitValue(torrent), 10)},
		"shareLimitsMode":          {shareLimitsModeValue(torrent.ShareLimitsMode)},
		"shareLimitAction":         {shareLimitActionValue(torrent.ShareLimitAction)},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/setShareLimits", params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to set share limits: %v", err)
	}

	return nil
}

func (a *App) SetSeedtimeLimit(hash, seedtimeStr string) error {
	seconds, err := parseSeedtimeLimit(seedtimeStr)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v '%s'", err, seedtimeStr)
	}
	minutes, err := seedtimeLimitSecondsToMinutes(seconds)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v '%s'", err, seedtimeStr)
	}
	torrents, err := a.torrentsForHashes(splitHashList(hash))
	if err != nil {
		return err
	}

	return a.setShareLimitGroups(torrents, func(torrent *TorrentInfo) {
		torrent.SeedingTimeLimit = minutes
	})
}

// SetInactiveSeedtimeLimit updates the limit applied after a torrent has been
// inactive. It uses the same duration syntax and wire sentinels as the regular
// seed-time limit.
func (a *App) SetInactiveSeedtimeLimit(hash, seedtimeStr string) error {
	seconds, err := parseSeedtimeLimit(seedtimeStr)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v '%s'", err, seedtimeStr)
	}
	minutes, err := seedtimeLimitSecondsToMinutes(seconds)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v '%s'", err, seedtimeStr)
	}
	torrents, err := a.torrentsForHashes(splitHashList(hash))
	if err != nil {
		return err
	}

	return a.setShareLimitGroups(torrents, func(torrent *TorrentInfo) {
		torrent.InactiveSeedingTL = minutes
		torrent.inactiveSeedingTLPresent = true
	})
}

// SetShareLimitsMode updates how ratio, active seed-time, and inactive
// seed-time limits are combined. MatchAll/MatchAny are qBittorrent's wire
// values; friendly aliases are accepted by validation before this method runs.
func (a *App) SetShareLimitsMode(hash, value string) error {
	mode, err := parseShareLimitsMode(value)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v", err)
	}
	if err := a.requireShareLimitPolicyVersion("share-limits-mode", "5.3.0"); err != nil {
		return err
	}
	torrents, err := a.torrentsForHashes(splitHashList(hash))
	if err != nil {
		return err
	}

	return a.setShareLimitGroups(torrents, func(torrent *TorrentInfo) {
		torrent.ShareLimitsMode = mode
	})
}

// SetShareLimitAction updates the action qBittorrent takes once the selected
// share limit is reached. The destructive remove actions are intentionally
// available to scripted callers; the TUI adds an explicit confirmation.
func (a *App) SetShareLimitAction(hash, value string) error {
	action, err := parseShareLimitAction(value)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v", err)
	}
	if err := a.requireShareLimitPolicyVersion("share-limit-action", "5.2.0"); err != nil {
		return err
	}
	torrents, err := a.torrentsForHashes(splitHashList(hash))
	if err != nil {
		return err
	}

	return a.setShareLimitGroups(torrents, func(torrent *TorrentInfo) {
		torrent.ShareLimitAction = action
	})
}

func (a *App) SetRatioLimit(hash, ratioStr string) error {
	ratio, err := parseRatioLimit(ratioStr)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v", err)
	}
	torrents, err := a.torrentsForHashes(splitHashList(hash))
	if err != nil {
		return err
	}

	return a.setShareLimitGroups(torrents, func(torrent *TorrentInfo) {
		torrent.RatioLimit = ratio
	})
}

func (a *App) setShareLimitGroups(torrents []TorrentInfo, update func(*TorrentInfo)) error {
	type group struct {
		hashes  []string
		torrent TorrentInfo
	}
	groups := make([]group, 0, len(torrents))
	indices := make(map[string]int, len(torrents))
	for _, torrent := range torrents {
		update(&torrent)
		key := fmt.Sprintf("%.6f|%d|%d|%s|%s", torrent.RatioLimit, torrent.SeedingTimeLimit, inactiveSeedingTimeLimitValue(torrent), shareLimitsModeValue(torrent.ShareLimitsMode), shareLimitActionValue(torrent.ShareLimitAction))
		index, exists := indices[key]
		if !exists {
			indices[key] = len(groups)
			groups = append(groups, group{torrent: torrent})
			index = len(groups) - 1
		}
		groups[index].hashes = append(groups[index].hashes, strings.ToLower(torrent.Hash))
	}
	completed := make([]string, 0, len(torrents))
	for groupIndex, group := range groups {
		selector := strings.Join(group.hashes, "|")
		if err := a.setShareLimits(selector, group.torrent); err != nil {
			skipped := make([]string, 0)
			for _, remaining := range groups[groupIndex+1:] {
				skipped = append(skipped, remaining.hashes...)
			}
			return &groupedMutationError{
				operation: "set share limits",
				completed: completed,
				failed:    append([]string(nil), group.hashes...),
				skipped:   skipped,
				cause:     err,
			}
		}
		completed = append(completed, group.hashes...)
	}
	return nil
}

func (a *App) SetCategory(hash, category string) error {
	if category == "" {
		params := url.Values{
			"hashes":   {hash},
			"category": {""},
		}
		if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/setCategory", params); err != nil {
			return codedErrWrapf(ExitSetFail, err, "Failed to clear category: %v", err)
		}
		return nil
	}

	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/torrents/categories", nil)
	if err != nil {
		return codedErrWrapf(ExitFetchFail, err, "Failed to fetch categories: %v", err)
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
			return codedErrWrapf(ExitSetFail, err, "Failed to create category '%s': %v", category, err)
		}
	}

	params := url.Values{
		"hashes":   {hash},
		"category": {category},
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/setCategory", params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to set category '%s': %v", category, err)
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
		return codedErrWrapf(ExitSetFail, err, "Failed to set tags to '%s': %v", tags, err)
	}

	// qBittorrent 5.0.0 does not expose setTags. Reconstruct the replacement
	// operation with the add/remove endpoints available in that release. Newer
	// servers take the native path above and avoid the extra info request.
	if err := a.replaceTagsLegacy(hash, tags); err != nil {
		if _, grouped := err.(*groupedMutationError); grouped {
			return codedErrWrapf(ExitSetFail, err, "%v", err)
		}
		return codedErrWrapf(ExitSetFail, err, "Failed to set tags to '%s': %v", tags, err)
	}

	return nil
}

func isUnsupportedSetTagsError(err error) bool {
	return isHTTPStatus(err, http.StatusNotFound) || isHTTPStatus(err, http.StatusMethodNotAllowed)
}

func (a *App) replaceTagsLegacy(hash, tags string) error {
	torrents, err := a.torrentsForHashes(splitHashList(hash))
	if err != nil {
		return fmt.Errorf("fetch current tags: %w", err)
	}
	type group struct {
		hashes []string
		add    []string
		remove []string
	}
	groups := make([]group, 0, len(torrents))
	indices := make(map[string]int, len(torrents))
	for _, torrent := range torrents {
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
		sort.Strings(toAdd)
		sort.Strings(toRemove)
		key := strings.Join(toAdd, ",") + "\x00" + strings.Join(toRemove, ",")
		index, exists := indices[key]
		if !exists {
			indices[key] = len(groups)
			groups = append(groups, group{add: toAdd, remove: toRemove})
			index = len(groups) - 1
		}
		groups[index].hashes = append(groups[index].hashes, strings.ToLower(torrent.Hash))
	}
	completed := make([]string, 0, len(torrents))
	for groupIndex, group := range groups {
		selector := strings.Join(group.hashes, "|")
		if len(group.add) > 0 {
			if err := a.updateTagsLegacy(selector, group.add, "/api/v2/torrents/addTags"); err != nil {
				skipped := make([]string, 0)
				for _, remaining := range groups[groupIndex+1:] {
					skipped = append(skipped, remaining.hashes...)
				}
				return &groupedMutationError{
					operation: "set tags (add)",
					completed: completed,
					failed:    append([]string(nil), group.hashes...),
					skipped:   skipped,
					cause:     err,
				}
			}
		}
		if len(group.remove) > 0 {
			if err := a.updateTagsLegacy(selector, group.remove, "/api/v2/torrents/removeTags"); err != nil {
				skipped := make([]string, 0)
				for _, remaining := range groups[groupIndex+1:] {
					skipped = append(skipped, remaining.hashes...)
				}
				operation := "set tags (remove)"
				if len(group.add) > 0 {
					operation += ", add completed"
				}
				return &groupedMutationError{
					operation: operation,
					completed: completed,
					failed:    append([]string(nil), group.hashes...),
					skipped:   skipped,
					cause:     err,
				}
			}
		}
		completed = append(completed, group.hashes...)
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
		return codedErrWrapf(ExitSetFail, err, "Failed to set %s: %v", label, err)
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
		return codedErrWrapf(ExitSetFail, err, "Failed to set autotmm: %v", err)
	}

	return nil
}

func (a *App) SetSequentialDownload(hash, value string) error {
	desired, err := parseToggleValue(value)
	if err != nil {
		return codedErrf(ExitBadArgs, "%v", err)
	}

	torrents, err := a.torrentsForHashes(splitHashList(hash))
	if err != nil {
		return err
	}

	toToggle := make([]string, 0, len(torrents))
	for _, torrent := range torrents {
		if torrent.SequentialDL != desired {
			toToggle = append(toToggle, strings.ToLower(torrent.Hash))
		}
	}
	if len(toToggle) == 0 {
		return nil
	}

	params := url.Values{"hashes": {strings.Join(toToggle, "|")}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/toggleSequentialDownload", params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to toggle sequential download: %v", err)
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
	case "inactive-seedtime-limit":
		return a.SetInactiveSeedtimeLimit(hash, value)
	case "share-limits-mode":
		return a.SetShareLimitsMode(hash, value)
	case "share-limit-action":
		return a.SetShareLimitAction(hash, value)
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
