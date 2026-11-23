package client

import "strings"

// CanonicalGetField normalizes a get field name alias to its canonical form.
func CanonicalGetField(field string) string {
	switch strings.ToLower(field) {
	case "hash":
		return "hash"
	case "name":
		return "name"
	case "tags":
		return "tags"
	case "category":
		return "category"
	case "up-limit", "upload-limit", "uplimit":
		return "up-limit"
	case "dl-limit", "download-limit", "dllimit":
		return "dl-limit"
	case "dl-path", "download-path", "path":
		return "dl-path"
	case "ratio-limit":
		return "ratio-limit"
	case "seedtime", "seed-time":
		return "seedtime"
	case "seedtime-limit", "seed-time-limit":
		return "seedtime-limit"
	case "inactive-seedtime-limit", "inactive-seed-time-limit", "inactive-seeding-time-limit", "inactive_seeding_time_limit", "inactive_seeding_tl":
		return "inactive-seedtime-limit"
	case "share-limits-mode", "share-limit-mode", "share_limits_mode", "share_mode", "share-mode":
		return "share-limits-mode"
	case "share-limit-action", "share_limit_action", "share_action", "share-action":
		return "share-limit-action"
	case "seqdl", "sequential-download":
		return "seqdl"
	case "autotmm", "auto-tmm":
		return "autotmm"
	case "superseed", "super-seed":
		return "superseed"
	case "tracker":
		return "tracker"
	case "tracker-list", "trackers":
		return "tracker-list"
	case "private":
		return "private"
	case "ratio":
		return "ratio"
	case "up-speed", "upload-speed":
		return "up-speed"
	case "dl-speed", "download-speed":
		return "dl-speed"
	case "size":
		return "size"
	case "uploaded":
		return "uploaded"
	case "downloaded":
		return "downloaded"
	case "eta":
		return "eta"
	case "state", "status":
		return "state"
	case "progress":
		return "progress"
	default:
		return strings.ToLower(field)
	}
}

// CanonicalSetField normalizes a set field name alias to its canonical form.
func CanonicalSetField(field string) string {
	switch strings.ToLower(field) {
	case "category":
		return "category"
	case "tags":
		return "tags"
	case "up-limit", "upload-limit", "uplimit":
		return "up-limit"
	case "dl-limit", "download-limit", "dllimit":
		return "dl-limit"
	case "ratio-limit":
		return "ratio-limit"
	case "seedtime-limit", "seed-time-limit":
		return "seedtime-limit"
	case "inactive-seedtime-limit", "inactive-seed-time-limit", "inactive-seeding-time-limit", "inactive_seeding_time_limit", "inactive_seeding_tl":
		return "inactive-seedtime-limit"
	case "share-limits-mode", "share-limit-mode", "share_limits_mode", "share_mode", "share-mode":
		return "share-limits-mode"
	case "share-limit-action", "share_limit_action", "share_action", "share-action":
		return "share-limit-action"
	case "seqdl", "sequential-download":
		return "seqdl"
	case "autotmm", "auto-tmm":
		return "autotmm"
	case "superseed", "super-seed":
		return "superseed"
	default:
		return strings.ToLower(field)
	}
}
