package client

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// normalizeContentLayout returns the exact qBittorrent wire value for a
// content layout choice. An empty or default choice leaves the server's
// default untouched.
func normalizeContentLayout(value string) (string, error) {
	switch normalizeAddChoice(value) {
	case "", "default":
		return "", nil
	case "original":
		return "Original", nil
	case "subfolder", "subfoldername":
		return "Subfolder", nil
	case "nosubfolder", "no-subfolder":
		return "NoSubfolder", nil
	default:
		return "", fmt.Errorf("content layout must be original, subfolder, or no-subfolder")
	}
}

// CanonicalContentLayout accepts the friendly CLI/TUI spelling and returns
// the exact qBittorrent add API value. Empty and default mean unset.
func CanonicalContentLayout(value string) (string, error) {
	return normalizeContentLayout(value)
}

func normalizeStopCondition(value string) (string, error) {
	switch normalizeAddChoice(value) {
	case "", "default":
		return "", nil
	case "none":
		return "None", nil
	case "metadata", "metadatareceived":
		return "MetadataReceived", nil
	case "files", "fileschecked":
		return "FilesChecked", nil
	default:
		return "", fmt.Errorf("stop condition must be none, metadata, or files")
	}
}

// CanonicalStopCondition accepts the friendly CLI/TUI spelling and returns
// the exact qBittorrent add API value. Empty and default mean unset.
func CanonicalStopCondition(value string) (string, error) {
	return normalizeStopCondition(value)
}

func normalizeAddChoice(value string) string {
	return strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.TrimSpace(value)))
}

// ParseAddRatioLimit parses a share-ratio limit for an add request. Unlike
// the historical set command parser, add accepts qBittorrent's -1 (unlimited)
// and -2 (global limit) sentinels.
func ParseAddRatioLimit(value string) (float64, error) {
	ratio, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < -2 || (ratio < 0 && ratio != -1 && ratio != -2) {
		return 0, fmt.Errorf("invalid ratio value: %s", value)
	}
	return ratio, nil
}

// NormalizeAddTorrentOptions validates and canonicalizes add options before
// they are sent to qBittorrent. It is exported so CLI and TUI validation can
// report malformed values before authentication or an upload begins.
func NormalizeAddTorrentOptions(options AddTorrentOptions) (AddTorrentOptions, error) {
	options.Category = strings.TrimSpace(options.Category)
	options.SavePath = strings.TrimSpace(options.SavePath)
	options.Tags = strings.TrimSpace(options.Tags)
	options.DownloadPath = strings.TrimSpace(options.DownloadPath)

	var err error
	if options.ContentLayout, err = normalizeContentLayout(options.ContentLayout); err != nil {
		return AddTorrentOptions{}, err
	}
	if options.StopCondition, err = normalizeStopCondition(options.StopCondition); err != nil {
		return AddTorrentOptions{}, err
	}

	if options.ShareRatioLimit != nil {
		ratio := *options.ShareRatioLimit
		if _, err := ParseAddRatioLimit(strconv.FormatFloat(ratio, 'g', -1, 64)); err != nil {
			return AddTorrentOptions{}, err
		}
		options.ShareRatioLimit = &ratio
	}
	for name, limit := range map[string]*int64{
		"seeding time limit":          options.ShareSeedingTimeLimit,
		"inactive seeding time limit": options.ShareInactiveSeedingTimeLimit,
	} {
		if limit == nil {
			continue
		}
		if *limit < -2 {
			return AddTorrentOptions{}, fmt.Errorf("%s must be non-negative minutes, -1 (unlimited), or -2 (global limit)", name)
		}
	}

	if strings.TrimSpace(options.ShareLimitsMode) != "" {
		options.ShareLimitsMode, err = CanonicalShareLimitsMode(options.ShareLimitsMode)
		if err != nil {
			return AddTorrentOptions{}, err
		}
	}
	if strings.TrimSpace(options.ShareLimitAction) != "" {
		options.ShareLimitAction, err = CanonicalShareLimitAction(options.ShareLimitAction)
		if err != nil {
			return AddTorrentOptions{}, err
		}
	}
	return options, nil
}

// ValidateAddTorrentOptions validates values without contacting qBittorrent.
func ValidateAddTorrentOptions(options AddTorrentOptions) error {
	_, err := NormalizeAddTorrentOptions(options)
	return err
}

func (options AddTorrentOptions) hasAdvancedOverrides() bool {
	return options.ContentLayout != "" ||
		options.DownloadPath != "" ||
		options.SkipCheckingSet ||
		options.StopCondition != "" ||
		options.SequentialDownloadSet ||
		options.FirstLastPiecePrioritySet ||
		options.ShareRatioLimit != nil ||
		options.ShareSeedingTimeLimit != nil ||
		options.ShareInactiveSeedingTimeLimit != nil ||
		options.ShareLimitsMode != "" ||
		options.ShareLimitAction != ""
}

func validateAddTorrentOptionsForVersion(options AddTorrentOptions, version string) error {
	if options.ShareLimitsMode != "" && compareDottedVersion(version, "5.3.0") < 0 {
		return codedErrf(ExitActionFail, "share limits mode requires qBittorrent 5.3.0 or newer (server is %s)", version)
	}
	return nil
}

func (a *App) validateAddTorrentOptions(options AddTorrentOptions) error {
	if !options.SkipCheckingSet && options.ShareLimitsMode == "" {
		return nil
	}
	version, err := a.fetchAppVersion()
	if err != nil {
		return err
	}
	if a.compatibilitySource == compatibilitySourceDisk {
		// A metadata-only compatibility cache can outlive an upgrade. These
		// options change wire names or availability at 5.3, so refresh before
		// choosing the request spelling.
		if err := a.refreshCompatibility(); err != nil {
			return err
		}
		version = a.appVersion
	}
	return validateAddTorrentOptionsForVersion(options, version)
}
