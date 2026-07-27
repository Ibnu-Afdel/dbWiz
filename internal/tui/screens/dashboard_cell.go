package screens

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// currentGrid returns the columns and rows the results pane is currently showing
// (building the describe grid on the fly), so the cell cursor and the renderer
// agree on one shape.
func (s dashboardScreen) currentGrid() ([]string, [][]any) {
	switch s.results {
	case resultsRows:
		return s.preview.Columns, s.preview.Rows
	case resultsQuery:
		return s.queryResult.Columns, s.queryResult.Rows
	case resultsDescribe:
		return describeGrid(s.columns)
	}
	return nil, nil
}

// describeGrid turns a column list into the name/type/null/key grid the describe
// view (and its cell cursor) share.
func describeGrid(cols []db.Column) ([]string, [][]any) {
	rows := make([][]any, len(cols))
	for i, c := range cols {
		nullable := "no"
		if c.Nullable {
			nullable = "yes"
		}
		rows[i] = []any{c.Name, c.Type, nullable, c.Key}
	}
	return []string{"Column", "Type", "Null", "Key"}, rows
}

// moveCell moves the results cell cursor by (dr, dc), clamped to the grid, then
// scrolls so the selected cell stays visible.
func (s *dashboardScreen) moveCell(dr, dc int) {
	cols, rows := s.currentGrid()
	if len(cols) == 0 || len(rows) == 0 {
		return
	}
	s.cellRow = clamp(s.cellRow+dr, 0, len(rows)-1)
	s.cellCol = clamp(s.cellCol+dc, 0, len(cols)-1)
	s.ensureCellVisible(cols, rows)
}

// ensureCellVisible nudges the row and column scroll offsets so the selected cell
// falls inside the visible window of the results pane.
func (s *dashboardScreen) ensureCellVisible(cols []string, rows [][]any) {
	d := s.dims()

	// Vertical: the grid spends one line on the header and one on the footer.
	visRows := max(d.resultsInner-3, 1)
	if s.cellRow < s.resultOffset {
		s.resultOffset = s.cellRow
	} else if s.cellRow >= s.resultOffset+visRows {
		s.resultOffset = s.cellRow - visRows + 1
	}

	// Horizontal: pull the window left if the cell is behind it, else advance the
	// offset until the selected (whole) column fits.
	innerW := max(d.rightInner, 1)
	widths := columnWidths(cols, rows)
	if s.cellCol < s.colOffset {
		s.colOffset = s.cellCol
	}
	for s.colOffset < len(cols)-1 {
		if _, end := visibleColumns(widths, s.colOffset, innerW); s.cellCol < end {
			break
		}
		s.colOffset++
	}
}

// openCell opens the detail overlay for the selected results cell: its full,
// untruncated value in a soft-wrapped, scrollable viewport — so a long
// description or a JSON blob is readable in full, in place, instead of being cut
// off with an ellipsis in the grid.
func (s dashboardScreen) openCell() (dashboardScreen, tea.Cmd) {
	cols, rows := s.currentGrid()
	if len(cols) == 0 || s.cellRow >= len(rows) || s.cellCol >= len(cols) {
		return s, nil
	}
	row := rows[s.cellRow]

	var val any
	if s.cellCol < len(row) {
		val = row[s.cellCol]
	}
	content := cellText(val)
	if val == nil {
		content = "NULL"
	}
	if content == "" {
		content = "(empty)"
	}

	d := s.dims()
	w := clamp(s.width-8, 20, 100)
	h := clamp(d.panesH-4, 5, 24)
	vp := viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))
	vp.SoftWrap = true
	vp.SetContent(content)

	s.cellVP = vp
	s.cellColName = cols[s.cellCol]
	s.mode = modeCell
	return s, nil
}

// updateCell handles keys while the cell-detail overlay is open: esc closes it,
// everything else scrolls the viewport (↑/↓, PgUp/PgDn, etc.).
func (s dashboardScreen) updateCell(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	if msg.String() == "esc" {
		s.mode = modeBrowse
		return s, nil
	}
	var cmd tea.Cmd
	s.cellVP, cmd = s.cellVP.Update(msg)
	return s, cmd
}

// renderCellOverlay draws the centered cell-detail box: the column name, the full
// value in the scrollable viewport, and a scroll/close hint.
func (s dashboardScreen) renderCellOverlay(width, height int) string {
	title := styles.PaneTitleFocused.Render(s.cellColName)
	hint := styles.Hint.Render("↑/↓ scroll · esc close")
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", s.cellVP.View(), "", hint)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, styles.OverlayBox.Render(body))
}
