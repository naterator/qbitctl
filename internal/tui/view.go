//go:build !plan9

package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/pkg/client"
)

// The accent follows the terminal's palette. Selection and focus also have
// visible markers for terminals without color.
var (
	viewAccentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("37"))
	viewBrandStyle  = viewAccentStyle.Bold(true)
	viewTitleStyle  = viewAccentStyle.Bold(true)
	viewMutedStyle  = lipgloss.NewStyle().Faint(true)
	viewSelectedSty = lipgloss.NewStyle().Reverse(true)
	viewErrorStyle  = lipgloss.NewStyle().Bold(true)
)

const (
	viewHeaderLines = 3
	viewFooterLines = 2
)

// View renders a complete terminal frame.  Keeping the frame bounded here is
// useful even while a remote server is returning malformed names or tracker
// values: no value can introduce a newline, escape sequence, or a row wider
// than the terminal.
func (m *model) View() tea.View {
	width, height := m.width, m.height
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}

	var content string
	if m.dialog != nil {
		// dialogView owns the complete overlay and is responsible for keeping
		// its own controls inside the available frame.
		content = lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, m.dialogView(width, height))
	} else {
		content = m.viewScreen(width, height)
	}

	v := tea.NewView(viewFillScreen(content, width, height))
	v.AltScreen = true
	return v
}

// tableHeight is shared by the update code when it pages through queue and
// search results. It intentionally reflects the compact layout; a side panel
// never changes the number of rows available in the main table.
func (m *model) tableHeight() int {
	height := m.height
	if height < 1 {
		height = 24
	}
	// The panel consumes two border rows and the table consumes one heading
	// row, leaving this many actual result rows for a page movement.
	rows := height - viewHeaderLines - viewFooterLines - 3
	if m.page == searchPage {
		// Search results always have a filter and sort summary. While the
		// result filter editor is open that summary grows by one line for the
		// field tabs and the focused input.
		rows -= m.resultFilterVisualHeight()
	}
	return max(1, rows)
}

func (m *model) resultFilterVisualHeight() int {
	if m.resultEditor != nil {
		// The summary row stays in place while editing; the tabs and focused
		// input are added below it.
		return 3
	}
	return 1
}

func (m *model) viewScreen(width, height int) string {
	header := m.viewHeader(width)
	footer := m.viewFooter(width)
	bodyHeight := max(1, height-len(header)-len(footer))
	body := m.viewBody(width, bodyHeight)

	lines := make([]string, 0, height)
	lines = append(lines, header...)
	lines = append(lines, body...)
	lines = append(lines, footer...)
	return strings.Join(viewFitLines(lines, width, height), "\n")
}

func (m *model) viewHeader(width int) []string {
	pageName := m.viewPageName()
	state := m.viewStatus()
	brand := viewBrandStyle.Render("qbitctl") + "  " + viewTitleStyle.Render(pageName)
	first := viewRightAligned(width, brand, state)

	tabs := []string{
		m.viewTab("1 Queue", queuePage),
		m.viewTab("2 Search", searchPage),
		m.viewTab("3 Server", serverPage),
	}
	context := ""
	switch m.page {
	case queuePage:
		if strings.TrimSpace(m.filter) != "" {
			context = "filter: /" + viewSafe(m.filter)
		}
	case searchPage:
		if strings.TrimSpace(m.query) != "" {
			context = "query: " + viewSafe(m.query)
		}
	}
	second := strings.Join(tabs, "   ")
	if context != "" {
		second += "   " + viewMutedStyle.Render(context)
	}
	return []string{first, second, viewRule(width)}
}

func (m *model) viewTab(label string, p page) string {
	if m.page == p {
		return viewSelectedSty.Render("[" + label + "]")
	}
	return viewMutedStyle.Render(" " + label + " ")
}

func (m *model) viewPageName() string {
	switch m.page {
	case searchPage:
		return "Search"
	case serverPage:
		return "Server"
	default:
		return "Queue"
	}
}

func (m *model) viewBody(width, height int) []string {
	switch m.page {
	case searchPage:
		return m.viewSearch(width, height)
	case serverPage:
		return m.viewServer(width, height)
	default:
		return m.viewQueue(width, height)
	}
}

