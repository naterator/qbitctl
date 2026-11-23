package client

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Category is one qBittorrent category and its server-side path settings.
// download_path is intentionally a RawMessage because qBittorrent versions
// have returned a string, false, or null for a category without an override.
type Category struct {
	Name         string          `json:"name"`
	SavePath     string          `json:"save_path"`
	DownloadPath json.RawMessage `json:"download_path"`
}

// GetCategories returns qBittorrent's category catalog in stable
// case-insensitive name order. The Web API returns an object keyed by name;
// the client exposes a sorted slice so CLI JSON and callers are deterministic.
func (a *App) GetCategories() ([]Category, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/torrents/categories", nil)
	if err != nil {
		return nil, codedErrWrapf(ExitFetchFail, err, "Failed to fetch categories: %v", err)
	}
	var raw map[string]struct {
		Name         string          `json:"name"`
		SavePath     string          `json:"savePath"`
		DownloadPath json.RawMessage `json:"download_path"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, codedErrf(ExitFetchFail, "Failed to parse categories: %v", err)
	}
	categories := make([]Category, 0, len(raw))
	for key, value := range raw {
		name := strings.TrimSpace(value.Name)
		if name == "" {
			name = key
		}
		categories = append(categories, Category{Name: name, SavePath: value.SavePath, DownloadPath: append(json.RawMessage(nil), value.DownloadPath...)})
	}
	sort.SliceStable(categories, func(i, j int) bool {
		left, right := strings.ToLower(categories[i].Name), strings.ToLower(categories[j].Name)
		if left == right {
			return categories[i].Name < categories[j].Name
		}
		return left < right
	})
	return categories, nil
}

func (a *App) GetCategory(name string) (Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Category{}, codedErrf(ExitBadArgs, "Category name is required")
	}
	categories, err := a.GetCategories()
	if err != nil {
		return Category{}, err
	}
	for _, category := range categories {
		if strings.EqualFold(category.Name, name) {
			return category, nil
		}
	}
	return Category{}, codedErrf(ExitBadArgs, "Category not found: %s", name)
}

func (a *App) CreateCategory(name, savePath string) error {
	return a.CreateCategoryWithPaths(name, savePath, nil, nil)
}

// CreateCategoryWithPaths creates a category and optionally configures its
// incomplete/download path. A nil option leaves the server default untouched.
func (a *App) CreateCategoryWithPaths(name, savePath string, downloadPath *string, downloadPathEnabled *bool) error {
	name, err := normalizeCategoryName(name)
	if err != nil {
		return err
	}
	params := url.Values{"category": {name}, "savePath": {strings.TrimSpace(savePath)}}
	setCategoryDownloadPath(params, downloadPath, downloadPathEnabled)
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/createCategory", params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to create category %q: %v", name, err)
	}
	return nil
}

// EditCategory updates a category's save path. qBittorrent's editCategory
// endpoint does not rename the category; callers should create a new category
// and migrate torrents when they need a different name.
func (a *App) EditCategory(name, savePath string) error {
	return a.EditCategoryWithPaths(name, savePath, nil, nil)
}

// EditCategoryWithPaths preserves the category's existing download-path
// override when callers omit both download-path options. This matters on
// qBittorrent versions whose editCategory endpoint replaces the whole path
// configuration instead of merging omitted fields.
func (a *App) EditCategoryWithPaths(name, savePath string, downloadPath *string, downloadPathEnabled *bool) error {
	name, err := normalizeCategoryName(name)
	if err != nil {
		return err
	}
	current, err := a.GetCategory(name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(savePath) == "" {
		savePath = current.SavePath
	}
	if downloadPath == nil && downloadPathEnabled == nil {
		downloadPath, downloadPathEnabled = categoryDownloadPathOptions(current.DownloadPath)
	}
	params := url.Values{"category": {name}, "savePath": {strings.TrimSpace(savePath)}}
	setCategoryDownloadPath(params, downloadPath, downloadPathEnabled)
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/editCategory", params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to edit category %q: %v", name, err)
	}
	return nil
}

func setCategoryDownloadPath(params url.Values, downloadPath *string, enabled *bool) {
	if downloadPath == nil && enabled == nil {
		return
	}
	if enabled == nil {
		value := true
		enabled = &value
	}
	params.Set("downloadPathEnabled", strconv.FormatBool(*enabled))
	if downloadPath != nil {
		params.Set("downloadPath", strings.TrimSpace(*downloadPath))
	} else {
		params.Set("downloadPath", "")
	}
}

func categoryDownloadPathOptions(raw json.RawMessage) (*string, *bool) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil, nil
	}
	var path string
	if json.Unmarshal(value, &path) == nil {
		enabled := true
		return &path, &enabled
	}
	var enabled bool
	if json.Unmarshal(value, &enabled) == nil {
		return nil, &enabled
	}
	return nil, nil
}

func (a *App) RemoveCategories(names ...string) error {
	normalized, err := normalizeCategoryNames(names)
	if err != nil {
		return err
	}
	params := url.Values{"categories": {strings.Join(normalized, "\n")}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/removeCategories", params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to remove categories: %v", err)
	}
	return nil
}

// GetTags returns qBittorrent's server-wide tag catalog in stable order.
func (a *App) GetTags() ([]string, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/torrents/tags", nil)
	if err != nil {
		return nil, codedErrWrapf(ExitFetchFail, err, "Failed to fetch tags: %v", err)
	}
	var tags []string
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, codedErrf(ExitFetchFail, "Failed to parse tags: %v", err)
	}
	clean := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		clean = append(clean, tag)
	}
	sort.SliceStable(clean, func(i, j int) bool {
		left, right := strings.ToLower(clean[i]), strings.ToLower(clean[j])
		if left == right {
			return clean[i] < clean[j]
		}
		return left < right
	})
	return clean, nil
}

func (a *App) CreateTags(tags ...string) error {
	normalized, err := normalizeTagNames(tags)
	if err != nil {
		return err
	}
	return a.mutateTagsCatalog("createTags", normalized, "create")
}

func (a *App) DeleteTags(tags ...string) error {
	normalized, err := normalizeTagNames(tags)
	if err != nil {
		return err
	}
	return a.mutateTagsCatalog("deleteTags", normalized, "delete")
}

func (a *App) mutateTagsCatalog(endpoint string, tags []string, verb string) error {
	params := url.Values{"tags": {strings.Join(tags, ",")}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/torrents/"+endpoint, params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to %s tags: %v", verb, err)
	}
	return nil
}

// AddTorrentTags and RemoveTorrentTags mutate tags on selected torrents. They
// intentionally do not call CreateTags/DeleteTags: server catalog deletion is
// a different qBittorrent operation from changing a torrent's assignment.
func (a *App) AddTorrentTags(hashes, tags string) error {
	return a.mutateTorrentTags(hashes, tags, "/api/v2/torrents/addTags", "add")
}

func (a *App) RemoveTorrentTags(hashes, tags string) error {
	return a.mutateTorrentTags(hashes, tags, "/api/v2/torrents/removeTags", "remove")
}

func (a *App) mutateTorrentTags(hashes, tags, endpoint, verb string) error {
	selector, err := normalizeTorrentSelector(hashes)
	if err != nil {
		return err
	}
	tagNames, err := normalizeTagString(tags)
	if err != nil {
		return err
	}
	params := url.Values{"hashes": {selector}, "tags": {strings.Join(tagNames, ",")}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, endpoint, params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to %s torrent tags: %v", verb, err)
	}
	return nil
}

func normalizeCategoryName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", codedErrf(ExitBadArgs, "Category name is required")
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", codedErrf(ExitBadArgs, "Category name cannot contain a newline")
	}
	return value, nil
}

func normalizeCategoryNames(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, codedErrf(ExitBadArgs, "At least one category is required")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		name, err := normalizeCategoryName(value)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

func normalizeTagString(value string) ([]string, error) {
	return normalizeTagNames(strings.Split(value, ","))
}

func normalizeTagNames(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, codedErrf(ExitBadArgs, "At least one tag is required")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, codedErrf(ExitBadArgs, "Tag names cannot be empty")
		}
		if strings.ContainsAny(value, ",\r\n") {
			return nil, codedErrf(ExitBadArgs, "Tag name %q contains a reserved separator", value)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func normalizeTorrentSelector(value string) (string, error) {
	parts := splitHashList(strings.TrimSpace(value))
	if len(parts) == 0 {
		return "", codedErrf(ExitBadArgs, "At least one torrent hash is required")
	}
	return strings.Join(parts, "|"), nil
}
