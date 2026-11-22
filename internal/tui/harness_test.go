//go:build !plan9

package tui

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

// recordingService lets model tests assert the exact request sent by an
// action without starting a Bubble Tea program or waiting on timer commands.
type recordingService struct {
	mu         sync.Mutex
	torrents   []client.TorrentInfo
	actions    []actionRequest
	actionErr  error
	block      chan struct{}
	started    chan struct{}
	queueCalls int
}

func (s *recordingService) queue(context.Context) ([]client.TorrentInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queueCalls++
	return append([]client.TorrentInfo(nil), s.torrents...), nil
}

func (s *recordingService) search(context.Context, string, string, time.Duration) ([]client.SearchResult, string, error) {
	return nil, "", nil
}

func (s *recordingService) server(context.Context) ([]detailRow, error) { return nil, nil }

func (s *recordingService) details(context.Context, string, bool) ([]detailRow, error) {
	return nil, nil
}

func (s *recordingService) action(_ context.Context, request actionRequest) error {
	s.mu.Lock()
	s.actions = append(s.actions, request)
	err := s.actionErr
	block, started := s.block, s.started
	s.started = nil
	s.mu.Unlock()
	if started != nil {
		close(started)
	}
	if block != nil {
		<-block
	}
	return err
}

func (s *recordingService) recordedActions() []actionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]actionRequest(nil), s.actions...)
}

func (s *recordingService) queueCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueCalls
}

func seedRecordingQueue(m *model, service *recordingService, torrents ...client.TorrentInfo) {
	service.torrents = append([]client.TorrentInfo(nil), torrents...)
	m.page = queuePage
	m.replaceQueue(torrents)
}

func runTUIWork(t *testing.T, m *model, command tea.Cmd) {
	t.Helper()
	if command == nil {
		return
	}
	message := command()
	switch message := message.(type) {
	case workDone:
		runTUIWork(t, m, m.finishWork(message))
	case tea.BatchMsg:
		for _, nested := range message {
			runTUIWork(t, m, nested)
		}
	}
}

func pressTUIKey(m *model, code rune) tea.Cmd {
	return m.update(tea.KeyPressMsg{Code: code})
}