func (m *model) viewQueue(width, height int) []string {
	title := "QUEUE"
	if m.queueSort != queueActiveFirst {
		title += " · " + m.queueSort.label()
	}
	if viewHasDetail(width) {
		rightWidth := viewDetailWidth(width)
		leftWidth := max(1, width-rightWidth-1)
		left := m.viewPanel(title, m.viewQueueList(leftWidth-2, height-2), leftWidth, height)
		right := m.viewPanel("DETAIL", m.viewTorrentDetail(rightWidth-2), rightWidth, height)
		return viewColumns(left, right, leftWidth, rightWidth, height)
	}
	return m.viewPanel(title, m.viewQueueList(width-2, height-2), width, height)
}

func (m *model) viewQueueList(width, height int) []string {
	width = max(1, width)
	rows := []string{m.viewQueueHeader(width)}
	if len(m.queue) == 0 {
		if m.busy != "" {
			rows = append(rows, viewMutedStyle.Render("Waiting for queue data…"))
		} else {
			rows = append(rows, viewMutedStyle.Render("No torrents in the current view."))
		}
		return viewFitLines(rows, width, max(1, height))
	}

	rowHeight := max(1, height-1)
	start := viewWindowStart(m.queueIndex, len(m.queue), rowHeight)
	end := min(len(m.queue), start+rowHeight)
	for i := start; i < end; i++ {
		rows = append(rows, m.viewQueueRow(m.queue[i], i == m.queueIndex, width))
	}
	return viewFitLines(rows, width, max(1, height))
}

func (m *model) viewQueueHeader(width int) string {
	showSpeed := width >= 88
	nameWidth, _, _, _ := viewQueueColumns(width, showSpeed)
	parts := []string{
		viewColumn("", 2),
		viewColumn("NAME", nameWidth),
		viewColumn("PROGRESS", 16),
		viewColumn("STATE", 13),
	}
	if showSpeed {
		parts = append(parts, viewColumn("DOWN / UP", 20))
	}
	return viewMutedStyle.Render(strings.Join(parts, " "))
}

func (m *model) viewQueueRow(t client.TorrentInfo, selected bool, width int) string {
	showSpeed := width >= 88
	nameWidth, _, _, speedWidth := viewQueueColumns(width, showSpeed)
	marker := "  "
	if m.marked != nil && m.marked[t.Hash] {
		marker = "* "
	}
	if selected {
		marker = "› "
		if m.marked != nil && m.marked[t.Hash] {
			marker = "›*"
		}
	}
	parts := []string{
		viewColumn(marker, 2),
		viewColumn(viewSafe(t.Name), nameWidth),
		viewColumn(viewProgress(t.Progress, 16), 16),
		viewColumn(viewSafe(t.State), 13),
	}
	if showSpeed {
		parts = append(parts, viewColumn(viewSpeed(t.DLSpeed)+" / "+viewSpeed(t.UpSpeed), speedWidth))
	}
	line := strings.Join(parts, " ")
	if selected {
		return viewSelectedSty.Width(width).Render(viewFit(line, width))
	}
	return line
}

func (m *model) viewTorrentDetail(width int) []string {
	t, ok := m.focusedTorrent()
	if !ok {
		return []string{"Select a torrent to inspect it.", "", "Enter opens details."}
	}
	return []string{
		viewKeyValue("Name", viewSafe(t.Name), width),
		viewKeyValue("State", viewSafe(t.State), width),
		viewKeyValue("Progress", viewProgress(t.Progress, 16), width),
		viewKeyValue("Size", viewBytes(t.TotalSize), width),
		viewKeyValue("Down", viewSpeed(t.DLSpeed), width),
		viewKeyValue("Up", viewSpeed(t.UpSpeed), width),
		viewKeyValue("Ratio", fmt.Sprintf("%.2f", t.Ratio), width),
		viewKeyValue("Tracker", viewSafe(t.Tracker), width),
		viewKeyValue("Path", viewSafe(t.ContentPath), width),
		"",
		viewMutedStyle.Render("Enter details · e edit · a add"),
	}
}

func (m *model) viewSearch(width, height int) []string {
	if viewHasDetail(width) {
		rightWidth := viewDetailWidth(width)
		leftWidth := max(1, width-rightWidth-1)
		left := m.viewPanel("SEARCH", m.viewSearchList(leftWidth-2, height-2), leftWidth, height)
		right := m.viewPanel("RESULT", m.viewResultDetail(rightWidth-2), rightWidth, height)
		return viewColumns(left, right, leftWidth, rightWidth, height)
	}
	return m.viewPanel("SEARCH", m.viewSearchList(width-2, height-2), width, height)
}

