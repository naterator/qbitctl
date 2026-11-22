//go:build !plan9

package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/internal/searchresults"
	"github.com/naterator/qbitctl/pkg/client"
)

var (
	viewCanvasColor = lipgloss.Color("#1D2531")
	viewPanelColor  = lipgloss.Color("#334155")
	viewFieldColor  = lipgloss.Color("#242F3F")
	viewTextStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E9EEF5"))
	viewTitleStyle  = viewTextStyle.Bold(true)
	viewAccentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#85D7C8"))
	viewKeyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#9AC8FF")).Bold(true)
	viewMutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#AFBCCD"))
	viewRuleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#536478"))
	viewSelectedSty = lipgloss.NewStyle().Foreground(lipgloss.Color("#102A2D")).
			Background(lipgloss.Color("#92DACD")).Bold(true)
	viewErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAEA9")).Bold(true)
	viewWarnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD18E"))
)

func viewInputStyle(background color.Color) textinput.Styles {
	text := viewTextStyle.Background(background)
	secondary := viewMutedStyle.Background(background)
	return textinput.Styles{
		Focused: textinput.StyleState{Text: text, Placeholder: secondary, Suggestion: secondary},
		Blurred: textinput.StyleState{Text: text, Placeholder: secondary, Suggestion: secondary},
		Cursor:  textinput.CursorStyle{Color: viewAccentStyle.GetForeground(), Shape: tea.CursorBlock},
	}
}

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

	content := m.viewScreen(width, height)
	if m.dialog != nil && m.dialog.kind != dialogHelp {
		panel := m.dialogView(width, height)
		backdrop := lipgloss.NewLayer(viewSurface(ansi.Strip(content), width, height,
			lipgloss.Color("#151C26"), lipgloss.Color("#718195")))
		overlay := lipgloss.NewLayer(panel).
			X(max(0, (width-lipgloss.Width(panel))/2)).
			Y(max(0, (height-lipgloss.Height(panel))/2)).Z(1)
		content = lipgloss.NewCompositor(backdrop, overlay).Render()
	} else {
		content = viewSurface(content, width, height, viewCanvasColor, viewTextStyle.GetForeground())
	}
	content = applyTheme(content, width, height, m.activeThemeID())

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
	// The panel title and table headings each take one row.
	rows := height - viewHeaderLines - viewFooterLines - 2
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
	inner := viewContentWidth(width)
	header := m.viewHeader(inner)
	footer := m.viewFooter(inner)
	if m.dialog != nil && m.dialog.kind == dialogHelp {
		footer = []string{viewRule(inner), viewKeys(inner, "esc", "back", "↑/↓", "scroll", "pgup/pgdn", "page")}
	}
	bodyHeight := max(1, height-len(header)-len(footer))
	var body []string
	if m.dialog != nil && m.dialog.kind == dialogHelp {
		body = m.helpView(inner, bodyHeight)
	} else {
		body = m.viewBody(inner, bodyHeight)
	}

	lines := make([]string, 0, height)
	lines = append(lines, header...)
	lines = append(lines, viewFitLines(body, inner, bodyHeight)...)
	lines = append(lines, footer...)
	lines = viewFitLines(lines, inner, height)
	if inner < width {
		for i := range lines {
			lines[i] = "  " + lines[i] + "  "
		}
	}
	return strings.Join(lines, "\n")
}

func viewContentWidth(width int) int {
	if width >= 40 {
		return width - 4
	}
	return width
}

func (m *model) viewHeader(width int) []string {
	brand := viewAccentStyle.Render("⇅") + " " + viewTitleStyle.Render("qbitctl")
	first := brand
	if m.dialog == nil || m.dialog.kind != dialogHelp {
		first = viewRightAligned(width, brand, viewKeys(max(0, width-lipgloss.Width(brand)-2), "?", "shortcuts", "ctrl+p", "commands"))
	}
	tabs := []string{
		m.viewTab("1 Queue", queuePage),
		m.viewTab("2 Search", searchPage),
		m.viewTab("3 Server", serverPage),
		m.viewTab("4 Config", configPage),
	}
	second := strings.Join(tabs, "   ")
	if m.page == queuePage && strings.TrimSpace(m.filter) != "" && lipgloss.Width(second)+3 < width {
		second += "   " + viewMutedStyle.Render(viewFit("filter: /"+viewSafe(m.filter), width-lipgloss.Width(second)-3))
	}
	return []string{first, second, viewRule(width)}
}

