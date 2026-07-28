package screens

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// maxCellWidth caps a results column so one wide value can't push the rest of
// the row off-screen.
const maxCellWidth = 28

// paneDims holds the derived inner sizes of the dashboard's panes for one frame.
// Both View and the editor-resize path in Update read it, so the textarea's
// wrapping width always matches what's actually drawn.
type paneDims struct {
	panesH                    int // body height below the status bar
	leftInner, rightInner     int // navigator / right-column content widths
	editorInner, resultsInner int // right-column pane content heights
	editorTextH               int // rows available to the textarea itself
}

// dims computes the pane layout: a left navigator (~a third, clamped) and a
// right column stacking the Query editor over the Results grid. The editor grows
// to ~half the height while it holds focus or a query is running, and stays
// short otherwise so the results have room.
func (s dashboardScreen) dims() paneDims {
	panesH := s.height - 1
	leftTotal := clamp(s.width/3, 26, 42)
	rightTotal := s.width - leftTotal

	editorTotal := min(6, panesH-4)
	if s.focus == focusEditor || s.querying {
		editorTotal = clamp(panesH/2, 6, max(panesH-6, 6))
	}
	resultsTotal := panesH - editorTotal

	d := paneDims{
		panesH:       panesH,
		leftInner:    max(leftTotal-4, 1),
		rightInner:   max(rightTotal-4, 1),
		editorInner:  editorTotal - 2,
		resultsInner: resultsTotal - 2,
	}
	// The editor pane spends one row on its title and one on the status line.
	d.editorTextH = max(d.editorInner-2, 1)
	return d
}

func (s dashboardScreen) View(width, height int) string {
	// The root hands the live size to Update via WindowSizeMsg, but View also
	// receives it each frame; prefer the frame's so a resize renders immediately.
	if width > 0 {
		s.width, s.height = width, height
	}
	if s.width < 40 || s.height < 8 {
		return styles.Screen.Render(styles.Subtitle.Render("Window too small — enlarge the terminal to browse."))
	}

	// An open admin overlay takes over the whole body.
	if s.mode != modeBrowse {
		return s.renderOverlay(s.width, s.height)
	}

	status := s.renderStatusBar(s.width)
	d := s.dims()
	if d.panesH < 4 {
		return status
	}

	left := s.renderNavigator(d.leftInner, d.panesH-2)
	editor := s.renderEditor(d.rightInner, d.editorInner)
	results := s.renderResults(d.rightInner, d.resultsInner)

	right := lipgloss.JoinVertical(lipgloss.Left, editor, results)
	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, status, panes)
}

// renderStatusBar shows the engine, the connection target, the current database,
// and a health pill. The pill flips to a caution colour when any pane is showing
// an error (e.g. the container was killed mid-browse).
func (s dashboardScreen) renderStatusBar(width int) string {
	parts := []string{styles.Title.Render(s.engine.Kind().String()), s.container.Name}
	if s.caps.MultipleDatabases && s.currentDB != "" {
		parts = append(parts, "db: "+s.currentDB)
	}
	left := styles.Subtitle.Render(strings.Join(parts, "  ·  "))

	// A transient result toast or "working…" indicator sits between the target
	// and the pill.
	switch {
	case s.working:
		left += "   " + s.spinner.View() + " " + styles.Hint.Render("working…")
	case s.notice != "":
		st := styles.SuccessText
		if s.noticeErr {
			st = styles.DangerText
		}
		left += "   " + st.Render(s.notice)
	case !s.hintDismissed:
		// One-time beginner hint until the first key: how to move around and find
		// the rest of the keys (Step 9.2).
		left += "   " + styles.Hint.Render("tab moves focus · e edit SQL · ? for help")
	}

	pill := styles.SuccessText.Render("● live")
	if s.dbErr != nil || s.tblErr != nil || s.resErr != nil || s.usersErr != nil {
		pill = styles.WarningText.Render("● connection error")
	}

	gap := max(width-lipgloss.Width(left)-lipgloss.Width(pill)-2, 1)
	return fitLine(" "+left+strings.Repeat(" ", gap)+pill+" ", width)
}

// navSection is one titled list in the navigator (databases, tables, users).
type navSection struct {
	title   string
	lines   []string
	cursor  int
	focused bool
	loading bool
	err     *db.DBError
}

