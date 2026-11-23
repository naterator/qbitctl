//go:build !plan9

package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *model) handleMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	if m.dialog != nil {
		return m.handleDialogMouseClick(msg.X, msg.Y)
	}
	if msg.Y == 1 {
		return m.handleTabMouseClick(msg.X)
	}
	if msg.Y < viewHeaderLines {
		return nil
	}
	index, ok := m.mouseListIndex(msg.X, msg.Y)
	if !ok {
		return nil
	}
	if m.page == queuePage {
		if m.queueIndex == index && m.mouseClickWithin(index) {
			return m.loadDetails(false)
		}
		m.lastMouseIndex, m.lastMouseAt = index, time.Now()
		m.queueIndex = index
	} else if m.page == searchPage {
		if m.resultIndex == index && m.mouseClickWithin(index) {
			return m.openAction("add-result")
		}
		m.lastMouseIndex, m.lastMouseAt = index, time.Now()
		m.resultIndex = index
	}
	return nil
}

func (m *model) handleTabMouseClick(x int) tea.Cmd {
	width := viewContentWidth(max(1, m.width))
	start := 0
	if m.width >= 40 {
		start = 2
	}
	for index, target := range []page{queuePage, searchPage, serverPage, configPage} {
		label := m.viewTab([]string{"1 Queue", "2 Search", "3 Server", "4 Config"}[index], target)
		end := start + lipgloss.Width(ansi.Strip(label))
		if x >= start && x < end {
			return m.goTo(target)
		}
		start = end + 3
		if start >= width {
			break
		}
	}
	return nil
}

func (m *model) handleMouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	delta := 1
	if msg.Button == tea.MouseWheelUp {
		delta = -1
	} else if msg.Button != tea.MouseWheelDown {
		return nil
	}
	if m.dialog != nil {
		d := m.dialog
		switch d.kind {
		case dialogDetails:
			if d.torrentDetails != nil {
				key := "down"
				if delta < 0 {
					key = "up"
				}
				if handled, cmd := m.updateTorrentDetailKey(d, key); handled {
					return cmd
				}
				innerWidth, _ := dialogDimensions(m.width)
				visible := detailBodyHeight(d, m.heightForDialog())
				d.stepScroll(delta, visible, len(m.detailBodyLines(d, innerWidth)))
				return nil
			}
			visible := dialogBodyHeight(m.heightForDialog())
			d.stepScroll(delta, visible, max(1, len(d.rows)))
		case dialogHelp:
			visible := dialogBodyHeight(m.heightForDialog())
			d.stepScroll(delta, visible, max(1, len(d.rows)))
		default:
			if len(d.items) > 0 {
				d.stepSelection(delta)
			}
		}
		return nil
	}
	m.moveCursor(delta)
	return nil
}

func (m *model) handleDialogMouseClick(x, y int) tea.Cmd {
	d := m.dialog
	if d == nil || len(d.items) == 0 {
		return nil
	}
	list := d.kind == dialogPalette || d.kind == dialogEditSelect || d.kind == dialogShareChoice || d.kind == dialogResultSort ||
		d.kind == dialogQueueSort || d.kind == dialogQueueColumns || d.kind == dialogCopy ||
		d.kind == dialogTheme || d.kind == dialogSearchPlugins || d.kind == dialogSearchPluginManage || d.kind == dialogSearchPluginActions || d.kind == dialogProfilePicker
	if !list {
		return nil
	}
	panel := m.dialogPanelBounds()
	if x < panel.left || x >= panel.right || y < panel.top || y >= panel.bottom {
		return nil
	}
	headerRows := 1
	if panel.height >= 11 {
		headerRows = 3
	}
	bodyY := panel.top + headerRows
	if y < bodyY {
		return nil
	}
	visible := d.bodyHeight(m.heightForDialog())
	index := d.listStart(visible) + y - bodyY
	if index < 0 || index >= len(d.items) || d.items[index].disabled {
		return nil
	}
	if d.selected == index && m.dialogMouseClickWithin(index) {
		return m.updateDialog(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	m.lastDialogMouseIndex, m.lastDialogMouseAt = index, time.Now()
	d.selected = index
	return nil
}

type dialogPanelBounds struct{ left, top, right, bottom, width, height int }

func (m *model) dialogPanelBounds() dialogPanelBounds {
	width, height := m.width, m.height
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	p := m.dialogView(width, height)
	panelWidth, panelHeight := lipgloss.Width(p), lipgloss.Height(p)
	return dialogPanelBounds{
		left: (width - panelWidth) / 2, top: (height - panelHeight) / 2,
		right: (width-panelWidth)/2 + panelWidth, bottom: (height-panelHeight)/2 + panelHeight,
		width: panelWidth, height: panelHeight,
	}
}

func (m *model) mouseListIndex(x, y int) (int, bool) {
	contentWidth := viewContentWidth(max(1, m.width))
	height := m.height
	if height < 1 {
		height = 24
	}
	if m.page == queuePage {
		if len(m.queue) == 0 {
			return 0, false
		}
		if y >= height-viewFooterLines {
			return 0, false
		}
		if viewHasDetail(contentWidth) {
			rightWidth := viewDetailWidth(contentWidth)
			leftWidth := max(1, contentWidth-rightWidth-1)
			if x >= leftWidth+2 {
				return 0, false
			}
		}
		stats := 0
		if m.queueTransfer.download != nil || m.queueTransfer.upload != nil || m.queueTransfer.free != nil {
			stats = 1
		}
		rowStart := viewHeaderLines + 1 + stats + 1
		if y < rowStart {
			return 0, false
		}
		visible := max(1, height-viewHeaderLines-viewFooterLines-1-stats-1)
		index := viewWindowStart(m.queueIndex, len(m.queue), visible) + y - rowStart
		return index, index >= 0 && index < len(m.queue)
	}
	if m.page == searchPage {
		if len(m.results) == 0 {
			return 0, false
		}
		if y >= height-viewFooterLines {
			return 0, false
		}
		if viewHasDetail(contentWidth) {
			rightWidth := viewDetailWidth(contentWidth)
			leftWidth := max(1, contentWidth-rightWidth-1)
			if x >= leftWidth+2 {
				return 0, false
			}
		}
		rowStart := viewHeaderLines + 1 + m.resultFilterVisualHeight() + 1
		if y < rowStart {
			return 0, false
		}
		visible := max(1, height-viewHeaderLines-viewFooterLines-1-1-m.resultFilterVisualHeight()-1)
		index := viewWindowStart(m.resultIndex, len(m.results), visible) + y - rowStart
		return index, index >= 0 && index < len(m.results)
	}
	return 0, false
}

func (m *model) mouseClickWithin(index int) bool {
	if m.lastMouseIndex == index && time.Since(m.lastMouseAt) < 500*time.Millisecond {
		m.lastMouseAt = time.Time{}
		return true
	}
	m.lastMouseIndex, m.lastMouseAt = index, time.Now()
	return false
}

func (m *model) dialogMouseClickWithin(index int) bool {
	if m.lastDialogMouseIndex == index && time.Since(m.lastDialogMouseAt) < 500*time.Millisecond {
		m.lastDialogMouseAt = time.Time{}
		return true
	}
	m.lastDialogMouseIndex, m.lastDialogMouseAt = index, time.Now()
	return false
}
