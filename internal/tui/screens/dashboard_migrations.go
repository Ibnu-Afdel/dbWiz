package screens

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/migrations"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// migrationsPhase walks the overlay: read, then report (v4 3.4).
type migrationsPhase int

const (
	migrationsRunning migrationsPhase = iota
	migrationsReport
)

// migrationsState backs the overlay: which database was inspected, and the
// rendered report once it arrives.
type migrationsState struct {
	phase    migrationsPhase
	database string
	lines    []string // the rendered report, one line per entry, for scrolling
	offset   int      // first visible line
	err      string
	seq      int
}

// migrationsDoneMsg carries a finished read back to the dashboard.
type migrationsDoneMsg struct {
	seq    int
	report string
	err    string
}

// migrationsRows is how many report lines the overlay shows at once.
const migrationsRows = 18

// migrationsTimeout bounds the read. Like the plan overlay this has no cancel
// key, so it fails on its own rather than wedging the dashboard.
const migrationsTimeout = 60 * time.Second

// startMigrations reads the current database's migration ledger. It is always
// the database the browser is on, which is what makes this a plain read with no
// picker in front of it — and, unlike the schema comparison, it never moves the
// connection anywhere else.
func (s dashboardScreen) startMigrations() (dashboardScreen, tea.Cmd) {
	if s.querying {
		s.notice, s.noticeErr = "A statement is still running — wait for it to finish first.", true
		return s, nil
	}
	s.migrationsSeq++
	s.migrations = migrationsState{
		phase:    migrationsRunning,
		database: s.currentDB,
		seq:      s.migrationsSeq,
	}
	s.mode = modeMigrations
	s.working = true
	return s, tea.Batch(s.spinner.Tick, migrationsCmd(s.engine, s.currentDB, s.migrationsSeq))
}

// updateMigrations drives the overlay.
func (s dashboardScreen) updateMigrations(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	if s.migrations.phase != migrationsReport {
		return s, nil // reading: ignore keys until the report lands
	}
	switch msg.String() {
	case "esc", "q":
		s.mode = modeBrowse
		return s, nil
	case "up", "k":
		if s.migrations.offset > 0 {
			s.migrations.offset--
		}
		return s, nil
	case "down", "j":
		if s.migrations.offset < len(s.migrations.lines)-migrationsRows {
			s.migrations.offset++
		}
		return s, nil
	case "pgup":
		s.migrations.offset = max(0, s.migrations.offset-migrationsRows)
		return s, nil
	case "pgdown":
		s.migrations.offset = min(max(0, len(s.migrations.lines)-migrationsRows), s.migrations.offset+migrationsRows)
		return s, nil
	}
	return s, nil
}

// applyMigrationsDone stores a finished read, dropping a report for one the user
// has already left.
func (s dashboardScreen) applyMigrationsDone(msg migrationsDoneMsg) (dashboardScreen, tea.Cmd) {
	if s.mode != modeMigrations || msg.seq != s.migrationsSeq {
		return s, nil
	}
	s.working = false
	s.migrations.phase = migrationsReport
	s.migrations.err = msg.err
	s.migrations.offset = 0
	s.migrations.lines = nil
	if msg.report != "" {
		s.migrations.lines = strings.Split(strings.TrimRight(msg.report, "\n"), "\n")
	}
	return s, nil
}

// migrationsCmd reads the ledger off the Update goroutine.
func migrationsCmd(engine db.Engine, database string, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), migrationsTimeout)
		defer cancel()

		status, err := migrations.Detect(ctx, engine, database, 0)
		if err != nil {
			return migrationsDoneMsg{seq: seq, err: err.Error()}
		}
		return migrationsDoneMsg{seq: seq, report: migrations.Render(status)}
	}
}

// migrationsView renders the current phase.
func (s dashboardScreen) migrationsView(width int) string {
	m := s.migrations
	inner := clamp(width-8, 40, 110)
	var lines []string

	switch m.phase {
	case migrationsRunning:
		where := m.database
		if where == "" {
			where = "this database"
		}
		lines = append(lines,
			styles.Title.Render("Reading the migration ledger…"),
			"",
			s.spinner.View()+" "+fitLine(where, inner))

	case migrationsReport:
		if m.err != "" {
			lines = append(lines,
				styles.ErrorTitle.Render("Couldn't read the migrations"),
				"",
				styles.DangerText.Width(inner).Render(m.err),
				"",
				styles.Hint.Render("esc close"))
			break
		}
		lines = append(lines, styles.Title.Render("Migrations"), "")
		end := min(len(m.lines), m.offset+migrationsRows)
		for _, line := range m.lines[m.offset:end] {
			lines = append(lines, fitLine(line, inner))
		}
		hint := "esc close"
		if len(m.lines) > migrationsRows {
			hint = "↑/↓ scroll · esc close"
		}
		lines = append(lines, "", styles.Hint.Render(hint))
	}

	return styles.OverlayBox.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