// renderNavigator draws the left pane, stacking whichever sections the engine
// offers: databases (multi-DB engines) → tables → users (Capabilities.Users).
// Absent capabilities are omitted entirely — no greyed-out UI (Step 5.4 / 6.4).
func (s dashboardScreen) renderNavigator(innerW, innerH int) string {
	focused := s.focus == focusDatabases || s.focus == focusTables || s.focus == focusUsers

	var secs []navSection
	if s.caps.MultipleDatabases {
		secs = append(secs, navSection{"Databases", s.databaseLines(), s.dbCursor,
			s.focus == focusDatabases, s.dbLoading, s.dbErr})
	}
	secs = append(secs, navSection{"Tables", s.tableLines(), s.tblCursor,
		s.focus == focusTables, s.tblLoading, s.tblErr})
	if s.caps.Users {
		secs = append(secs, navSection{"Users", s.userLines(), s.userCursor,
			s.focus == focusUsers, s.usersLoading, s.usersErr})
	}

	// Divide the vertical space evenly across the sections, reserving a blank
	// spacer row between each.
	per := (innerH - (len(secs) - 1)) / len(secs)
	blocks := make([]string, 0, len(secs)*2)
	for i, sc := range secs {
		blocks = append(blocks, s.section(sc.title, sc.lines, sc.cursor, sc.focused, sc.loading, sc.err, per-1, innerW))
		if i < len(secs)-1 {
			blocks = append(blocks, "")
		}
	}
	return s.pane("Navigator", lipgloss.JoinVertical(lipgloss.Left, blocks...), focused, innerW, innerH)
}

// renderOverlay centers the active admin overlay (form/confirm/grant) in the
// body area.
func (s dashboardScreen) renderOverlay(width, height int) string {
	var body string
	switch s.mode {
	case modeForm:
		body = s.form.View(width)
	case modeConfirm:
		body = s.confirm.View(width)
	case modeGrant:
		body = s.grant.View(width)
	case modeHistory:
		body = s.historyList.View(width)
	case modeSaved:
		body = s.savedList.View(width)
	case modeExport:
		body = s.export.View(width)
	case modeComplete:
		body = s.completeList.View(width)
	case modeCell:
		return s.renderCellOverlay(width, height)
	case modeEditCell:
		body = s.cellEditView(width)
	case modeConfirmSQL:
		body = s.confirmSQLView(width)
	case modeInsertRow:
		body = s.insertRowView(width)
	case modeFilter:
		body = s.filterView(width)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, body)
}

// section renders a titled, scrollable list with its own loading spinner and
// inline error, so one pane failing never blanks the others.
func (s dashboardScreen) section(title string, items []string, cursor bool2int, listFocused, loading bool, err *db.DBError, rows, innerW int) string {
	head := styles.Hint.Render(title)
	switch {
	case loading:
		return lipgloss.JoinVertical(lipgloss.Left, head, s.spinner.View()+" "+styles.Hint.Render("loading…"))
	case err != nil:
		return lipgloss.JoinVertical(lipgloss.Left, head, s.paneError(err, innerW))
	case len(items) == 0:
		return lipgloss.JoinVertical(lipgloss.Left, head, styles.Hint.Render("(none)"))
	}
	list := renderList(items, int(cursor), listFocused, rows, innerW)
	return lipgloss.JoinVertical(lipgloss.Left, head, list)
}

// bool2int is a tiny alias so section can take the concrete cursor index; the
// name keeps the signature readable at the call site.
type bool2int = int

// databaseLines is the display list for the databases section, marking the
// active database with a filled bullet so it's distinct from the cursor.
func (s dashboardScreen) databaseLines() []string {
	out := make([]string, len(s.databases))
	for i, d := range s.databases {
		marker := "  "
		name := d.Name
		if d.Name == s.currentDB {
			marker = styles.SuccessText.Render("● ")
			name = styles.Selected.Render(name)
		}
		out[i] = marker + name
	}
	return out
}

// tableLines is the display list for the tables section, with an estimated row
// count where the engine could supply one.
func (s dashboardScreen) tableLines() []string {
	out := make([]string, len(s.tables))
	for i, t := range s.tables {
		label := t.Name
		if t.Rows >= 0 {
			label += styles.Hint.Render(fmt.Sprintf("  ~%d", t.Rows))
		}
		out[i] = label
	}
	return out
}

