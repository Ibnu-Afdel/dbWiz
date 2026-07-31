package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// onDatabases puts the dashboard on the databases pane, where the compare action
// lives.
func onDatabases(t *testing.T) (dashboardScreen, *fakeEngine) {
	t.Helper()
	s, eng := newPGDashboard(t)
	s.focus = focusDatabases
	return s, eng
}

// TestSchemaDiffOpensPicker covers v4 1.5: [S] on the databases pane offers the
// other databases on the connection, never the baseline itself.
func TestSchemaDiffOpensPicker(t *testing.T) {
	s, _ := onDatabases(t)

	s, _ = press(s, tea.KeyPressMsg{Code: 'S', Text: "S"})
	if s.mode != modeSchemaDiff || s.schemaDiff.phase != schemaDiffPick {
		t.Fatalf("S should open the compare picker; mode=%d phase=%d", s.mode, s.schemaDiff.phase)
	}
	if s.schemaDiff.base != "postgres" {
		t.Errorf("baseline = %q, want the selected database postgres", s.schemaDiff.base)
	}
	if len(s.schemaDiff.choices) != 1 || s.schemaDiff.choices[0] != "appdb" {
		t.Errorf("choices = %v, want only appdb (never the baseline)", s.schemaDiff.choices)
	}
	if view := s.View(120, 40); !strings.Contains(view, "Compare postgres with…") {
		t.Errorf("picker should name the baseline:\n%s", view)
	}
}

// TestSchemaDiffRunsAndReports covers the running → report phases and the
// content of the rendered comparison.
func TestSchemaDiffRunsAndReports(t *testing.T) {
	s, eng := onDatabases(t)
	s, _ = press(s, tea.KeyPressMsg{Code: 'S', Text: "S"})

	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.schemaDiff.phase != schemaDiffRunning || !s.working {
		t.Fatalf("enter should start the comparison; phase=%d working=%v", s.schemaDiff.phase, s.working)
	}
	if cmd == nil {
		t.Fatal("enter should return a command that does the capturing")
	}

	// Run the real capture command against the fake engine.
	msg, ok := schemaDiffCmd(eng, "postgres", "appdb", "postgres")().(schemaDiffDoneMsg)
	if !ok {
		t.Fatal("schemaDiffCmd should report a schemaDiffDoneMsg")
	}
	if msg.err != "" {
		t.Fatalf("comparison failed: %s", msg.err)
	}

	s = feed(s, msg)
	if s.schemaDiff.phase != schemaDiffReport || s.working {
		t.Fatalf("the report should end the run; phase=%d working=%v", s.schemaDiff.phase, s.working)
	}

	view := s.View(120, 40)
	for _, want := range []string{
		"--- postgres",
		"+++ appdb",
		"- table pg_stat", // only in the baseline
		"+ table orders",  // only in appdb
		"+ table users",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("report missing %q:\n%s", want, view)
		}
	}

	// esc returns to browsing.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.mode != modeBrowse {
		t.Errorf("esc should close the report, mode=%d", s.mode)
	}
}

// TestSchemaDiffRestoresConnectionDatabase guards the Postgres-specific trap: the
// captures move the single pool, so the command must put it back where the
// dashboard thinks it is before returning.
func TestSchemaDiffRestoresConnectionDatabase(t *testing.T) {
	_, eng := onDatabases(t)

	// Baseline is appdb while the connection sits on postgres.
	schemaDiffCmd(eng, "appdb", "postgres", "postgres")()
	if eng.lastListTables != "postgres" {
		t.Errorf("connection left on %q, want it restored to postgres", eng.lastListTables)
	}
}