func (m *model) viewSearchList(width, height int) []string {
	width = max(1, width)
	rows := []string{m.viewResultControls()}
	if m.resultEditor != nil {
		rows = append(rows, m.viewResultFilterEditor(width)...)
	}
	rows = append(rows, m.viewSearchHeader(width))
	if len(m.results) == 0 {
		message := "Press / to search Jackett."
		if m.busy != "" && m.work == searchWork {
			message = "Waiting for results…"
		} else if len(m.allResults) > 0 {
			message = "No results match the filter."
		} else if strings.TrimSpace(m.query) != "" {
			message = "No results found."
		}
		rows = append(rows, viewMutedStyle.Render(message))
		return viewFitLines(rows, width, max(1, height))
	}

	rowHeight := max(1, height-len(rows))
	start := viewWindowStart(m.resultIndex, len(m.results), rowHeight)
	end := min(len(m.results), start+rowHeight)
	for i := start; i < end; i++ {
		rows = append(rows, m.viewSearchRow(m.results[i], i == m.resultIndex, width))
	}
	return viewFitLines(rows, width, max(1, height))
}

const resultFilterPrompt = "Filter "

func (m *model) resultFilterInputWidth() int {
	width := m.width
	if viewHasDetail(width) {
		width -= viewDetailWidth(width) + 1
	}
	prefix := resultFilterPrompt
	if m.resultEditor != nil {
		active := max(0, min(m.resultEditor.active, len(resultFilterFields)-1))
		prefix += resultFilterFields[active].label + " "
	}
	return max(1, width-2-lipgloss.Width(prefix)-1)
}

func (m *model) viewResultControls() string {
	summary := fmt.Sprintf("%d results  ·  %s", len(m.results), m.resultSort.label())
	if m.resultFilter.active() {
		parts := []string{
			fmt.Sprintf("%d of %d results", len(m.results), len(m.allResults)),
			m.resultSort.label(),
		}
		// Put bounded fields before free-form text. A long title/source query
		// should be the part that truncates, while the useful numeric bounds
		// remain visible in a compact terminal.
		if value := strings.TrimSpace(m.resultFilter.minSeeders.text); value != "" {
			parts = append(parts, "seeds ≥ "+viewSafe(value))
		}
		minSize := strings.TrimSpace(m.resultFilter.minSize.text)
		maxSize := strings.TrimSpace(m.resultFilter.maxSize.text)
		switch {
		case minSize != "" && maxSize != "":
			parts = append(parts, "size "+viewSafe(minSize)+"–"+viewSafe(maxSize))
		case minSize != "":
			parts = append(parts, "size ≥ "+viewSafe(minSize))
		case maxSize != "":
			parts = append(parts, "size ≤ "+viewSafe(maxSize))
		}
		if query := strings.TrimSpace(m.resultFilter.query); query != "" {
			parts = append(parts, "text: "+viewSafe(query))
		}
		summary = strings.Join(parts, "  ·  ")
	}
	return viewMutedStyle.Render(summary)
}

func (m *model) viewResultFilterEditor(width int) []string {
	if m.resultEditor == nil || len(resultFilterFields) == 0 {
		return nil
	}
	active := max(0, min(m.resultEditor.active, len(resultFilterFields)-1))

	// Keep all four fields on one line. The active marker is deliberately
	// textual so the editor remains legible in monochrome terminals and at
	// the compact 76-column layout.
	tabs := make([]string, 0, len(resultFilterFields))
	for index, field := range resultFilterFields {
		marker := "  "
		if index == active {
			marker = "› "
		}
		label := marker + viewSafe(field.label)
		if index == active {
			label = viewAccentStyle.Render(label)
		} else {
			label = viewMutedStyle.Render(label)
		}
		tabs = append(tabs, label)
	}

	field := resultFilterFields[active]
	prefix := resultFilterPrompt + viewSafe(field.label) + " "
	input := m.resultEditor.inputs[active].View()
	return []string{
		viewFit(strings.Join(tabs, "  "), width),
		viewAccentStyle.Render(prefix) + input,
	}
}

