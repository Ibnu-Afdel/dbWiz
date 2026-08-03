package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

var keyM = tea.KeyPressMsg{Code: 'M', Text: "M"}

// ledgered puts a Laravel-shaped migration table in the connected database and
// scripts the two statements the read composes: the exact count, then the rows.
func ledgered(t *testing.T) (dashboardScreen, *fakeEngine) {
	t.Helper()
	s, eng := newPGDashboard(t)
	eng.tables["postgres"] = []db.Table{{Name: "users"}, {Name: "migrations"}}
	eng.columns = []db.Column{
		{Name: "id", Type: "int", Key: "PRI"},
		{Name: "migration", Type: "text"},
		{Name: "batch", Type: "int"},
	}
	eng.mutationFn = func(sql string) (db.Result, error) {
		if strings.HasPrefix(sql, "SELECT COUNT(*)") {
			return db.Result{Columns: []string{"count"}, Rows: [][]any{{int64(2)}}}, nil
		}
		return db.Result{
			Columns: []string{"id", "migration", "batch"},
			Rows: [][]any{
				{int64(2), "2026_07_14_120000_create_orders_table", int64(2)},
				{int64(1), "2024_10_12_000000_create_users_table", int64(1)},
			},
		}, nil
	}
	s = feed(s, tablesLoadedMsg{database: "postgres", tables: eng.tables["postgres"]})
	return s, eng
}

// TestMigrationsOpensFromBrowsePane covers v4 3.4: [M] answers "what migration is
// this database on?" without leaving the dashboard.
func TestMigrationsOpensFromBrowsePane(t *testing.T) {
	s, eng := ledgered(t)

	s, cmd := press(s, keyM)
	if s.mode != modeMigrations || s.migrations.phase != migrationsRunning || !s.working {
		t.Fatalf("[M] should start a read; mode=%d phase=%d working=%v", s.mode, s.migrations.phase, s.working)
	}
	if cmd == nil {
		t.Fatal("[M] should return a command that does the reading")
	}

	msg, ok := migrationsCmd(eng, s.migrations.database, s.migrationsSeq)().(migrationsDoneMsg)
	if !ok {
		t.Fatal("migrationsCmd should report a migrationsDoneMsg")
	}
	if msg.err != "" {
		t.Fatalf("read failed: %s", msg.err)
	}

	s = feed(s, msg)
	if s.migrations.phase != migrationsReport || s.working {
		t.Fatalf("the report should end the read; phase=%d working=%v", s.migrations.phase, s.working)
	}

	view := s.View(120, 40)
	for _, want := range []string{
		"Migrations",
		"Laravel · migrations · 2 migrations applied",
		"2026_07_14_120000_create_orders_table",
		"esc close",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("report missing %q:\n%s", want, view)
		}
	}

	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.mode != modeBrowse {
		t.Errorf("esc should close the report, mode=%d", s.mode)
	}
}

// TestMigrationsReadsTheCurrentDatabase: the report is about the database the
// browser is on, and — unlike the schema comparison — reading it must leave the
// connection exactly where it was.
func TestMigrationsReadsTheCurrentDatabase(t *testing.T) {
	s, eng := ledgered(t)
	eng.tables["appdb"] = eng.tables["postgres"]
	s.focus = focusDatabases
	s.dbCursor = 1 // appdb
	s, _ = s.selectFocused()
	s = feed(s, tablesLoadedMsg{database: "appdb", tables: eng.tables["appdb"]})

	s, _ = press(s, keyM)
	if s.migrations.database != "appdb" {
		t.Fatalf("read %q, want the current database", s.migrations.database)
	}
	s = feed(s, migrationsCmd(eng, s.migrations.database, s.migrationsSeq)())

	if eng.lastMutationDB != "appdb" {
		t.Errorf("the read went to %q, want appdb", eng.lastMutationDB)
	}
	if s.currentDB != "appdb" {
		t.Errorf("current database moved to %q", s.currentDB)
	}
	if eng.lastQuery != "" {
		t.Errorf("nothing should go through the editor's own path, got %q", eng.lastQuery)
	}
}

// TestMigrationsNoLedger: a database nothing manages gets the explanation, not an
// empty box.
func TestMigrationsNoLedger(t *testing.T) {
	s, eng := newPGDashboard(t)

	s, _ = press(s, keyM)
	s = feed(s, migrationsCmd(eng, s.migrations.database, s.migrationsSeq)())

	if view := s.View(120, 40); !strings.Contains(view, "No migration ledger found.") {
		t.Errorf("report should explain itself:\n%s", view)
	}
}

// TestMigrationsWaitsForARunningStatement: the connection is busy, so asking
// explains rather than queueing behind the statement.
func TestMigrationsWaitsForARunningStatement(t *testing.T) {
	s, _ := ledgered(t)
	s.querying = true

	s, _ = s.startMigrations()
	if s.mode == modeMigrations {
		t.Fatal("a read must not start while a statement is running")
	}
	if !s.noticeErr || !strings.Contains(s.notice, "still running") {
		t.Errorf("expected an explanatory notice, got %q", s.notice)
	}
}

// TestMigrationsDropsStaleReport: a report for a read the user already left must
// not reopen the overlay.
func TestMigrationsDropsStaleReport(t *testing.T) {
	s, _ := ledgered(t)
	s, _ = press(s, keyM)
	s = feed(s, migrationsDoneMsg{seq: s.migrationsSeq, report: "Laravel · migrations · 2 migrations applied\n"})
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEscape})

	s = feed(s, migrationsDoneMsg{seq: s.migrationsSeq, report: "late arrival"})
	if s.mode != modeBrowse {
		t.Errorf("a late report should not reopen the overlay, mode=%d", s.mode)
	}
	if strings.Contains(s.View(120, 40), "late arrival") {
		t.Error("the stale report reached the screen")
	}
}

// TestMigrationsScrolls: a long history has to be readable inside the overlay.
func TestMigrationsScrolls(t *testing.T) {
	s, _ := ledgered(t)
	s, _ = press(s, keyM)

	long := make([]string, 0, migrationsRows*3)
	for range migrationsRows * 3 {
		long = append(long, "  2026_07_14_120000_create_orders_table")
	}
	s = feed(s, migrationsDoneMsg{seq: s.migrationsSeq, report: strings.Join(long, "\n")})

	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown})
	if s.migrations.offset != 1 {
		t.Errorf("down should scroll by one, offset=%d", s.migrations.offset)
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if s.migrations.offset != 1+migrationsRows {
		t.Errorf("pgdown should scroll by a page, offset=%d", s.migrations.offset)
	}
	for range 10 {
		s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if s.migrations.offset > len(s.migrations.lines)-migrationsRows {
		t.Errorf("offset %d ran past the end of %d lines", s.migrations.offset, len(s.migrations.lines))
	}
}

// TestMigrationsErrorShowsInTheOverlay: a failed read explains itself in place
// rather than dropping the user onto the full-screen error path.
func TestMigrationsErrorShowsInTheOverlay(t *testing.T) {
	s, eng := ledgered(t)
	eng.tablesErr = &db.DBError{Kind: db.DBErrConnRefused, Title: "Connection refused", Detail: "server closed the connection"}

	s, _ = press(s, keyM)
	s = feed(s, migrationsCmd(eng, s.migrations.database, s.migrationsSeq)())

	if s.migrations.err == "" {
		t.Fatal("a failed read must be reported")
	}
	if view := s.View(120, 40); !strings.Contains(view, "Couldn't read the migrations") {
		t.Errorf("the failure should be shown in the overlay:\n%s", view)
	}
}
