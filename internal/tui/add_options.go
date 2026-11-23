//go:build !plan9

package tui

import (
	"fmt"
	"strings"

	"github.com/naterator/qbitctl/pkg/client"
)

const (
	addSourceField = iota
	addCategoryField
	addDestinationField
	addStoppedField
	addTagsField
	addContentLayoutField
	addDownloadPathField
	addSkipCheckingField
	addStopConditionField
	addSequentialField
	addFirstLastPieceField
	addRatioLimitField
	addSeedingTimeField
	addInactiveSeedingTimeField
	addShareLimitsModeField
	addShareLimitActionField
	addDestructiveConfirmField
)

func parseOptionalTUIChoice(value string) (set, enabled bool, err error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "default":
		return false, false, nil
	case "yes", "true", "on":
		return true, true, nil
	case "no", "false", "off":
		return true, false, nil
	default:
		return false, false, fmt.Errorf("choice must be default, yes, or no")
	}
}

func parseOptionalSeedtime(value, label string) (*int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	seconds, err := client.ParseSeedtimeLimit(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", label, err)
	}
	minutes, err := client.SeedtimeLimitSecondsToMinutes(seconds)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", label, err)
	}
	return &minutes, nil
}

func parseAddDialogOptions(d *dialog) (client.AddTorrentOptions, error) {
	stopped, err := parseTUIChoice(d.value(addStoppedField))
	if err != nil {
		return client.AddTorrentOptions{}, fmt.Errorf("stopped must be yes or no")
	}

	options := client.AddTorrentOptions{
		Category:     strings.TrimSpace(d.value(addCategoryField)),
		SavePath:     strings.TrimSpace(d.value(addDestinationField)),
		Stopped:      stopped,
		Tags:         strings.TrimSpace(d.value(addTagsField)),
		DownloadPath: strings.TrimSpace(d.value(addDownloadPathField)),
	}
	if options.ContentLayout, err = client.CanonicalContentLayout(d.value(addContentLayoutField)); err != nil {
		return client.AddTorrentOptions{}, err
	}
	if options.StopCondition, err = client.CanonicalStopCondition(d.value(addStopConditionField)); err != nil {
		return client.AddTorrentOptions{}, err
	}
	if options.SkipCheckingSet, options.SkipChecking, err = parseOptionalTUIChoice(d.value(addSkipCheckingField)); err != nil {
		return client.AddTorrentOptions{}, fmt.Errorf("skip checking: %v", err)
	}
	if options.SequentialDownloadSet, options.SequentialDownload, err = parseOptionalTUIChoice(d.value(addSequentialField)); err != nil {
		return client.AddTorrentOptions{}, fmt.Errorf("sequential download: %v", err)
	}
	if options.FirstLastPiecePrioritySet, options.FirstLastPiecePriority, err = parseOptionalTUIChoice(d.value(addFirstLastPieceField)); err != nil {
		return client.AddTorrentOptions{}, fmt.Errorf("first/last piece priority: %v", err)
	}

	if ratio := strings.TrimSpace(d.value(addRatioLimitField)); ratio != "" {
		value, parseErr := client.ParseAddRatioLimit(ratio)
		if parseErr != nil {
			return client.AddTorrentOptions{}, fmt.Errorf("share ratio: %v", parseErr)
		}
		options.ShareRatioLimit = &value
	}
	if options.ShareSeedingTimeLimit, err = parseOptionalSeedtime(d.value(addSeedingTimeField), "seeding time"); err != nil {
		return client.AddTorrentOptions{}, err
	}
	if options.ShareInactiveSeedingTimeLimit, err = parseOptionalSeedtime(d.value(addInactiveSeedingTimeField), "inactive seeding time"); err != nil {
		return client.AddTorrentOptions{}, err
	}

	if value := strings.TrimSpace(d.value(addShareLimitsModeField)); value != "" && !strings.EqualFold(value, "default") {
		options.ShareLimitsMode, err = client.CanonicalShareLimitsMode(value)
		if err != nil {
			return client.AddTorrentOptions{}, err
		}
	}
	if value := strings.TrimSpace(d.value(addShareLimitActionField)); value != "" && !strings.EqualFold(value, "default") {
		options.ShareLimitAction, err = client.CanonicalShareLimitAction(value)
		if err != nil {
			return client.AddTorrentOptions{}, err
		}
	}
	if client.IsDestructiveShareLimitAction(options.ShareLimitAction) {
		confirmed, confirmErr := parseTUIChoice(d.value(addDestructiveConfirmField))
		if confirmErr != nil || !confirmed {
			return client.AddTorrentOptions{}, fmt.Errorf("confirm the destructive share-limit action with yes")
		}
	}
	return client.NormalizeAddTorrentOptions(options)
}
