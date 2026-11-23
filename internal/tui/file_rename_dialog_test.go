//go:build !plan9

package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

type fileRenameBackend struct {
	*liveDetailsService
	file       client.TorrentFile
	renameKind string
	hash       string
	oldPath    string
	newPath    string
}

func (b *fileRenameBackend) files(context.Context, string) ([]client.TorrentFile, error) {
	return []client.TorrentFile{b.file}, nil
}

func (b *fileRenameBackend) renameFile(_ context.Context, hash, oldPath, newPath string) error {
	b.renameKind, b.hash, b.oldPath, b.newPath = "file", hash, oldPath, newPath
	b.file.Name = newPath
	return nil
}

func (b *fileRenameBackend) renameFolder(_ context.Context, hash, oldPath, newPath string) error {
	b.renameKind, b.hash, b.oldPath, b.newPath = "folder", hash, oldPath, newPath
	b.file.Name = newPath + strings.TrimPrefix(b.file.Name, oldPath)
	return nil
}

func TestFilesTabRenameForms(t *testing.T) {
	torrent := liveDetailsTorrent("Example", "hash", 0.5)
	backend := &fileRenameBackend{
		liveDetailsService: &liveDetailsService{
			rows:     []detailRow{{label: "Name", value: torrent.Name}},
			torrents: []client.TorrentInfo{torrent},
		},
		file: client.TorrentFile{Name: "Root/Sub/ old.bin "},
	}
	m := newLiveDetailsModel(t, backend.liveDetailsService, torrent)
	m.backend = backend
	finishLiveDetailsCommand(t, m, m.loadDetails(false))
	finishLiveDetailsCommand(t, m, m.loadDetailTab(detailTabFiles))
	parent := m.dialog

	m.update(tea.KeyPressMsg{Code: tea.KeyF2})
	if m.dialog == parent || m.dialog.value(0) != backend.file.Name {
		t.Fatal("F2 did not prefill the exact current file path")
	}
	m.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog != parent || backend.renameKind != "" {
		t.Fatal("canceling the rename changed the parent or called the server")
	}

	m.update(tea.KeyPressMsg{Code: tea.KeyF2})
	m.dialog.fields[0].input.SetValue("")
	if cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || m.dialog.status == "" {
		t.Fatal("an empty path was not rejected")
	}
	m.dialog.fields[0].input.SetValue("Root/Sub/new.bin")
	finishLiveDetailsCommand(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if backend.renameKind != "file" || backend.hash != torrent.Hash ||
		backend.oldPath != "Root/Sub/ old.bin " || backend.newPath != "Root/Sub/new.bin" {
		t.Fatalf("unexpected file rename request: %+v", backend)
	}
	if m.dialog != parent || parent.detail.files[0].Name != "Root/Sub/new.bin" {
		t.Fatal("file list did not refresh after the rename")
	}

	m.update(tea.KeyPressMsg{Code: tea.KeyF3})
	if m.dialog == parent || m.dialog.value(0) != "Root/Sub" {
		t.Fatal("F3 did not prefill the selected file's folder")
	}
	m.dialog.fields[0].input.SetValue("Root/Renamed")
	finishLiveDetailsCommand(t, m, m.update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if backend.renameKind != "folder" || backend.oldPath != "Root/Sub" || backend.newPath != "Root/Renamed" ||
		parent.detail.files[0].Name != "Root/Renamed/new.bin" {
		t.Fatalf("unexpected folder rename request or refreshed file: %+v", backend)
	}
}
