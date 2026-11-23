package client

import (
	"fmt"
	"strings"
)

// Share-limit policy values are the strings qBittorrent accepts on the
// setShareLimits endpoint. qBittorrent 5.3 added the mode and action fields;
// callers may use the friendly aliases accepted by the parsers below when
// building command-line or TUI requests.
const (
	shareLimitsModeDefault             = "Default"
	shareLimitsModeAll                 = "MatchAll"
	shareLimitsModeAny                 = "MatchAny"
	shareLimitActionDefault            = "Default"
	shareLimitActionStop               = "Stop"
	shareLimitActionRemove             = "Remove"
	shareLimitActionRemoveWithContent  = "RemoveWithContent"
	shareLimitActionEnableSuperseeding = "EnableSuperSeeding"
)

var shareLimitsModes = []string{
	shareLimitsModeDefault,
	shareLimitsModeAll,
	shareLimitsModeAny,
}

var shareLimitActions = []string{
	shareLimitActionDefault,
	shareLimitActionStop,
	shareLimitActionRemove,
	shareLimitActionRemoveWithContent,
	shareLimitActionEnableSuperseeding,
}

// ShareLimitsModeChoices returns the qBittorrent wire values in TUI order.
func ShareLimitsModeChoices() []string {
	return append([]string(nil), shareLimitsModes...)
}

// ShareLimitActionChoices returns the qBittorrent wire values in TUI order.
func ShareLimitActionChoices() []string {
	return append([]string(nil), shareLimitActions...)
}

// CanonicalShareLimitsMode accepts qBittorrent wire values and friendly
// command-line aliases and returns the exact wire value.
func CanonicalShareLimitsMode(value string) (string, error) {
	return parseShareLimitsMode(value)
}

// CanonicalShareLimitAction accepts qBittorrent wire values and friendly
// command-line aliases and returns the exact wire value.
func CanonicalShareLimitAction(value string) (string, error) {
	return parseShareLimitAction(value)
}

func parseShareLimitsMode(value string) (string, error) {
	switch normalizeShareLimitChoice(value) {
	case "", "default":
		return shareLimitsModeDefault, nil
	case "matchall", "all", "and":
		return shareLimitsModeAll, nil
	case "matchany", "any", "or":
		return shareLimitsModeAny, nil
	default:
		return "", fmt.Errorf("share limits mode must be default, match-all, or match-any")
	}
}

func parseShareLimitAction(value string) (string, error) {
	switch normalizeShareLimitChoice(value) {
	case "", "default":
		return shareLimitActionDefault, nil
	case "stop":
		return shareLimitActionStop, nil
	case "remove", "removefromlist":
		return shareLimitActionRemove, nil
	case "removewithcontent", "removecontent", "delete":
		return shareLimitActionRemoveWithContent, nil
	case "enablesuperseeding", "enable-super-seeding", "superseeding":
		return shareLimitActionEnableSuperseeding, nil
	default:
		return "", fmt.Errorf("share limit action must be default, stop, remove, remove-with-content, or enable-super-seeding")
	}
}

func normalizeShareLimitChoice(value string) string {
	return strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.TrimSpace(value)))
}

func shareLimitsModeValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return shareLimitsModeDefault
	}
	return value
}

func shareLimitActionValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return shareLimitActionDefault
	}
	return value
}

func inactiveSeedingTimeLimitValue(torrent TorrentInfo) int64 {
	if torrent.inactiveSeedingTLPresent || torrent.InactiveSeedingTL != 0 {
		return torrent.InactiveSeedingTL
	}
	// qBittorrent's omitted/default inactive policy is the global limit.
	return -2
}

// EffectiveInactiveSeedingTimeLimit returns the value qbitctl will preserve
// when it sends another share-limit update. A response that omitted the
// optional field inherits qBittorrent's global limit (-2).
func EffectiveInactiveSeedingTimeLimit(torrent TorrentInfo) int64 {
	return inactiveSeedingTimeLimitValue(torrent)
}

func isDestructiveShareLimitAction(value string) bool {
	return value == shareLimitActionRemove || value == shareLimitActionRemoveWithContent
}

// IsDestructiveShareLimitAction reports whether applying the selected policy
// can remove the torrent from qBittorrent.
func IsDestructiveShareLimitAction(value string) bool {
	canonical, err := parseShareLimitAction(value)
	return err == nil && isDestructiveShareLimitAction(canonical)
}

// requireShareLimitPolicyVersion rejects fields that the connected server
// cannot honor. The base inactive-seeding limit is available on the qBittorrent
// 5.0 compatibility floor; the action and mode fields were added later.
func (a *App) requireShareLimitPolicyVersion(field, minimum string) error {
	version, err := a.fetchAppVersion()
	if err != nil {
		return err
	}
	if compareDottedVersion(version, minimum) < 0 {
		return codedErrf(ExitSetFail, "%s requires qBittorrent %s or newer (server is %s)", field, minimum, version)
	}
	return nil
}

// groupedMutationError keeps the public CodedError (via Unwrap) while making
// partial multi-request updates visible to scripts and people. A failed
// request may have affected only part of its pipe-separated hash group, so the
// group is reported as failed rather than claiming each member was untouched.
type groupedMutationError struct {
	operation string
	completed []string
	failed    []string
	skipped   []string
	cause     error
}

func (e *groupedMutationError) Error() string {
	message := fmt.Sprintf("failed to %s", e.operation)
	if len(e.completed) > 0 {
		message += "; confirmed: " + strings.Join(e.completed, ",")
	}
	if len(e.failed) > 0 {
		message += "; failed group: " + strings.Join(e.failed, ",")
	}
	if len(e.skipped) > 0 {
		message += "; skipped: " + strings.Join(e.skipped, ",")
	}
	if e.cause != nil {
		message += ": " + e.cause.Error()
	}
	return message
}

func (e *groupedMutationError) Unwrap() error { return e.cause }