// userLines is the display list for the users section.
func (s dashboardScreen) userLines() []string {
	out := make([]string, len(s.users))
	for i, u := range s.users {
		out[i] = u.Name
	}
	return out
}

// renderEditor draws the Query pane: the multi-line SQL textarea over a status
// line that shows the run hint, a running query's elapsed time (Step 7.5), or an
// inline query error (Step 7.4). The textarea is sized to the pane so wrapping
// matches what the user edits.
func (s dashboardScreen) renderEditor(innerW, innerH int) string {
	s.editor.SetWidth(max(innerW, 1))
	s.editor.SetHeight(max(innerH-2, 1)) // reserve the title and status rows
	body := lipgloss.JoinVertical(lipgloss.Left, s.editor.View(), s.editorStatus(innerW))
	return s.pane("Query", body, s.focus == focusEditor, innerW, innerH)
}

// editorStatus is the one line under the textarea: a running/elapsed indicator
// with a cancel hint, an inline error, or the default key hints.
func (s dashboardScreen) editorStatus(innerW int) string {
	switch {
	case s.querying:
		el := time.Since(s.queryStart).Round(100 * time.Millisecond)
		line := s.spinner.View() + " " + styles.Hint.Render("running "+el.String()) +
			"  " + styles.WarningText.Render("[esc] cancel")
		if time.Since(s.queryStart) >= querySoftTimeout {
			line += "  " + styles.WarningText.Render("still running — wait or cancel")
		}
		return fitLine(line, innerW)
	case s.queryErr != nil:
		msg := s.queryErr.Title
		if s.queryErr.Detail != "" {
			msg += " — " + s.queryErr.Detail
		}
		return fitLine(styles.DangerText.Render("⚠ "+msg), innerW)
	case s.vim.enabled && s.focus == focusEditor:
		// Modal editor: show the current mode and its key hints (v2 2.4).
		tag, style, hint := "-- NORMAL --", styles.SuccessText, "i insert · hjkl move · dd/x edit · ^R/F5 run · esc leave"
		if s.vim.mode == editorInsert {
			tag, style, hint = "-- INSERT --", styles.WarningText, "esc normal · Ctrl+R / F5 run"
		}
		return fitLine(style.Render(tag)+"  "+styles.Hint.Render(hint), innerW)
	default:
		return fitLine(styles.Hint.Render("Ctrl+R / F5 run · ^p/^n history · esc leave"), innerW)
	}
}

// renderResults draws the bottom-right grid: a row preview, a table's columns,
// or a friendly idle/empty state. Load failures render here inline with [R]
// retry rather than routing to the full-screen error.
func (s dashboardScreen) renderResults(innerW, innerH int) string {
	title := "Results"
	switch {
	case s.results == resultsQuery:
		title = "Results — query"
	case s.resultsTable != "":
		verb := "preview"
		if s.results == resultsDescribe {
			verb = "columns"
		}
		title = fmt.Sprintf("Results — %s (%s)", s.resultsTable, verb)
	}

	var body string
	switch {
	case s.resLoading:
		body = s.spinner.View() + " " + styles.Hint.Render("loading…")
	case s.resErr != nil:
		body = s.paneError(s.resErr, innerW)
	case s.results == resultsQuery:
		body = s.renderQueryResult(innerW, innerH-1)
	case s.results == resultsDescribe:
		body = s.renderDescribe(innerW, innerH-1)
	case s.results == resultsRows:
		body = s.renderRows(innerW, innerH-1)
	default:
		body = styles.Hint.Render("Select a table and press enter to preview its rows.")
	}
	return s.pane(title, body, s.focus == focusResults, innerW, innerH)
}

// renderRows formats the preview as a header row over the cell grid, scrolled by
// resultOffset. An empty table is a friendly note, not a blank grid.
func (s dashboardScreen) renderRows(innerW, visibleRows int) string {
	r := s.preview
	if len(r.Columns) == 0 {
		return styles.Hint.Render("Query returned no columns.")
	}
	if len(r.Rows) == 0 {
		return styles.Hint.Render("This table is empty.")
	}

	note := fmt.Sprintf("%d rows (%s)", len(r.Rows), r.Duration.Round(time.Millisecond))
	return s.renderGrid(r.Columns, r.Rows, innerW, visibleRows, note)
}

