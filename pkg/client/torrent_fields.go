package client

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// GetFieldNames lists all field names available for the get command.
var GetFieldNames = []string{
	"hash",
	"name",
	"tags",
	"category",
	"up-limit",
	"dl-limit",
	"dl-path",
	"ratio-limit",
	"seedtime",
	"seedtime-limit",
	"inactive-seedtime-limit",
	"share-limits-mode",
	"share-limit-action",
	"seqdl",
	"autotmm",
	"superseed",
	"tracker",
	"private",
	"ratio",
	"up-speed",
	"dl-speed",
	"size",
	"uploaded",
	"downloaded",
	"eta",
	"state",
	"added-on",
	"completion-on",
	"last-activity",
	"seen-complete",
	"time-active",
	"save-path",
	"temp-download-path",
	"remaining-bytes",
	"completed-bytes",
	"availability",
	"seeds",
	"leechers",
	"swarm-seeds",
	"swarm-leechers",
	"queue-position",
	"tracker-count",
	"progress",
}

// SetFieldNames lists all field names available for the set command.
var SetFieldNames = []string{
	"autotmm",
	"category",
	"dl-limit",
	"ratio-limit",
	"seedtime-limit",
	"inactive-seedtime-limit",
	"share-limits-mode",
	"share-limit-action",
	"seqdl",
	"superseed",
	"tags",
	"up-limit",
}

func canonicalTorrentField(field string) string {
	switch strings.ToLower(field) {
	case "hash":
		return "hash"
	default:
		return CanonicalGetField(field)
	}
}

func isSelectableTorrentField(field string) bool {
	for _, name := range GetFieldNames {
		if field == name {
			return true
		}
	}
	return false
}

func ParseSelectableTorrentFields(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("field list cannot be empty")
	}

	parts := strings.Split(value, ",")
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		field := canonicalTorrentField(strings.TrimSpace(part))
		if !isSelectableTorrentField(field) {
			return nil, fmt.Errorf("unsupported field for list/show output: %s", strings.TrimSpace(part))
		}
		fields = append(fields, field)
	}

	return fields, nil
}

func torrentFieldValue(t TorrentInfo, field string) string {
	switch field {
	case "hash":
		if len(t.Hash) > 8 {
			return t.Hash[:8]
		}
		return t.Hash
	case "hash-full":
		return t.Hash
	case "name":
		return t.Name
	case "tags":
		return t.Tags
	case "category":
		return t.Category
	case "up-limit":
		return strconv.FormatInt(t.UpLimit, 10)
	case "dl-limit":
		return strconv.FormatInt(t.DLLimit, 10)
	case "dl-path":
		return t.ContentPath
	case "ratio-limit":
		return formatRatio(t.RatioLimit)
	case "seedtime":
		return strconv.FormatInt(t.SeedingTime, 10)
	case "seedtime-limit":
		return strconv.FormatInt(t.SeedingTimeLimit, 10)
	case "inactive-seedtime-limit":
		return strconv.FormatInt(EffectiveInactiveSeedingTimeLimit(t), 10)
	case "share-limits-mode":
		return shareLimitsModeValue(t.ShareLimitsMode)
	case "share-limit-action":
		return shareLimitActionValue(t.ShareLimitAction)
	case "seqdl":
		return formatBool(t.SequentialDL)
	case "autotmm":
		return formatBool(t.AutoTMM)
	case "superseed":
		return formatBool(t.SuperSeeding)
	case "tracker":
		return t.Tracker
	case "private":
		return formatBool(t.Private)
	case "ratio":
		return formatRatio(t.Ratio)
	case "up-speed":
		return strconv.FormatInt(t.UpSpeed, 10)
	case "dl-speed":
		return strconv.FormatInt(t.DLSpeed, 10)
	case "size":
		return strconv.FormatInt(t.Size, 10)
	case "uploaded":
		return strconv.FormatInt(t.Uploaded, 10)
	case "downloaded":
		return strconv.FormatInt(t.Downloaded, 10)
	case "eta":
		return strconv.FormatInt(t.ETA, 10)
	case "state":
		return t.State
	case "progress":
		return formatProgress(t.Progress)
	case "progress-pct":
		pct := int(t.Progress * 100)
		if pct > 100 {
			pct = 100
		}
		return fmt.Sprintf("%d%%", pct)
	case "added-on":
		return optionalIntString(t.AddedOn)
	case "completion-on":
		return optionalIntString(t.CompletionOn)
	case "last-activity":
		return optionalIntString(t.LastActivity)
	case "seen-complete":
		return optionalIntString(t.SeenComplete)
	case "time-active":
		return optionalIntString(t.TimeActive)
	case "save-path":
		if t.SavePath != nil {
			return *t.SavePath
		}
	case "temp-download-path":
		if t.DownloadPath != nil {
			return *t.DownloadPath
		}
	case "remaining-bytes":
		return optionalIntString(t.AmountLeft)
	case "completed-bytes":
		return optionalIntString(t.Completed)
	case "availability":
		if t.Availability != nil {
			return strconv.FormatFloat(*t.Availability, 'f', -1, 64)
		}
	case "seeds":
		return optionalIntString(t.ConnectedSeeds)
	case "leechers":
		return optionalIntString(t.ConnectedLeechers)
	case "swarm-seeds":
		return optionalIntString(t.TotalSeeders)
	case "swarm-leechers":
		return optionalIntString(t.TotalLeechers)
	case "queue-position":
		return optionalIntString(t.QueuePosition)
	case "tracker-count":
		return optionalIntString(t.TrackerCount)
	default:
		return ""
	}
	return ""
}