func (m *model) viewTab(label string, p page) string {
	if m.page == p {
		return viewTitleStyle.Background(viewPanelColor).Render("▸ " + label + " ")
	}
	return viewMutedStyle.Render("  " + label + " ")
}

func (m *model) viewBody(width, height int) []string {
	switch m.page {
	case searchPage:
		return m.viewSearch(width, height)
	case serverPage:
		return m.viewServer(width, height)
	case configPage:
		return m.viewConfig(width, height)
	default:
		return m.viewQueue(width, height)
	}
}

func (m *model) viewQueue(width, height int) []string {
	title := fmt.Sprintf("Transfers  ·  %d", len(m.queue))
	if m.queueSort != queueActiveFirst {
		title += " · " + m.queueSort.label()
	}
	if len(m.marked) > 0 {
		title += fmt.Sprintf(" · %d marked", len(m.marked))
	}
	if viewHasDetail(width) {
		rightWidth := viewDetailWidth(width)
		leftWidth := max(1, width-rightWidth-1)
		left := viewPanel(title, m.viewQueueList(leftWidth-2, height-1), leftWidth, height)
		right := viewPanel("Selected torrent", m.viewTorrentDetail(rightWidth-2), rightWidth, height)
		return viewColumns(left, right, leftWidth, rightWidth, height)
	}
	return viewPanel(title, m.viewQueueList(width-2, height-1), width, height)
}