// renderQueryResult formats a Query result in the shared results pane (Step
// 7.3): a row grid for statements that return rows, an "N rows affected" line
// for exec statements, and a friendly "0 rows" for an empty result set. Duration
// is always shown; NULL cells render distinctly via dataRow.
func (s dashboardScreen) renderQueryResult(innerW, visibleRows int) string {
	r := s.queryResult
	if len(r.Columns) == 0 {
		verb := s.queryVerb
		if verb == "" {
			verb = "OK"
		}
		return fitLine(styles.SuccessText.Render(fmt.Sprintf("%s — %d row(s) affected", verb, r.RowsAffected))+
			styles.Hint.Render(fmt.Sprintf("  ·  %s", r.Duration.Round(time.Millisecond))), innerW)
	}
	if len(r.Rows) == 0 {
		return fitLine(styles.Hint.Render(fmt.Sprintf("0 rows  ·  %s", r.Duration.Round(time.Millisecond))), innerW)
	}

	note := fmt.Sprintf("%d rows (%s)", len(r.Rows), r.Duration.Round(time.Millisecond))
	return s.renderGrid(r.Columns, r.Rows, innerW, visibleRows, note)
}

// renderDescribe formats DescribeTable output as a name/type/null/key grid.
func (s dashboardScreen) renderDescribe(innerW, visibleRows int) string {
	if len(s.columns) == 0 {
		return styles.Hint.Render("No columns.")
	}
	cols, rows := describeGrid(s.columns)
	return s.renderGrid(cols, rows, innerW, visibleRows, fmt.Sprintf("%d columns", len(rows)))
}

// paneError renders a typed failure inside a pane: the plain-language detail plus
// the [R] retry hint, so a mid-browse failure stays local to the pane.
func (s dashboardScreen) paneError(e *db.DBError, innerW int) string {
	detail := e.Detail
	if detail == "" {
		detail = e.Title
	}
	return fitBlock([]string{
		styles.DangerText.Render("⚠ " + e.Title),
		styles.Hint.Render(detail),
		styles.Hint.Render("[R] retry"),
	}, innerW)
}

// pane wraps a title and body in the shared bordered box, brightening the border
// and title when the pane holds focus so the active pane is obvious.
func (s dashboardScreen) pane(title, body string, focused bool, innerW, innerH int) string {
	style, tstyle := styles.Pane, styles.PaneTitle
	if focused {
		style, tstyle = styles.PaneFocused, styles.PaneTitleFocused
	}
	head := tstyle.Render(fitLine(title, innerW))
	content := clampHeight(lipgloss.JoinVertical(lipgloss.Left, head, body), innerH)
	return style.Width(innerW).Height(innerH).Render(content)
}

// --- rendering helpers ---

// renderList draws a scrollable list windowed around the cursor. Only the
// focused list styles its cursor row, so an unfocused pane reads as inactive.
func renderList(items []string, cursor int, focused bool, rows, innerW int) string {
	if rows < 1 {
		rows = 1
	}
	start := 0
	if cursor >= rows {
		start = cursor - rows + 1
	}
	end := min(start+rows, len(items))

	var lines []string
	for i := start; i < end; i++ {
		marker := "  "
		line := items[i]
		if i == cursor && focused {
			marker = styles.Selected.Render("▸ ")
			line = styles.Selected.Render(stripStyle(items[i]))
		}
		lines = append(lines, fitLine(marker+line, innerW))
	}
	return strings.Join(lines, "\n")
}

// renderGrid draws a scrollable table: only the whole columns that fit innerW
// starting at colOffset (never cut mid-column), the visible row window from
// resultOffset, and a footer showing the column window + row note. This is the
// one place preview/query/describe grids are laid out, so they scroll and align
// identically. note is appended to the footer (e.g. "200 rows (3ms)").
func (s dashboardScreen) renderGrid(cols []string, rows [][]any, innerW, visibleRows int, note string) string {
	widths := columnWidths(cols, rows)
	cStart, cEnd := visibleColumns(widths, clamp(s.colOffset, 0, max(len(cols)-1, 0)), innerW)
	subCols, subW := cols[cStart:cEnd], widths[cStart:cEnd]

	// Reserve one line for the header and one for the footer.
	dataRows := max(visibleRows-2, 1)
	rStart := clamp(s.resultOffset, 0, max(len(rows)-1, 0))
	rEnd := min(rStart+dataRows, len(rows))

	// The cell cursor is only highlighted while the results pane holds focus.
	selRow, selCol := -1, -1
	if s.focus == focusResults {
		selRow, selCol = s.cellRow, s.cellCol
	}

	lines := []string{headerRow(subCols, subW)}
	for r := rStart; r < rEnd; r++ {
		sel := -1
		if r == selRow {
			sel = selCol
		}
		lines = append(lines, gridDataRow(rows[r], cStart, cEnd, widths, sel))
	}
	lines = append(lines, gridFooter(cStart, cEnd, len(cols), note))
	return fitBlock(lines, innerW)
}

