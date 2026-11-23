//go:build !plan9

package tui

import (
	"fmt"
	"strings"

	"github.com/naterator/qbitctl/pkg/client"
)

func shareLimitModeValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Default"
	}
	return value
}

func shareLimitActionValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Default"
	}
	return value
}

func shareLimitChoiceItems(field string) []dialogItem {
	values := client.ShareLimitsModeChoices()
	if field == "share-limit-action" {
		values = client.ShareLimitActionChoices()
	}
	items := make([]dialogItem, 0, len(values))
	for _, value := range values {
		items = append(items, dialogItem{
			label:       shareLimitChoiceLabel(value),
			action:      value,
			description: value,
		})
	}
	return items
}

func shareLimitChoiceLabel(value string) string {
	switch value {
	case "MatchAll":
		return "Match all limits"
	case "MatchAny":
		return "Match any limit"
	case "RemoveWithContent":
		return "Remove with content"
	case "EnableSuperSeeding":
		return "Enable super seeding"
	default:
		return value
	}
}

func shareLimitChoiceConfirmation(m *model, hashes, action string) string {
	count := len(strings.Split(hashes, "|"))
	noun := "torrents"
	if count == 1 {
		noun = "torrent"
	}
	lines := []string{
		fmt.Sprintf("Set share-limit action to %s for %d %s?", shareLimitChoiceLabel(action), count, noun),
		"This may remove the torrent from qBittorrent and cannot be undone.",
	}
	nameByHash := make(map[string]string, len(m.allTorrents)+len(m.queue))
	for _, torrent := range append(append([]client.TorrentInfo(nil), m.allTorrents...), m.queue...) {
		if _, exists := nameByHash[torrent.Hash]; !exists {
			nameByHash[torrent.Hash] = viewSafe(strings.TrimSpace(torrent.Name))
		}
	}
	parts := strings.Split(hashes, "|")
	for _, hash := range parts[:min(3, len(parts))] {
		name := nameByHash[hash]
		if name == "" {
			name = hash
		}
		lines = append(lines, "  · "+name)
	}
	if len(parts) > 3 {
		lines = append(lines, fmt.Sprintf("  · +%d more", len(parts)-3))
	}
	lines = append(lines, "Press Enter to apply or Esc to cancel.")
	return strings.Join(lines, "\n")
}
