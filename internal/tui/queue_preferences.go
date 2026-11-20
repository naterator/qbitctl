//go:build !plan9

package tui

import "fmt"

const queuePreferencesFileName = "tui-queue.json"

type queuePreferences struct {
	Sort queueSort `json:"sort"`
}

func loadQueuePreferences(path string) (queueSort, error) {
	disk := queuePreferences{Sort: queueActiveFirst}
	if err := readPreferences(path, &disk); err != nil {
		return queueActiveFirst, err
	}
	if disk.Sort.label() == "" {
		return queueActiveFirst, fmt.Errorf("invalid queue sort %q", disk.Sort)
	}
	return disk.Sort, nil
}

func saveQueuePreferences(path string, order queueSort) error {
	if order.label() == "" {
		return fmt.Errorf("invalid queue sort %q", order)
	}
	return writePreferences(path, queuePreferences{Sort: order})
}

func (m *model) loadQueuePreferences(path string) {
	m.queuePreferencesFile = path
	order, err := loadQueuePreferences(path)
	if err != nil {
		if m.notice != "" {
			m.notice += " "
		}
		m.notice += "Could not load queue sort. " + err.Error()
		return
	}
	m.queueSort = order
}
