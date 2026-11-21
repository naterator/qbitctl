//go:build !plan9

package tui

import (
	"encoding/json"
	"fmt"
)

const queuePreferencesFileName = "tui-queue.json"

type queuePreferences struct {
	Sort queueSort `json:"sort"`
}

func loadQueuePreferences(path string) (queueSort, error) {
	data, err := readPreferences(path, "queue")
	if err != nil {
		return queueActiveFirst, err
	}
	return decodeQueuePreferences(data)
}

func decodeQueuePreferences(data json.RawMessage) (queueSort, error) {
	disk := queuePreferences{Sort: queueActiveFirst}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &disk); err != nil {
			return queueActiveFirst, err
		}
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
	return writePreferences(path, "queue", map[string]string{"sort": string(order)})
}
