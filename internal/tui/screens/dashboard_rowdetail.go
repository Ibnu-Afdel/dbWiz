package screens

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// openRowDetail opens the detail overlay for the selected results row: every
// column, name and full untruncated value, listed vertically in a scrollable
// viewport (v5 1.2). It's the answer to a wide table — the grid caps a column at
// maxCellWidth and hides the rest behind horizontal scroll, so reading a dozen
// columns of one row means moving the cell cursor a dozen times; this shows them
// all at once, the way openCell already does for a single cell.
func (s dashboardScreen) openRowDetail() (dashboardScreen, tea.Cmd) {
	if s.focus != focusResults {
		return s, nil
	}
	cols, rows := s.currentGrid()
	if len(cols) == 0 || s.cellRow >= len(rows) {
		return s, nil
	}
	row := rows[s.cellRow]

	d := s.dims()
	w := clamp(s.width-8, 20, 100)
	h := clamp(d.panesH-4, 5, 24)
	vp := viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))
	vp.SoftWrap = true
	vp.SetContent(renderRowFields(cols, row))

	s.rowVP = vp
	s.rowDetailTitle = s.rowDetailHeading(len(rows))
	s.mode = modeRowDetail
	return s, nil
}

// rowDetailHeading names the overlay: the table being browsed (or "query result"
// for an ad-hoc statement) plus the row's position, so paging through several
// rows in a row still shows where you are.
func (s dashboardScreen) rowDetailHeading(total int) string {
	name := s.resultsTable
	if name == "" {
		name = "query result"
	}
	return fmt.Sprintf("%s — row %d of %d", name, s.cellRow+1, total)
}

// renderRowFields lays out one row as aligned "column: value" lines — the column
// names right-padded to the widest one, NULL and empty strings called out
// distinctly (matching the grid's own convention) instead of rendering as blank.
func renderRowFields(cols []string, row []any) string {
	nameW := 0
	for _, c := range cols {
		nameW = max(nameW, lipgloss.Width(c))
	}
	lines := make([]string, len(cols))
	for i, c := range cols {
		var raw any
		if i < len(row) {
			raw = row[i]
		}
		label := styles.Hint.Render(padRight(c, nameW) + "  ")
		var val string
		switch {
		case raw == nil:
			val = styles.NullText.Render("NULL")
		case cellText(raw) == "":
			val = styles.Hint.Render("(empty)")
		default:
			val = styles.Item.Render(cellText(raw))
		}
		lines[i] = label + val
	}
	return strings.Join(lines, "\n")
}

// updateRowDetail handles keys while the overlay is open: esc closes it,
// everything else scrolls the viewport (↑/↓, PgUp/PgDn, etc.) — the same split as
// the single-cell overlay.
func (s dashboardScreen) updateRowDetail(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	if msg.String() == "esc" {
		s.mode = modeBrowse
		return s, nil
	}
	var cmd tea.Cmd
	s.rowVP, cmd = s.rowVP.Update(msg)
	return s, cmd
}

// renderRowDetailOverlay draws the centered row-detail box: the heading, the
// column/value list in the scrollable viewport, and a scroll/close hint.
func (s dashboardScreen) renderRowDetailOverlay(width, height int) string {
	title := styles.PaneTitleFocused.Render(s.rowDetailTitle)
	hint := styles.Hint.Render("↑/↓ scroll · esc close")
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", s.rowVP.View(), "", hint)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, styles.OverlayBox.Render(body))
}
