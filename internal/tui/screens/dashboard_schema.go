package screens

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/schema"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// schemaDiffPhase walks the compare flow: pick the other database, wait for both
// captures, then read the report (v4 1.5).
type schemaDiffPhase int

const (
	schemaDiffPick schemaDiffPhase = iota
	schemaDiffRunning
	schemaDiffReport
)

// schemaDiffState backs the compare overlay: which database is the baseline,
// which others can be picked, and the rendered report once it arrives.
type schemaDiffState struct {
	phase   schemaDiffPhase
	base    string   // the "from" side — the database in context
	choices []string // every other database on this connection
	cursor  int
	lines   []string // the rendered report, one line per entry, for scrolling
	offset  int      // first visible line
	err     string
}

// schemaDiffDoneMsg carries a finished comparison back to the dashboard.
type schemaDiffDoneMsg struct {
	base   string
	report string
	err    string
}

// schemaDiffRows is how many report lines the overlay shows at once. The overlay
// is a fixed box rather than a full screen, so the report scrolls inside it.
const schemaDiffRows = 18

// startSchemaDiff opens the compare overlay for the database in context. It
// needs an engine that actually hosts more than one database — on SQLite there
// is nothing to compare against, so it says so rather than offering an empty
// list. Comparing across two *servers* stays a CLI job (`dbwiz schema diff
// --against-target`), because the dashboard owns exactly one connection.
func (s dashboardScreen) startSchemaDiff() (dashboardScreen, tea.Cmd) {
	if !s.engine.Capabilities().MultipleDatabases {
		s.notice, s.noticeErr = "Comparing databases needs an engine that hosts more than one — use `dbwiz schema diff --against-file` for two SQLite files.", true
		return s, nil
	}
	base := s.currentDB
	if s.focus == focusDatabases {
		if d, ok := s.selectedDatabase(); ok {
			base = d.Name
		}
	}
	if base == "" {
		s.notice, s.noticeErr = "Pick a database to compare first.", true
		return s, nil
	}

	var choices []string
	for _, d := range s.databases {
		if d.Name != base {
			choices = append(choices, d.Name)
		}
	}
	if len(choices) == 0 {
		s.notice, s.noticeErr = "There's no second database on this connection to compare "+base+" with.", true
		return s, nil
	}

	s.schemaDiff = schemaDiffState{phase: schemaDiffPick, base: base, choices: choices}
	s.mode = modeSchemaDiff
	return s, nil
}

// updateSchemaDiff drives the overlay one phase at a time.
func (s dashboardScreen) updateSchemaDiff(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	switch s.schemaDiff.phase {
	case schemaDiffPick:
		switch msg.String() {
		case "esc":
			s.mode = modeBrowse
			return s, nil
		case "up", "k":
			if s.schemaDiff.cursor > 0 {
				s.schemaDiff.cursor--
			}
			return s, nil
		case "down", "j":
			if s.schemaDiff.cursor < len(s.schemaDiff.choices)-1 {
				s.schemaDiff.cursor++
			}
			return s, nil
		case "enter":
			return s.runSchemaDiff()
		}
		return s, nil

	case schemaDiffReport:
		switch msg.String() {
		case "esc", "q", "enter":
			s.mode = modeBrowse
			return s, nil
		case "up", "k":
			if s.schemaDiff.offset > 0 {
				s.schemaDiff.offset--
			}
			return s, nil
		case "down", "j":
			if s.schemaDiff.offset < len(s.schemaDiff.lines)-schemaDiffRows {
				s.schemaDiff.offset++
			}
			return s, nil
		case "pgup":
			s.schemaDiff.offset = max(0, s.schemaDiff.offset-schemaDiffRows)
			return s, nil
		case "pgdown":
			s.schemaDiff.offset = min(max(0, len(s.schemaDiff.lines)-schemaDiffRows), s.schemaDiff.offset+schemaDiffRows)
			return s, nil
		}
		return s, nil
	}
	return s, nil // running: ignore keys until the captures land
}

