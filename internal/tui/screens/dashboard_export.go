package screens

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/export"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// exportFormat is one of the two portable formats the results pane can write out.
type exportFormat int

const (
	exportCSV exportFormat = iota
	exportJSON
)

func (f exportFormat) label() string {
	if f == exportJSON {
		return "JSON"
	}
	return "CSV"
}

func (f exportFormat) ext() string {
	if f == exportJSON {
		return "json"
	}
	return "csv"
}

// exportResult is what a key press did to the export chooser.
type exportResult int

const (
	exportPending  exportResult = iota // still choosing
	exportCanceled                     // esc
	exportChosen                       // enter — write the highlighted format
)

// exportModel is the little "Export results" chooser (v2 2.3): two options, CSV
// and JSON. It's deliberately not a bubbles/list — two fixed choices need only a
// cursor — and it carries the row count and destination directory so the box can
// tell the user exactly what will be written and where.
type exportModel struct {
	choice exportFormat
	rows   int
	dir    string
}

// newExportModel builds the chooser for a result of n rows destined for dir.
func newExportModel(n int, dir string) exportModel {
	return exportModel{choice: exportCSV, rows: n, dir: dir}
}

// update handles one key: esc cancels, ↑/↓ (and j/k) move between the two
// formats, enter confirms the highlighted one.
func (m exportModel) update(msg tea.KeyPressMsg) (exportModel, exportResult) {
	switch msg.String() {
	case "esc":
		return m, exportCanceled
	case "up", "k", "down", "j", "tab":
		if m.choice == exportCSV {
			m.choice = exportJSON
		} else {
			m.choice = exportCSV
		}
		return m, exportPending
	case "enter":
		return m, exportChosen
	}
	return m, exportPending
}

func (m exportModel) View(width int) string {
	lines := []string{
		styles.Title.Render("Export results"),
		"",
		styles.Hint.Render(fmt.Sprintf("%d rows → %s", m.rows, m.dir)),
		"",
	}
	for _, f := range []exportFormat{exportCSV, exportJSON} {
		marker, label := "  ", styles.Hint.Render(f.label())
		if f == m.choice {
			marker, label = styles.Selected.Render("▸ "), styles.Subtitle.Render(f.label())
		}
		lines = append(lines, marker+label)
	}
	lines = append(lines, "", styles.Hint.Render("↑/↓ choose · enter export · esc cancel"))
	return styles.Screen.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// openExport opens the format chooser over the current results grid. With nothing
// to export (an idle pane, an empty or exec-only result) it shows a notice
// instead, so the key always reads clearly.
func (s dashboardScreen) openExport() (dashboardScreen, tea.Cmd) {
	cols, rows := s.currentGrid()
	if len(cols) == 0 || len(rows) == 0 {
		s.notice, s.noticeErr = "Nothing to export — run a query or preview a table first.", false
		return s, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	s.export = newExportModel(len(rows), dir)
	s.mode = modeExport
	return s, nil
}

// exportChosenCmd writes the current grid to a timestamped file in dir and
// reports the path. It runs off the Update goroutine (protocol #4: file I/O never
// blocks Update) and returns a typed done/err message the dashboard turns into a
// toast.
func exportChosenCmd(f exportFormat, base, dir string, cols []string, rows [][]any) tea.Cmd {
	return func() tea.Msg {
		var (
			data []byte
			err  error
		)
		switch f {
		case exportJSON:
			data, err = export.JSON(cols, rows)
		default:
			data, err = export.CSV(cols, rows)
		}
		if err != nil {
			return exportErrMsg{err: err}
		}
		name := fmt.Sprintf("dbwiz-%s-%s.%s", base, time.Now().Format("20060102-150405"), f.ext())
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return exportErrMsg{err: err}
		}
		return exportDoneMsg{path: path, rows: len(rows), format: f}
	}
}

// exportDoneMsg / exportErrMsg carry the outcome of a file export back to Update.
type (
	exportDoneMsg struct {
		path   string
		rows   int
		format exportFormat
	}
	exportErrMsg struct{ err error }
)

// exportBase names the file after what's on screen: the previewed table, or a
// generic "query" for ad-hoc results. It's sanitised to a filename-safe slug so a
// schema-qualified or oddly-named table can't produce a broken path.
func (s dashboardScreen) exportBase() string {
	base := s.resultsTable
	if s.results == resultsQuery || base == "" {
		base = "query"
	}
	return slugify(base)
}

// slugify reduces a label to lowercase filename-safe characters, collapsing runs
// of anything else to a single dash.
func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "query"
	}
	return out
}

// --- clipboard (OSC 52, so a copy works over SSH too) ---

// copyCell yanks the selected cell's value to the system clipboard. It only acts
// in the results pane over a real grid; elsewhere it's a no-op so the bare 'y'
// key is harmless. NULL copies as an empty string (what the value actually is).
func (s dashboardScreen) copyCell() (dashboardScreen, tea.Cmd) {
	cols, rows := s.currentGrid()
	if s.focus != focusResults || len(cols) == 0 || s.cellRow >= len(rows) || s.cellCol >= len(cols) {
		return s, nil
	}
	row := rows[s.cellRow]
	var val any
	if s.cellCol < len(row) {
		val = row[s.cellCol]
	}
	s.notice, s.noticeErr = "Copied cell to clipboard.", false
	return s, tea.SetClipboard(cellText(val))
}

// copyRow yanks the whole selected row to the clipboard, tab-separated so it
// pastes cleanly into a spreadsheet. Like copyCell it only acts in the results
// pane over a real grid.
func (s dashboardScreen) copyRow() (dashboardScreen, tea.Cmd) {
	cols, rows := s.currentGrid()
	if s.focus != focusResults || len(cols) == 0 || s.cellRow >= len(rows) {
		return s, nil
	}
	row := rows[s.cellRow]
	cells := make([]string, len(cols))
	for i := range cols {
		if i < len(row) {
			cells[i] = cellText(row[i])
		}
	}
	s.notice, s.noticeErr = "Copied row to clipboard.", false
	return s, tea.SetClipboard(strings.Join(cells, "\t"))
}