func (m *model) viewSearchHeader(width int) string {
	nameWidth, _, _, _ := viewSearchColumns(width)
	parts := []string{
		viewColumn("", 2),
		viewColumn("NAME", nameWidth),
		viewColumn("ID", 12),
		viewColumn("SIZE", 10),
		viewColumn("SEEDS", 6),
		viewColumn("SOURCE", 16),
	}
	return viewMutedStyle.Render(strings.Join(parts, " "))
}

func (m *model) viewSearchRow(r client.SearchResult, selected bool, width int) string {
	nameWidth, _, _, _ := viewSearchColumns(width)
	id := "—"
	if r.ID > 0 {
		id = fmt.Sprintf("%d", r.ID)
	}
	parts := []string{
		viewColumn(map[bool]string{true: "› ", false: "  "}[selected], 2),
		viewColumn(viewSafe(r.Name), nameWidth),
		viewColumn(id, 12),
		viewColumn(viewBytes(r.Size), 10),
		viewColumn(fmt.Sprintf("%d", r.Seeders), 6),
		viewColumn(viewSafe(r.Source), 16),
	}
	line := strings.Join(parts, " ")
	if selected {
		return viewSelectedSty.Width(width).Render(viewFit(line, width))
	}
	return line
}

func (m *model) viewResultDetail(width int) []string {
	r, ok := m.focusedResult()
	if !ok {
		return []string{"Select a result to inspect it.", "", "Enter adds the selected result."}
	}
	id := "not cached"
	if r.ID > 0 {
		id = fmt.Sprintf("%d", r.ID)
	}
	return []string{
		viewKeyValue("Name", viewSafe(r.Name), width),
		viewKeyValue("ID", id, width),
		viewKeyValue("Size", viewBytes(r.Size), width),
		viewKeyValue("Seeders", fmt.Sprintf("%d", r.Seeders), width),
		viewKeyValue("Leechers", fmt.Sprintf("%d", r.Leechers), width),
		viewKeyValue("Source", viewSafe(r.Source), width),
		"",
		viewMutedStyle.Render("Enter add · a add · / new search"),
	}
}

func (m *model) viewServer(width, height int) []string {
	rows := make([]string, 0, len(m.serverRows)+1)
	if len(m.serverRows) == 0 {
		if m.busy != "" {
			rows = append(rows, viewMutedStyle.Render("Waiting for server information…"))
		} else {
			rows = append(rows, viewMutedStyle.Render("No server information loaded. Press r to refresh."))
		}
	} else {
		rows = append(rows, viewMutedStyle.Render(viewColumn("SETTING", max(1, width/3))+" "+"VALUE"))
		for _, row := range m.serverRows {
			label := viewSafe(row.label)
			value := viewSafe(row.value)
			rows = append(rows, viewColumn(label, max(1, width/3))+" "+value)
		}
	}
	return m.viewPanel("SERVER", rows, width, height)
}

func (m *model) viewFooter(width int) []string {
	status := m.viewStatus()
	if m.failed {
		status = viewErrorStyle.Render("ERROR")
	}
	if m.notice != "" {
		if status == "Ready" {
			status = viewSafe(m.notice)
		} else {
			status += " · " + viewSafe(m.notice)
		}
	}
	if m.resultEditor != nil && strings.TrimSpace(m.resultEditor.problem) != "" {
		// A parse error belongs to the draft currently being edited. Prefer it
		// over an older notice so it remains readable in the compact footer.
		status = viewErrorStyle.Render(viewSafe(strings.TrimSpace(m.resultEditor.problem)))
	}
	statusLine := viewRightAligned(width, status, "")

	hint := ""
	switch m.page {
	case searchPage:
		hint = "/ search  s sort  f filter  Enter add  Ctrl+P actions  q quit"
		if m.resultEditor != nil {
			hint = "Tab field  Enter keep  Esc cancel  ↑↓ move"
		} else if m.busy != "" && m.work == searchWork {
			hint = "Esc cancel search  Ctrl+P actions  q quit"
		} else if m.resultFilter.active() {
			hint = "/ search  s sort  f filter  Esc clear  Enter add  Ctrl+P actions"
		}
	case serverPage:
		hint = "Enter details  r refresh  Ctrl+P actions  q quit"
	default:
		hint = "/ filter  o sort  Enter details  Space mark  a add  Ctrl+P actions  q quit"
	}
	return []string{statusLine, viewMutedStyle.Render(hint)}
}