// gridDataRow renders the visible columns [cStart,cEnd) of a row, padding each
// cell to its column width before styling so alignment holds and the selected
// cell (selCol, or -1 for none) is highlighted across its whole width.
func gridDataRow(row []any, cStart, cEnd int, widths []int, selCol int) string {
	cells := make([]string, 0, cEnd-cStart)
	for ci := cStart; ci < cEnd; ci++ {
		var raw any
		if ci < len(row) {
			raw = row[ci]
		}
		text := "NULL"
		style := styles.NullText
		if raw != nil {
			text, style = trunc(cellText(raw), widths[ci]), styles.Item
		}
		padded := text + strings.Repeat(" ", max(widths[ci]-lipgloss.Width(text), 0))
		if ci == selCol {
			style = styles.TableSelected
		}
		cells = append(cells, style.Render(padded))
	}
	return strings.Join(cells, "  ")
}

// visibleColumns returns the [start,end) range of columns that fit in innerW
// from start, always including at least one column. Columns are shown whole or
// not at all, so nothing is cut mid-cell.
func visibleColumns(widths []int, start, innerW int) (int, int) {
	if len(widths) == 0 {
		return 0, 0
	}
	used, end := 0, start
	for end < len(widths) {
		w := widths[end]
		if end > start {
			w += 2 // the "  " column separator
		}
		if end > start && used+w > innerW {
			break
		}
		used += w
		end++
	}
	if end == start {
		end = start + 1
	}
	return start, end
}

// gridFooter renders the table footer: a column-window indicator with a scroll
// hint when columns are off-screen, plus the caller's row note.
func gridFooter(cStart, cEnd, total int, note string) string {
	var parts []string
	if cStart > 0 || cEnd < total {
		parts = append(parts, fmt.Sprintf("cols %d–%d of %d · ←/→ scroll", cStart+1, cEnd, total))
	}
	if note != "" {
		parts = append(parts, note)
	}
	return styles.Hint.Render(strings.Join(parts, "  ·  "))
}

// columnWidths sizes each column to the widest of its header and visible cells,
// capped at maxCellWidth.
func columnWidths(cols []string, rows [][]any) []int {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = min(len(c), maxCellWidth)
	}
	for _, row := range rows {
		for i := range cols {
			if i < len(row) {
				if w := len(cellText(row[i])); w > widths[i] {
					widths[i] = min(w, maxCellWidth)
				}
			}
		}
	}
	return widths
}

func headerRow(cols []string, widths []int) string {
	cells := make([]string, len(cols))
	for i, c := range cols {
		cells[i] = padRight(styles.TableHeader.Render(trunc(c, widths[i])), widths[i])
	}
	return strings.Join(cells, "  ")
}

// cellText renders a driver cell for display: []byte as its string, nil handled
// by the caller, everything else via fmt.
func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// fitBlock trims each line to innerW and joins them.
func fitBlock(lines []string, innerW int) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fitLine(l, innerW)
	}
	return strings.Join(out, "\n")
}

// fitLine truncates a single (possibly styled) line to a visible width.
func fitLine(s string, w int) string {
	if w < 1 {
		w = 1
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

// clampHeight keeps only the first n lines so a pane's content never overflows
// its border box.
func clampHeight(s string, n int) string {
	if n < 1 {
		n = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// trunc shortens plain text to n runes with an ellipsis when it doesn't fit.
func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// stripStyle removes the row-count/other embedded styling from a list item so
// the cursor's Selected style applies cleanly. It drops ANSI escapes.
func stripStyle(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}