// runSchemaDiff kicks off the two captures.
func (s dashboardScreen) runSchemaDiff() (dashboardScreen, tea.Cmd) {
	if s.schemaDiff.cursor < 0 || s.schemaDiff.cursor >= len(s.schemaDiff.choices) {
		return s, nil
	}
	other := s.schemaDiff.choices[s.schemaDiff.cursor]
	s.schemaDiff.phase = schemaDiffRunning
	s.working = true
	return s, tea.Batch(s.spinner.Tick, schemaDiffCmd(s.engine, s.schemaDiff.base, other, s.currentDB))
}

// applySchemaDiffDone stores the finished report (or the failure) and shows it.
func (s dashboardScreen) applySchemaDiffDone(msg schemaDiffDoneMsg) (dashboardScreen, tea.Cmd) {
	// A report for a comparison the user has already left is dropped, the same
	// way a superseded query reply is.
	if s.mode != modeSchemaDiff || s.schemaDiff.base != msg.base {
		return s, nil
	}
	s.working = false
	s.schemaDiff.phase = schemaDiffReport
	s.schemaDiff.err = msg.err
	s.schemaDiff.offset = 0
	s.schemaDiff.lines = nil
	if msg.report != "" {
		s.schemaDiff.lines = strings.Split(strings.TrimRight(msg.report, "\n"), "\n")
	}
	return s, nil
}

// schemaDiffCmd captures both databases off the Update goroutine and renders the
// comparison.
//
// The captures run "other" first and the baseline second, then restore the
// connection to restoreDB: Postgres reaches another database by reconnecting its
// single pool, so without that last step a comparison would silently move the
// query editor's database out from under the user. MySQL and SQLite don't move,
// which makes the restore a no-op there rather than a special case.
func schemaDiffCmd(engine db.Engine, base, other, restoreDB string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		to, err := schema.Capture(ctx, engine, other)
		if err != nil {
			return schemaDiffDoneMsg{base: base, err: err.Error()}
		}
		from, err := schema.Capture(ctx, engine, base)
		if err != nil {
			return schemaDiffDoneMsg{base: base, err: err.Error()}
		}
		if restoreDB != "" && restoreDB != base {
			// Best-effort: a failure here shows up on the next pane load, and must
			// not discard a comparison the user is waiting for.
			_, _ = engine.ListTables(ctx, restoreDB)
		}

		from.Kind, to.Kind = engine.Kind(), engine.Kind()
		return schemaDiffDoneMsg{base: base, report: schema.Render(schema.Diff(from, to))}
	}
}

// schemaDiffView renders the current phase.
func (s dashboardScreen) schemaDiffView(width int) string {
	d := s.schemaDiff
	inner := clamp(width-8, 40, 110)
	var lines []string

	switch d.phase {
	case schemaDiffPick:
		lines = append(lines,
			styles.Title.Render("Compare "+d.base+" with…"),
			"")
		for i, name := range d.choices {
			lines = append(lines, choiceLine(name, i == d.cursor))
		}
		lines = append(lines, "", styles.Hint.Render("↑/↓ choose · enter compare · esc cancel"))

	case schemaDiffRunning:
		lines = append(lines,
			styles.Title.Render("Comparing "+d.base+"…"),
			"",
			s.spinner.View()+" reading both structures")

	case schemaDiffReport:
		if d.err != "" {
			lines = append(lines,
				styles.ErrorTitle.Render("Couldn't compare"),
				"",
				styles.DangerText.Width(inner).Render(d.err),
				"",
				styles.Hint.Render("esc close"))
			break
		}
		lines = append(lines, styles.Title.Render("Structure comparison"), "")
		end := min(len(d.lines), d.offset+schemaDiffRows)
		for _, line := range d.lines[d.offset:end] {
			lines = append(lines, fitLine(line, inner))
		}
		hint := "esc close"
		if len(d.lines) > schemaDiffRows {
			hint = "↑/↓ scroll · esc close"
		}
		lines = append(lines, "", styles.Hint.Render(hint))
	}

	return styles.OverlayBox.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