func (m *model) viewQueueList(width, height int) []string {
	width = max(1, width)
	rows := []string{m.viewQueueHeader(width)}
	if len(m.queue) == 0 {
		if m.busy != "" {
			message := "Waiting for queue data…"
			if m.work == connectWork {
				message = "Connecting to " + viewSafe(m.connectTarget) + "…"
			}
			rows = append(rows, viewMutedStyle.Render(message))
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
	name, progress, state, speed := viewQueueColumns(width)
	parts := []string{
		viewColumn("", 2),
		viewColumn("Name", name),
		viewColumn("Done", progress),
	}
	if state > 0 {
		parts = append(parts, viewColumn("State", state))
	}
	if speed > 0 {
		parts = append(parts, viewColumn("Down / Up", speed))
	}
	return viewMutedStyle.Render(strings.Join(parts, " "))
}

func (m *model) viewQueueRow(t client.TorrentInfo, selected bool, width int) string {
	name, progress, state, speed := viewQueueColumns(width)
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
	progressText := viewProgress(t.Progress, progress)
	stateText := viewSafe(t.State)
	if !selected {
		progressText = viewAccentStyle.Render(progressText)
		stateText = viewTorrentState(t.State)
	}
	parts := []string{
		viewColumn(marker, 2),
		viewColumn(viewSafe(t.Name), name),
		viewColumn(progressText, progress),
	}
	if state > 0 {
		parts = append(parts, viewColumn(stateText, state))
	}
	if speed > 0 {
		parts = append(parts, viewColumn(viewSpeed(t.DLSpeed)+" / "+viewSpeed(t.UpSpeed), speed))
	}
	line := viewFit(strings.Join(parts, " "), width)
	if selected {
		return viewSelectedSty.Width(width).Render(viewFit(line, width))
	}
	return line
}

func viewTorrentState(state string) string {
	style := viewMutedStyle
	switch state {
	case "downloading", "forcedDL":
		style = viewKeyStyle.Bold(false)
	case "uploading", "forcedUP", "stalledUP":
		style = viewAccentStyle
	case "error", "missingFiles":
		style = viewErrorStyle
	case "checkingDL", "checkingUP", "checkingResumeData", "stalledDL", "metaDL", "forcedMetaDL", "moving":
		style = viewWarnStyle
	}
	return style.Render(viewSafe(state))
}

func (m *model) viewTorrentDetail(width int) []string {
	t, ok := m.focusedTorrent()
	if !ok {
		return []string{"Select a torrent to inspect it.", "", "Enter opens details."}
	}
	return []string{
		viewTitleStyle.Render(viewFit(viewSafe(t.Name), width)),
		viewTorrentState(t.State) + viewMutedStyle.Render("  ·  "+viewBytes(t.TotalSize)),
		"",
		viewAccentStyle.Render(viewProgress(t.Progress, min(24, width))),
		"",
		viewKeyValue("Down", viewSpeed(t.DLSpeed)),
		viewKeyValue("Up", viewSpeed(t.UpSpeed)),
		viewKeyValue("Ratio", fmt.Sprintf("%.2f", t.Ratio)),
		viewKeyValue("Tracker", viewSafe(t.Tracker)),
		viewKeyValue("Path", viewSafe(t.ContentPath)),
	}
}

func (m *model) viewSearch(width, height int) []string {
	heading := viewPanelRows([]string{m.viewSearchHeading(width - 2)}, width, 1)
	bodyHeight := max(0, height-1)
	if viewHasDetail(width) {
		rightWidth := viewDetailWidth(width)
		leftWidth := max(1, width-rightWidth-1)
		left := viewPanelRows(m.viewSearchList(leftWidth-2, bodyHeight), leftWidth, bodyHeight)
		right := viewPanel("Selected result", m.viewResultDetail(rightWidth-2), rightWidth, bodyHeight)
		return append(heading, viewColumns(left, right, leftWidth, rightWidth, bodyHeight)...)
	}
	return append(heading, viewPanelRows(m.viewSearchList(width-2, bodyHeight), width, bodyHeight)...)
}

func (m *model) viewSearchHeading(width int) string {
	title := "Search results"
	if query := strings.TrimSpace(m.query); query != "" {
		title = "Query: " + viewSafe(query)
		if width >= 60 {
			title = "Search results   " + title
		}
	}
	const gap = 3
	minTitleWidth := min(lipgloss.Width(title), width/2)
	provider := viewFit("Provider: "+m.searchPluginLabel(m.searchPlugin), width-minTitleWidth-gap)
	title = viewFit(title, width-lipgloss.Width(provider)-gap)
	return viewRightAligned(width, viewTitleStyle.Render(title), viewAccentStyle.Render(provider))
}

func (m *model) viewSearchList(width, height int) []string {
	width = max(1, width)
	rows := []string{m.viewResultControls()}
	if m.resultEditor != nil {
		rows = append(rows, m.viewResultFilterEditor(width)...)
	}
	rows = append(rows, m.viewSearchHeader(width))
	if len(m.results) == 0 {
		message := "Press / to search " + m.searchPluginLabel(m.searchPlugin) + "."
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
	width := viewContentWidth(m.width)
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
	if m.resultEditor != nil && m.resultEditor.problem != "" {
		return viewErrorStyle.Render(viewSafe(m.resultEditor.problem))
	}
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
		viewSurface(viewFit(viewAccentStyle.Render(prefix)+input, width), width, 1,
			viewFieldColor, viewTextStyle.GetForeground()),
	}
}

func (m *model) viewSearchHeader(width int) string {
	return viewMutedStyle.Render(viewSearchLine(width, "", "Title", "ID", "Size", "Seeds", "Source"))
}

func (m *model) viewSearchRow(r client.SearchResult, selected bool, width int) string {
	id := "—"
	if r.ID > 0 {
		id = fmt.Sprintf("%d", r.ID)
	}
	marker := ""
	if selected {
		marker = "›"
	}
	line := viewSearchLine(width, marker, viewSafe(r.Name), id, viewBytes(r.Size), fmt.Sprintf("%d", r.Seeders), viewSafe(r.Source))
	if selected {
		return viewSelectedSty.Width(width).Render(line)
	}
	return line
}

func viewSearchLine(width int, marker, title, id, size, seeds, source string) string {
	columns := []string{viewColumn(size, 9), viewColumn(seeds, 5)}
	if width >= 58 {
		columns = append(columns, viewColumn(id, 10))
	}
	if width >= 68 {
		columns = append(columns, viewColumn(source, 14))
	}
	metrics := strings.Join(columns, " ")
	nameWidth := max(1, width-4-lipgloss.Width(metrics))
	return viewFit(viewColumn(marker, 2)+" "+viewColumn(title, nameWidth)+" "+metrics, width)
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
		viewTitleStyle.Render(viewFit(viewSafe(r.Name), width)),
		viewMutedStyle.Render(viewBytes(r.Size) + "  ·  " + viewSafe(r.Source)),
		"",
		viewKeyValue("Seeders", fmt.Sprintf("%d", r.Seeders)),
		viewKeyValue("Leechers", fmt.Sprintf("%d", r.Leechers)),
		viewKeyValue("Result ID", id),
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
		labelWidth := min(24, max(1, width/2-2))
		for _, row := range m.serverRows {
			label := viewMutedStyle.Render(viewColumn(viewSafe(row.label), labelWidth))
			value := viewSafe(row.value)
			if row.label == "Connection" && strings.EqualFold(value, "connected") {
				value = viewAccentStyle.Render("● " + value)
			}
			rows = append(rows, label+" "+value)
		}
	}
	return viewPanel("Server & transfers", rows, width, height)
}

func (m *model) viewConfig(width, height int) []string {
	rowWidth := max(1, width-2)
	rows := []string{
		viewAccentStyle.Render("Connection"),
		viewConfigRow("Server address", m.setup.url, rowWidth),
		viewConfigRow("Username", m.setup.user, rowWidth),
		viewConfigRow("Password", configPasswordStatus(m.setup.hasPassword), rowWidth),
		"",
		viewAccentStyle.Render("Startup"),
		viewConfigRow("TUI by default", configToggleStatus(m.setup.tuiDefault), rowWidth),
		viewConfigRow("Color scheme", configThemeStatus(m), rowWidth),
		"",
		viewMutedStyle.Render("Config file"),
		viewMutedStyle.Render(viewSafe(m.configPath)),
	}
	return viewPanel("Configuration", rows, width, height)
}

func viewConfigRow(label, value string, width int) string {
	labelWidth := min(22, max(14, width/3))
	label = viewSafe(strings.TrimSpace(label))
	value = strings.TrimSpace(viewSafe(value))
	if value == "" {
		value = "Not configured"
	}
	labelCell := viewMutedStyle.Background(viewFieldColor).Render(" " + viewColumn(label, labelWidth) + " ")
	valueCell := viewTextStyle.Render(" " + viewFit(value, max(1, width-labelWidth-5)))
	line := labelCell + viewRuleStyle.Render("│") + valueCell
	return viewSurface(viewFit(line, width), width, 1, viewPanelColor, viewTextStyle.GetForeground())
}

func configPasswordStatus(saved bool) string {
	if saved {
		return "••••••••  saved (hidden)"
	}
	return "Not configured"
}

func configToggleStatus(enabled bool) string {
	if enabled {
		return "Enabled"
	}
	return "Disabled"
}

func configThemeStatus(m *model) string {
	label := m.themeLabel()
	if strings.TrimSpace(label) == "" {
		return "Default"
	}
	return label
}

func (m *model) viewFooter(width int) []string {
	status := viewAccentStyle.Render("● Ready")
	switch {
	case m.busy != "":
		status = viewWarnStyle.Render("◌ " + viewSafe(m.busy))
	case m.failed:
		status = viewErrorStyle.Render("● Error")
	case m.notice != "":
		status = viewAccentStyle.Render("● " + viewSafe(m.notice))
	}
	if m.notice != "" && (m.busy != "" || m.failed) {
		status += viewMutedStyle.Render("  ·  " + viewSafe(m.notice))
	}
	var keys []string
	switch m.page {
	case searchPage:
		keys = []string{"/", "search", "p", "Providers", "s", "sort", "f", "filter", "enter", "add", "q", "quit"}
		if m.resultEditor != nil {
			keys = []string{"tab", "field", "enter", "apply", "esc", "cancel", "↑/↓", "move"}
		} else if m.busy != "" && m.work == searchWork {
			keys = []string{"p", "Providers", "esc", "cancel search", "q", "quit"}
		} else if m.resultFilter.active() {
			keys = []string{"p", "Providers", "/", "search", "s", "sort", "f", "filter", "esc", "clear", "enter", "add", "q", "quit"}
		}
	case serverPage:
		keys = []string{"enter", "details", "r", "refresh", "q", "quit"}
	case configPage:
		keys = []string{"enter", "edit", "d", "toggle default", "c", "colors", "q", "quit"}
	default:
		keys = []string{"/", "filter", "o", "sort", "enter", "details", "space", "mark", "a", "add", "q", "quit"}
	}
	return []string{viewFit(status, width), viewKeys(width, keys...)}
}

func viewKeys(width int, pairs ...string) string {
	var hints []string
	remaining := width
	for i := 0; i+1 < len(pairs); i += 2 {
		key, label := pairs[i], pairs[i+1]
		span := lipgloss.Width(key + " " + label)
		if span > remaining {
			break
		}
		hints = append(hints, viewKeyStyle.Render(key)+" "+viewMutedStyle.Render(label))
		remaining -= span + 3
	}
	return strings.Join(hints, "   ")
}

func viewPanel(title string, body []string, width, height int) []string {
	lines := []string{viewTitleStyle.Render(viewFit(title, width-2))}
	return viewPanelRows(append(lines, body...), width, height)
}

func viewPanelRows(lines []string, width, height int) []string {
	for i := range lines {
		lines[i] = " " + viewColumn(lines[i], width-2) + " "
	}
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
		lines = append(lines, viewColumn(l, leftWidth)+viewRuleStyle.Render("│")+viewFit(r, rightWidth))
	}
	return lines
}