func (m *model) viewStatus() string {
	if m.busy != "" {
		return "BUSY " + viewSafe(m.busy)
	}
	if m.failed {
		return "ERROR"
	}
	return "Ready"
}

func (m *model) viewPanel(title string, body []string, width, height int) []string {
	width = max(4, width)
	height = max(2, height)
	inner := width - 2
	titleText := " " + viewSafe(title) + " "
	if lipgloss.Width(titleText) > inner {
		titleText = " "
	}
	top := "┌" + titleText + strings.Repeat("─", max(0, inner-lipgloss.Width(titleText))) + "┐"
	lines := []string{top}
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		lines = append(lines, "│"+viewColumn(line, inner)+"│")
	}
	lines = append(lines, "└"+strings.Repeat("─", inner)+"┘")
	return viewFitLines(lines, width, height)
}

func viewColumns(left, right []string, leftWidth, rightWidth, height int) []string {
	lines := make([]string, 0, height)
	for i := 0; i < height; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		lines = append(lines, viewFit(l, leftWidth)+" "+viewFit(r, rightWidth))
	}
	return lines
}

func viewHasDetail(width int) bool {
	return width >= 112
}

func viewDetailWidth(width int) int {
	return max(30, width/3)
}

func viewQueueColumns(width int, showSpeed bool) (name, progress, state, speed int) {
	progress, state, speed = 16, 13, 20
	fixed := 2 + progress + state + 3 // marker and three separators
	if showSpeed {
		fixed += speed + 1 // the additional separator before speeds
	}
	name = max(1, width-fixed)
	if !showSpeed {
		speed = 0
	}
	return
}

func viewSearchColumns(width int) (name, id, size, seed int) {
	id, size, seed = 12, 10, 6
	fixed := 2 + id + size + seed + 16 + 5 // marker, source, and separators
	name = max(1, width-fixed)
	return
}

func viewWindowStart(index, count, visible int) int {
	if count <= 0 || visible <= 0 {
		return 0
	}
	index = min(max(0, index), count-1)
	start := index - visible + 1
	if start < 0 {
		start = 0
	}
	return start
}

func viewKeyValue(label, value string, width int) string {
	label = viewSafe(label)
	value = viewSafe(value)
	return viewTitleStyle.Render(label+":") + " " + value
}

func viewProgress(progress float64, width int) string {
	if width <= 7 {
		return viewPercent(progress)
	}
	barWidth := width - 7 // two brackets, a separator, and a four-cell percent
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	filled := int(progress * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := "[" + strings.Repeat("=", filled) + strings.Repeat(".", barWidth-filled) + "]"
	return viewAccentStyle.Render(bar + " " + viewPercent(progress))
}

func viewPercent(progress float64) string {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	return fmt.Sprintf("%3.0f%%", progress*100)
}

func viewBytes(value int64) string {
	if value < 0 {
		return "?"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	amount := float64(value)
	unit := 0
	for amount >= 1024 && unit < len(units)-1 {
		amount /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", amount, units[unit])
}

func viewSpeed(value int64) string {
	if value <= 0 {
		return "—"
	}
	return viewBytes(value) + "/s"
}

func viewColumn(value string, width int) string {
	width = max(0, width)
	value = viewFit(value, width)
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func viewRightAligned(width int, left, right string) string {
	left = viewFit(left, width)
	right = viewFit(right, width)
	if right == "" {
		return left
	}
	space := width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 1 {
		return viewFit(left+" "+right, width)
	}
	return left + strings.Repeat(" ", space) + right
}

func viewRule(width int) string {
	return strings.Repeat("─", max(0, width))
}

func viewFit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) > width {
		return ansi.TruncateWc(value, width, "…")
	}
	return value
}

func viewFitLines(lines []string, width, height int) []string {
	if height < 1 {
		return nil
	}
	result := make([]string, 0, height)
	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		line = viewFit(line, width)
		if padding := width - lipgloss.Width(line); padding > 0 {
			line = line + strings.Repeat(" ", padding)
		}
		result = append(result, line)
	}
	return result
}

func viewFillScreen(content string, width, height int) string {
	return strings.Join(viewFitLines(strings.Split(content, "\n"), width, height), "\n")
}

// viewSafe converts all terminal control characters, including C1 controls,
// to spaces. ANSI sequences are stripped first because a server-controlled
// torrent name should never be able to alter the terminal or move the cursor.
func viewSafe(value string) string {
	value = ansi.Strip(value)
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
