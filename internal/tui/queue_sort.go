//go:build !plan9

package tui

import (
	"cmp"
	"strings"

	"github.com/naterator/qbitctl/pkg/client"
)

type queueSort string

const (
	queueActiveFirst  queueSort = "Active first"
	queueNameAsc      queueSort = "Name A-Z"
	queueNameDesc     queueSort = "Name Z-A"
	queueProgressAsc  queueSort = "Progress low to high"
	queueProgressDesc queueSort = "Progress high to low"
	queueStateAsc     queueSort = "State A-Z"
	queueStateDesc    queueSort = "State Z-A"
)

var queueSortChoices = []queueSort{
	queueActiveFirst, queueNameAsc, queueNameDesc, queueProgressAsc, queueProgressDesc,
	queueStateAsc, queueStateDesc,
}

func (m *model) setQueueSort(order queueSort) {
	if m.queueSort == order {
		return
	}
	m.queueSort = order
	m.rebuildQueue()
}

func (order queueSort) compare(left, right client.TorrentInfo) int {
	var comparison int
	switch order {
	case queueActiveFirst:
		leftActive := left.DLSpeed > 0 || left.UpSpeed > 0
		rightActive := right.DLSpeed > 0 || right.UpSpeed > 0
		if leftActive != rightActive {
			if leftActive {
				return -1
			}
			return 1
		}
	case queueNameDesc:
		return strings.Compare(strings.ToLower(right.Name), strings.ToLower(left.Name))
	case queueProgressAsc:
		comparison = cmp.Compare(left.Progress, right.Progress)
	case queueProgressDesc:
		comparison = cmp.Compare(right.Progress, left.Progress)
	case queueStateAsc:
		comparison = strings.Compare(strings.ToLower(left.State), strings.ToLower(right.State))
	case queueStateDesc:
		comparison = strings.Compare(strings.ToLower(right.State), strings.ToLower(left.State))
	}
	if comparison != 0 {
		return comparison
	}
	return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
}
