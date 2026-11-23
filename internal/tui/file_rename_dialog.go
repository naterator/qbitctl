//go:build !plan9

package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type torrentFileRenameService interface {
	renameFile(context.Context, string, string, string) error
	renameFolder(context.Context, string, string, string) error
}

type torrentPathRename struct {
	parent  *dialog
	oldPath string
	folder  bool
}

func (m *model) openTorrentPathRename(parent *dialog, files []client.TorrentFile, folder bool) tea.Cmd {
	if m.busy != "" {
		parent.status = "Finish the current request first"
		return nil
	}
	if parent.detail.fileCursor >= len(files) {
		parent.status = "Select a file first"
		return nil
	}
	if _, ok := m.backend.(torrentFileRenameService); !ok {
		parent.status = "Renaming is unavailable for this connection"
		return nil
	}

	oldPath := files[parent.detail.fileCursor].Name
	title := "Rename file"
	if folder {
		// File paths use the server's torrent-relative slash syntax even when
		// qbitctl itself is running on Windows.
		end := strings.LastIndexByte(oldPath, '/')
		if end <= 0 {
			parent.status = "The selected file has no folder to rename"
			return nil
		}
		oldPath = oldPath[:end]
		title = "Rename folder"
	}
	d := &dialog{
		kind:       dialogRename,
		title:      title,
		fields:     []dialogField{newDialogField("New path", oldPath, "", "Enter a path relative to the torrent")},
		renamePath: &torrentPathRename{parent: parent, oldPath: oldPath, folder: folder},
	}
	m.dialog = d
	return d.focusField(0)
}

func (m *model) submitTorrentPathRename(d *dialog) tea.Cmd {
	if m.busy != "" {
		d.status = "Finish the current request first"
		return nil
	}
	newPath := d.value(0)
	if strings.TrimSpace(newPath) == "" {
		d.status = "Enter a new path"
		return nil
	}
	request := *d.renamePath
	parent := request.parent
	m.dialog = parent
	if newPath == request.oldPath {
		return nil
	}
	rename, ok := m.backend.(torrentFileRenameService)
	source, canList := m.backend.(torrentFilesService)
	if !ok || !canList {
		parent.status = "Renaming is unavailable for this connection"
		return nil
	}
	hash := parent.torrentDetails.hash
	renamePath := rename.renameFile
	label := "Renaming file"
	if request.folder {
		renamePath = rename.renameFolder
		label = "Renaming folder"
	}
	return m.startWork(detailTabWork, label, func(ctx context.Context) workDone {
		update := &detailTabUpdate{dialog: parent, tab: detailTabFiles}
		update.err = renamePath(ctx, hash, request.oldPath, newPath)
		if update.err == nil {
			update.files, update.err = source.files(ctx, hash)
		}
		return workDone{detailTabUpdate: update, err: update.err}
	})
}