func optionalIntString(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

// torrentDisplayFieldValue formats the fields that are intended for people to
// read. The raw torrentFieldValue function is deliberately kept separate: it
// is also used by --json and Go templates, whose existing values are part of
// the CLI contract.
func torrentDisplayFieldValue(units DataUnits, t TorrentInfo, field string) string {
	switch field {
	case "up-limit":
		return formatTorrentRateLimit(units, t.UpLimit)
	case "dl-limit":
		return formatTorrentRateLimit(units, t.DLLimit)
	case "up-speed":
		return units.FormatRate(t.UpSpeed)
	case "dl-speed":
		return units.FormatRate(t.DLSpeed)
	case "size":
		return units.FormatSize(t.Size)
	case "uploaded":
		return units.FormatSize(t.Uploaded)
	case "downloaded":
		return units.FormatSize(t.Downloaded)
	case "remaining-bytes":
		if t.AmountLeft != nil {
			return units.FormatSize(*t.AmountLeft)
		}
	case "completed-bytes":
		if t.Completed != nil {
			return units.FormatSize(*t.Completed)
		}
	case "added-on", "completion-on", "last-activity", "seen-complete":
		raw := torrentFieldValue(t, field)
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
			return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
		}
		return raw
	default:
		return torrentFieldValue(t, field)
	}
	return ""
}

func formatTorrentRateLimit(units DataUnits, value int64) string {
	switch value {
	case -1:
		return "unlimited"
	case -2:
		return "global"
	default:
		return units.FormatRate(value)
	}
}

func writeTorrentFieldRows(w io.Writer, torrents []TorrentInfo, fields []string, header bool, units DataUnits) error {
	writer := csv.NewWriter(w)
	writer.Comma = '\t'

	if header {
		if err := writer.Write(fields); err != nil {
			return err
		}
	}

	for _, torrent := range torrents {
		row := make([]string, 0, len(fields))
		for _, field := range fields {
			row = append(row, torrentDisplayFieldValue(units, torrent, field))
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	writer.Flush()
	return writer.Error()
}

func torrentTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"json": func(value any) string {
			body, err := json.Marshal(value)
			if err != nil {
				return ""
			}
			return string(body)
		},
		"field": func(field string, torrent TorrentInfo) string {
			return torrentFieldValue(torrent, canonicalTorrentField(field))
		},
	}
}

func renderTorrentTemplate(w io.Writer, name, tmpl string, data any) error {
	parsed, err := parseTorrentTemplate(name, tmpl)
	if err != nil {
		return err
	}
	return parsed.Execute(w, data)
}

func parseTorrentTemplate(name, tmpl string) (*template.Template, error) {
	return template.New(name).Funcs(torrentTemplateFuncs()).Parse(tmpl)
}