func viewHasDetail(width int) bool {
	return width >= 102
}

func viewDetailWidth(width int) int {
	return max(30, width/3)
}

func viewQueueColumns(width int) (name, progress, state, speed int) {
	progress = 4
	if width >= 62 {
		progress = 15
	}
	fixed := 2 + progress + 2 // marker and separators
	if width >= 34 {
		state = 13
		fixed += state + 1
	}
	if width >= 85 {
		speed = 23
		fixed += speed + 1
	}
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

func viewKeyValue(label, value string) string {
	return viewMutedStyle.Render(viewColumn(label, 10)) + value
}

func viewProgress(progress float64, width int) string {
	if width <= 5 {
		return viewPercent(progress)
	}
	barWidth := width - 5 // one separator and a four-cell percent
	progress = boundedProgress(progress)
	filled := int(progress * float64(barWidth))
	bar := strings.Repeat("━", filled) + strings.Repeat("┄", barWidth-filled)
	return bar + " " + viewPercent(progress)
}

func viewPercent(progress float64) string {
	return fmt.Sprintf("%3.0f%%", boundedProgress(progress)*100)
}

func boundedProgress(progress float64) float64 {
	if math.IsNaN(progress) {
		return 0
	}
	return min(1, max(0, progress))
}

func viewBytes(value int64) string {
	return searchresults.FormatSize(value)
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
	return viewRuleStyle.Render(strings.Repeat("─", max(0, width)))
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

// Apply the surface colors to individual cells. An inline style can reset SGR,
// so wrapping the styled string in a background style can leave unpainted gaps.
func viewSurface(content string, width, height int, background, foreground color.Color) string {
	canvas := lipgloss.NewCanvas(width, height).Compose(lipgloss.NewLayer(content))
	for y := range height {
		for x := range width {
			cell := canvas.CellAt(x, y)
			if cell.Width == 0 {
				continue // continuation of a wide glyph
			}
			if cell.Style.Bg == nil {
				cell.Style.Bg = background
			}
			if cell.Style.Fg == nil {
				cell.Style.Fg = foreground
			}
		}
	}
	return canvas.Render()
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