// TestSchemaDiffIdenticalDatabases: comparing two structurally identical
// databases says so rather than showing an empty report.
func TestSchemaDiffIdenticalDatabases(t *testing.T) {
	s, eng := onDatabases(t)
	eng.tables["appdb"] = eng.tables["postgres"]

	s, _ = press(s, tea.KeyPressMsg{Code: 'S', Text: "S"})
	s = feed(s, schemaDiffCmd(eng, "postgres", "appdb", "postgres")())

	if view := s.View(120, 40); !strings.Contains(view, "No differences") {
		t.Errorf("identical structures should say so:\n%s", view)
	}
}

// TestSchemaDiffScrolls covers the report's scrolling, which is what makes a long
// comparison readable inside the overlay.
func TestSchemaDiffScrolls(t *testing.T) {
	s, _ := onDatabases(t)
	s, _ = press(s, tea.KeyPressMsg{Code: 'S', Text: "S"})

	long := make([]string, 0, schemaDiffRows*3)
	for range schemaDiffRows * 3 {
		long = append(long, "line")
	}
	s = feed(s, schemaDiffDoneMsg{base: "postgres", report: strings.Join(long, "\n")})

	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown})
	if s.schemaDiff.offset != 1 {
		t.Errorf("down should scroll by one, offset=%d", s.schemaDiff.offset)
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if s.schemaDiff.offset != 1+schemaDiffRows {
		t.Errorf("pgdown should scroll by a page, offset=%d", s.schemaDiff.offset)
	}
	// Scrolling can never run past the end.
	for range 10 {
		s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if s.schemaDiff.offset > len(s.schemaDiff.lines)-schemaDiffRows {
		t.Errorf("offset %d ran past the end of %d lines", s.schemaDiff.offset, len(s.schemaDiff.lines))
	}
}

// TestSchemaDiffDropsStaleReport: a report for a comparison the user already left
// must not reopen the overlay.
func TestSchemaDiffDropsStaleReport(t *testing.T) {
	s, _ := onDatabases(t)
	s, _ = press(s, tea.KeyPressMsg{Code: 'S', Text: "S"})
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEscape}) // left the overlay

	s = feed(s, schemaDiffDoneMsg{base: "postgres", report: "--- a\n+++ b\n"})
	if s.mode != modeBrowse {
		t.Errorf("a late report should not reopen the overlay, mode=%d", s.mode)
	}
}

// TestSchemaDiffNeedsASecondDatabase: with nothing to compare against, the
// dashboard explains instead of opening an empty picker.
func TestSchemaDiffNeedsASecondDatabase(t *testing.T) {
	s, eng := onDatabases(t)
	eng.databases = []db.Database{{Name: "postgres"}}
	s = feed(s, databasesLoadedMsg{databases: eng.databases})
	s.focus = focusDatabases

	s, _ = press(s, tea.KeyPressMsg{Code: 'S', Text: "S"})
	if s.mode == modeSchemaDiff {
		t.Fatal("compare must not open with only one database")
	}
	if !s.noticeErr || !strings.Contains(s.notice, "no second database") {
		t.Errorf("expected an explanatory notice, got %q", s.notice)
	}
}

// TestSchemaDiffUnsupportedOnSQLite: one file, one database — the action points
// at the CLI's two-file form instead of pretending.
func TestSchemaDiffUnsupportedOnSQLite(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	eng := &fakeEngine{
		caps:   db.Capabilities{}, // all false
		tables: map[string][]db.Table{"": {{Name: "notes", Rows: 5}}},
	}
	s := NewDashboard(eng, db.Target{Path: "/tmp/dev.sqlite"},
		docker.Container{Name: "dev.sqlite", Engine: docker.EngineUnknown}).(dashboardScreen)
	s = sized(s)
	s = feed(s, tablesLoadedMsg{database: "", tables: eng.tables[""]})

	s, _ = press(s, tea.KeyPressMsg{Code: 'S', Text: "S"})
	if s.mode == modeSchemaDiff {
		t.Fatal("compare must not open on a single-database engine")
	}
	if !s.noticeErr || !strings.Contains(s.notice, "--against-file") {
		t.Errorf("the notice should point at the CLI's two-file form, got %q", s.notice)
	}
}
