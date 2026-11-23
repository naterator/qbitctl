//go:build !plan9

package tui

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

// writeOSC52 writes a terminal clipboard sequence. The tmux wrapper lets a
// remote qbitctl session pass the sequence through to the user's outer
// terminal when tmux is in the path.
func writeOSC52(w io.Writer, value string, tmux bool) error {
	if w == nil {
		return fmt.Errorf("terminal output is unavailable")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	sequence := "\x1b]52;c;" + encoded + "\a"
	if tmux {
		sequence = "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	_, err := io.WriteString(w, sequence)
	return err
}

func (m *model) copyToTerminal(value string) {
	if strings.TrimSpace(value) == "" {
		m.setNotice("Nothing available to copy", true)
		return
	}
	if err := writeOSC52(m.output, value, m.tmux); err != nil {
		m.setNotice("Copy failed: "+err.Error(), true)
		return
	}
	m.setNotice("Copied to terminal clipboard", false)
}

func torrentMagnet(torrent client.TorrentInfo) string {
	hash := strings.TrimSpace(torrent.Hash)
	if hash == "" {
		return ""
	}
	values := url.Values{}
	values.Set("xt", "urn:btih:"+hash)
	if name := strings.TrimSpace(torrent.Name); name != "" {
		values.Set("dn", name)
	}
	return "magnet:?" + values.Encode()
}

func magnetInfoHash(value string) string {
	target, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !strings.EqualFold(target.Scheme, "magnet") {
		return ""
	}
	const prefix = "urn:btih:"
	hash := strings.TrimSpace(target.Query().Get("xt"))
	if len(hash) < len(prefix) || !strings.EqualFold(hash[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(hash[len(prefix):])
}

func (m *model) openCopyDialog() tea.Cmd {
	items := make([]dialogItem, 0, 3)
	values := make(map[string]string)
	if m.page == queuePage {
		if torrent, ok := m.focusedTorrent(); ok {
			if hash := strings.TrimSpace(torrent.Hash); hash != "" {
				items = append(items, dialogItem{label: "Torrent hash", action: "hash", description: "copy the full info hash"})
				values["hash"] = hash
			}
			if magnet := torrentMagnet(torrent); magnet != "" {
				items = append(items, dialogItem{label: "Magnet link", action: "magnet", description: "copy a magnet URI for this torrent"})
				values["magnet"] = magnet
			}
		}
	} else if m.page == searchPage {
		if result, ok := m.focusedResult(); ok {
			if target := result.DownloadURL(); target != "" {
				if hash := magnetInfoHash(target); hash != "" {
					items = append(items, dialogItem{label: "Torrent hash", action: "hash", description: "copy the hash from the magnet URI"})
					values["hash"] = hash
				}
				label, description := "Download URL", "copy the plugin's result URL"
				if strings.HasPrefix(strings.ToLower(target), "magnet:") {
					label, description = "Magnet link", "copy the plugin's magnet URI"
				}
				items = append(items, dialogItem{label: label, action: "result", description: description})
				values["result"] = target
			}
		}
	}
	if len(items) == 0 {
		m.setNotice("No hash or magnet is available for the selection", false)
		return nil
	}
	m.dialog = &dialog{kind: dialogCopy, title: "Copy selection", items: items, selected: 0, copyValues: values}
	return nil
}
