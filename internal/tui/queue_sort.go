//go:build !plan9

package tui

import (
	"cmp"
	"math"
	"strings"

	"github.com/naterator/qbitctl/internal/configfile"
	"github.com/naterator/qbitctl/pkg/client"
)

type queueSort string

const (
	queueActiveFirst  queueSort = configfile.QueueActiveFirst
	queueNameAsc      queueSort = configfile.QueueNameAsc
	queueNameDesc     queueSort = configfile.QueueNameDesc
	queueProgressAsc  queueSort = configfile.QueueProgressAsc
	queueProgressDesc queueSort = configfile.QueueProgressDesc
	queueStateAsc     queueSort = configfile.QueueStateAsc
	queueStateDesc    queueSort = configfile.QueueStateDesc
	queueSizeDesc     queueSort = configfile.QueueSizeDesc
	queueRatioDesc    queueSort = configfile.QueueRatioDesc
	queueETAAsc       queueSort = configfile.QueueETAAsc
	queueDownloadDesc queueSort = configfile.QueueDownloadDesc
	queueUploadDesc   queueSort = configfile.QueueUploadDesc
)

var queueSortChoices = []struct {
	order queueSort
	label string
}{
	{queueActiveFirst, "Active first"},
	{queueNameAsc, "Name A-Z"},
	{queueNameDesc, "Name Z-A"},
	{queueProgressAsc, "Progress low to high"},
	{queueProgressDesc, "Progress high to low"},
	{queueStateAsc, "State A-Z"},
	{queueStateDesc, "State Z-A"},
	{queueSizeDesc, "Largest first"},
	{queueRatioDesc, "Highest ratio"},
	{queueETAAsc, "Shortest ETA"},
	{queueDownloadDesc, "Highest download speed"},
	{queueUploadDesc, "Highest upload speed"},
}

func (order queueSort) label() string {
	for _, choice := range queueSortChoices {
		if choice.order == order {
			return choice.label
		}
	}
	return ""
}

func (m *model) setQueueSort(order queueSort) {
	if m.queueSort != order {
		m.queueSort = order
		m.rebuildQueue()
	}
	if m.preferencesPath == "" {
		return
	}
	if err := saveQueuePreferences(m.preferencesPath, order); err != nil {
		m.setNotice("Could not save queue sort. "+err.Error(), true)
		return
	}
	m.setNotice("Queue sort saved", false)
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
	case queueSizeDesc:
		comparison = cmp.Compare(right.TotalSize, left.TotalSize)
	case queueRatioDesc:
		comparison = cmp.Compare(queueRatio(right.Ratio), queueRatio(left.Ratio))
	case queueETAAsc:
		comparison = cmp.Compare(queueETA(left.ETA), queueETA(right.ETA))
	case queueDownloadDesc:
		comparison = cmp.Compare(right.DLSpeed, left.DLSpeed)
	case queueUploadDesc:
		comparison = cmp.Compare(right.UpSpeed, left.UpSpeed)
	}
	if comparison != 0 {
		return comparison
	}
	return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
}

func queueRatio(ratio float64) float64 {
	if ratio == -1 { // qBittorrent serializes an unbounded share ratio as -1.
		return math.Inf(1)
	}
	return ratio
}

func queueETA(seconds int64) int64 {
	if seconds < 0 {
		return math.MaxInt64
	}
	return seconds
}
