package client

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/template"
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
	"progress",
}

// SetFieldNames lists all field names available for the set command.
var SetFieldNames = []string{
	"autotmm",
	"category",
	"dl-limit",
	"ratio-limit",
	"seedtime-limit",
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
	default:
		return ""
	}
}

func writeTorrentFieldRows(w io.Writer, torrents []TorrentInfo, fields []string, header bool) error {
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
			row = append(row, torrentFieldValue(torrent, field))
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
	parsed, err := template.New(name).Funcs(torrentTemplateFuncs()).Parse(tmpl)
	if err != nil {
		return err
	}
	return parsed.Execute(w, data)
}
