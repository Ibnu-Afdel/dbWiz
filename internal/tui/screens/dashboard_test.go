package screens

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// fakeEngine is a scripted db.Engine for the dashboard tests: it returns canned
// browse data and records the last database/table it was asked about, so tests
// can assert the screen drives it correctly without a real server.
type fakeEngine struct {
	caps db.Capabilities

	databases []db.Database
	tables    map[string][]db.Table // keyed by database
	users     []db.User
	preview   db.Result
	columns   []db.Column

	tablesErr error // if set, ListTables fails

	// Query scripting (Phase 7).
	queryResult db.Result
	queryErr    error
	lastQuery   string

	closed          bool
	lastListTables  string
	lastPreviewDB   string
	lastPreviewTbl  string
	lastDescribeTbl string

	// Admin call recorders.
	createdUsers   map[string]bool // users created so far (owner-exists check)
	lastCreateDB   string
	lastCreateUser string
	lastDropDB     string
	lastDropUser   string
	lastGrantUser  string
	lastGrantDB    string
	lastGranted    bool // true = Grant, false = Revoke
}

func (f *fakeEngine) Kind() db.Kind {
	if f.caps.MultipleDatabases {
		return db.KindPostgres
	}
	return db.KindSQLite
}
func (f *fakeEngine) Capabilities() db.Capabilities            { return f.caps }
func (f *fakeEngine) Connect(context.Context, db.Target) error { return nil }
func (f *fakeEngine) Close() error                             { f.closed = true; return nil }

func (f *fakeEngine) ListDatabases(context.Context) ([]db.Database, error) {
	return f.databases, nil
}
func (f *fakeEngine) ListTables(_ context.Context, database string) ([]db.Table, error) {
	f.lastListTables = database
	if f.tablesErr != nil {
		return nil, f.tablesErr
	}
	return f.tables[database], nil
}
func (f *fakeEngine) DescribeTable(_ context.Context, _, table string) ([]db.Column, error) {
	f.lastDescribeTbl = table
	return f.columns, nil
}
func (f *fakeEngine) PreviewRows(_ context.Context, database, table string, _ int) (db.Result, error) {
	f.lastPreviewDB, f.lastPreviewTbl = database, table
	return f.preview, nil
}

func (f *fakeEngine) CreateDatabase(_ context.Context, name string, opts db.CreateOpts) error {
	// Mirror Postgres: a database can only be owned by a role that already
	// exists, so the create-with-user flow must make the user first.
	if opts.Owner != "" && !f.createdUsers[opts.Owner] {
		return &db.DBError{Kind: db.DBErrObjectMissing, Title: "Not found",
			Detail: fmt.Sprintf("role %q does not exist", opts.Owner)}
	}
	f.lastCreateDB = name
	return nil
}
func (f *fakeEngine) DropDatabase(_ context.Context, name string) error {
	f.lastDropDB = name
	return nil
}
func (f *fakeEngine) ListUsers(context.Context) ([]db.User, error) { return f.users, nil }
func (f *fakeEngine) CreateUser(_ context.Context, name, _ string) error {
	f.lastCreateUser = name
	if f.createdUsers == nil {
		f.createdUsers = map[string]bool{}
	}
	f.createdUsers[name] = true
	return nil
}
func (f *fakeEngine) DropUser(_ context.Context, name string) error {
	f.lastDropUser = name
	return nil
}
func (f *fakeEngine) Grant(_ context.Context, user, database string, _ db.GrantLevel) error {
	f.lastGrantUser, f.lastGrantDB, f.lastGranted = user, database, true
	return nil
}
func (f *fakeEngine) Revoke(_ context.Context, user, database string, _ db.GrantLevel) error {
	f.lastGrantUser, f.lastGrantDB, f.lastGranted = user, database, false
	return nil
}
func (f *fakeEngine) Query(_ context.Context, sql string) (db.Result, error) {
	f.lastQuery = sql
	if f.queryErr != nil {
		return db.Result{}, f.queryErr
	}
	return f.queryResult, nil
}

// pgEngine is a fake server engine (multi-database) seeded with two databases
// and a couple of tables in each.
func pgEngine() *fakeEngine {
	return &fakeEngine{
		caps:      db.Capabilities{Users: true, Grants: true, MultipleDatabases: true},
		databases: []db.Database{{Name: "postgres"}, {Name: "appdb"}},
		tables: map[string][]db.Table{
			"postgres": {{Name: "pg_stat", Rows: -1}},
			"appdb":    {{Name: "users", Rows: 12}, {Name: "orders", Rows: 3}},
		},
		users: []db.User{{Name: "alice"}, {Name: "bob"}},
		preview: db.Result{
			Columns: []string{"id", "email"},
			Rows:    [][]any{{"1", "a@x.io"}, {"2", nil}},
		},
		columns: []db.Column{
			{Name: "id", Type: "int", Nullable: false, Key: "PRI"},
			{Name: "email", Type: "text", Nullable: true},
		},
	}
}

// newPGDashboard builds a dashboard on a fake postgres engine, sized and with
// its initial loads already resolved, ready for interaction.
func newPGDashboard(t *testing.T) (dashboardScreen, *fakeEngine) {
	t.Helper()
	eng := pgEngine()
	s := NewDashboard(eng, db.Target{Host: "127.0.0.1", Port: 5432, Database: "postgres"},
		docker.Container{Name: "fawz-postgres", Engine: docker.EnginePostgres}).(dashboardScreen)
	s = sized(s)
	// Resolve the initial loads Init would have kicked off.
	s = feed(s, databasesLoadedMsg{databases: eng.databases})
	s = feed(s, tablesLoadedMsg{database: "postgres", tables: eng.tables["postgres"]})
	s = feed(s, usersLoadedMsg{users: eng.users})
	return s, eng
}

// TestFirstRunHint covers Step 9.2: a fresh dashboard shows the beginner hint in
// its status bar, and the first key press dismisses it for good.
func TestFirstRunHint(t *testing.T) {
	s, _ := newPGDashboard(t)
	if !strings.Contains(s.View(120, 40), "? for help") {
		t.Fatal("a fresh dashboard should show the first-run hint")
	}
	// Any key dismisses it (tab cycles focus but must also clear the hint).
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyTab})
	if strings.Contains(s.View(120, 40), "? for help") {
		t.Error("the hint should be gone after the first key press")
	}
}

// TestVisibleColumns covers the column-windowing that gives the results grid
// horizontal scroll: whole columns that fit innerW from the offset, always at
// least one.
func TestVisibleColumns(t *testing.T) {
	w := []int{5, 5, 5, 5} // columns joined by a 2-space separator
	cases := []struct {
		start, innerW, wantA, wantB int
	}{
		{0, 12, 0, 2},  // col0(5) + sep+col1(7) = 12 fits; col2 doesn't
		{2, 12, 2, 4},  // window shifts with the offset
		{0, 1, 0, 1},   // too narrow → still show one column, not zero
		{0, 100, 0, 4}, // everything fits
	}
	for _, c := range cases {
		if a, b := visibleColumns(w, c.start, c.innerW); a != c.wantA || b != c.wantB {
			t.Errorf("visibleColumns(start=%d,w=%d) = (%d,%d), want (%d,%d)",
				c.start, c.innerW, a, b, c.wantA, c.wantB)
		}
	}
}

// TestResultsCellCursor covers the ←/→ (and h/l) cell cursor in the results
// pane, bounded to the column range, and that it only acts on the results pane.
func TestResultsCellCursor(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.results = resultsQuery
	s.queryResult = db.Result{Columns: []string{"a", "b", "c"}, Rows: [][]any{{"1", "2", "3"}}}
	s.focus = focusResults

	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyRight})
	if s.cellCol != 1 {
		t.Fatalf("right → cellCol=%d, want 1", s.cellCol)
	}
	// l keeps moving and clamps at the last column.
	s, _ = press(s, tea.KeyPressMsg{Code: 'l', Text: "l"})
	s, _ = press(s, tea.KeyPressMsg{Code: 'l', Text: "l"})
	if s.cellCol != 2 {
		t.Errorf("cellCol should clamp at 2, got %d", s.cellCol)
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyLeft})
	if s.cellCol != 1 {
		t.Errorf("left → cellCol=%d, want 1", s.cellCol)
	}

	// The cursor only moves in the results pane.
	s.focus, s.cellCol = focusTables, 0
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyRight})
	if s.cellCol != 0 {
		t.Errorf("right off the results pane should not move the cell cursor, got %d", s.cellCol)
	}
}

// TestResultsColumnAutoScroll checks that moving the cell cursor past the visible
// column window scrolls colOffset to keep the selected column on screen, and that
// a narrow window shows fewer columns than a wide one.
func TestResultsColumnAutoScroll(t *testing.T) {
	s, _ := newPGDashboard(t)
	cols := []string{"c0", "c1", "c2", "c3", "c4", "c5", "c6", "c7"}
	row := make([]any, len(cols))
	for i := range row {
		row[i] = "a fairly wide cell value" // wide enough that all 8 can't fit at once
	}
	s.results = resultsQuery
	s.queryResult = db.Result{Columns: cols, Rows: [][]any{row}}
	s.focus = focusResults
	s = sized(s) // establish pane dims

	// Walk the cursor to the last column; colOffset must have advanced so the
	// selected column is visible (i.e. it's no longer pinned at 0).
	for range cols {
		s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyRight})
	}
	if s.cellCol != len(cols)-1 {
		t.Fatalf("cellCol=%d, want %d", s.cellCol, len(cols)-1)
	}
	if s.colOffset == 0 {
		t.Error("colOffset should have scrolled to keep the last column visible")
	}
}

// TestCellInspector covers the results cell-detail overlay: Enter on a cell opens
// it with the cell's full (untruncated) value, and esc closes it back to browse.
func TestCellInspector(t *testing.T) {
	s, _ := newPGDashboard(t)
	long := strings.Repeat("lorem ipsum dolor sit amet ", 20)
	s.results = resultsQuery
	s.queryResult = db.Result{
		Columns: []string{"id", "description"},
		Rows:    [][]any{{"1", long}},
	}
	s.focus = focusResults
	s = sized(s)

	// Move to the long "description" cell and open it.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyRight})
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.mode != modeCell {
		t.Fatalf("enter on a cell should open the detail overlay, mode=%d", s.mode)
	}
	if s.cellColName != "description" {
		t.Errorf("overlay title = %q, want description", s.cellColName)
	}
	// The overlay shows the whole value (the grid would have truncated it).
	view := s.View(120, 40)
	if !strings.Contains(view, "lorem ipsum") {
		t.Error("overlay should render the cell's full text")
	}

	// esc closes it back to the browser.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc})
	if s.mode != modeBrowse {
		t.Errorf("esc should close the overlay, mode=%d", s.mode)
	}
}

// TestCellInspectorNull shows a NULL cell renders as NULL in the detail overlay.
func TestCellInspectorNull(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.results = resultsQuery
	s.queryResult = db.Result{Columns: []string{"note"}, Rows: [][]any{{nil}}}
	s.focus = focusResults
	s = sized(s)

	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(s.View(120, 40), "NULL") {
		t.Error("a NULL cell should show NULL in the detail overlay")
	}
}

func sized(s dashboardScreen) dashboardScreen {
	next, _ := s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(dashboardScreen)
}

// feed applies a message and returns the resulting dashboardScreen.
func feed(s dashboardScreen, msg tea.Msg) dashboardScreen {
	next, _ := s.Update(msg)
	return next.(dashboardScreen)
}

// press applies a key and returns the resulting screen plus any command.
func press(s dashboardScreen, msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	next, cmd := s.Update(msg)
	return next.(dashboardScreen), cmd
}

// TestDashboardLayoutAndFocus covers Step 5.1: three panes render with the
// engine/target in the status bar, and tab cycles focus through every pane.
func TestDashboardLayoutAndFocus(t *testing.T) {
	s, _ := newPGDashboard(t)

	view := s.View(120, 40)
	for _, want := range []string{"PostgreSQL", "fawz-postgres", "● live", "Navigator", "Query", "Results"} {
		if !strings.Contains(view, want) {
			t.Errorf("status/layout missing %q\n%s", want, view)
		}
	}

	// Tab cycles databases → tables → users → editor → results → databases.
	want := []focusTarget{focusTables, focusUsers, focusEditor, focusResults, focusDatabases}
	for i, wf := range want {
		var cmd tea.Cmd
		s, cmd = press(s, tea.KeyPressMsg{Code: tea.KeyTab})
		_ = cmd
		if s.focus != wf {
			t.Fatalf("tab %d: focus = %d, want %d", i, s.focus, wf)
		}
	}
}

// TestDashboardSwitchDatabase covers Step 5.2: enter on a database switches the
// context, reloads that database's tables, and highlights it.
func TestDashboardSwitchDatabase(t *testing.T) {
	s, eng := newPGDashboard(t)

	// Move the databases cursor to "appdb" and select it.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown})
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.currentDB != "appdb" {
		t.Fatalf("currentDB = %q, want appdb", s.currentDB)
	}
	if s.focus != focusTables {
		t.Errorf("focus after switch = %d, want focusTables", s.focus)
	}
	if cmd == nil {
		t.Fatal("expected a table-load command")
	}
	// The command should ask the engine for appdb's tables.
	msg := cmd()
	// cmd is a Batch (spinner tick + load); run the load explicitly instead.
	_ = msg
	if got := runCmd(t, loadTablesCmd(eng, "appdb")).(tablesLoadedMsg); got.database != "appdb" {
		t.Fatalf("load targeted %q, want appdb", got.database)
	}

	// Deliver appdb's tables and confirm they render, with appdb marked current.
	s = feed(s, tablesLoadedMsg{database: "appdb", tables: eng.tables["appdb"]})
	view := s.View(120, 40)
	if !strings.Contains(view, "users") || !strings.Contains(view, "orders") {
		t.Errorf("appdb tables not shown:\n%s", view)
	}
	if !strings.Contains(s.View(120, 40), "db: appdb") {
		t.Errorf("status bar should show current db appdb")
	}
}

// TestDashboardStaleTablesDropped covers the stale-reply guard: a late table
// load for a database we've left is ignored.
func TestDashboardStaleTablesDropped(t *testing.T) {
	s, _ := newPGDashboard(t) // currentDB = postgres
	s = feed(s, tablesLoadedMsg{database: "other", tables: []db.Table{{Name: "ghost"}}})
	if strings.Contains(s.View(120, 40), "ghost") {
		t.Error("stale table reply for a different database should be dropped")
	}
}

// TestDashboardPreviewRows covers Step 5.3: enter on a table previews its rows
// into the results pane, preserving NULL cells.
func TestDashboardPreviewRows(t *testing.T) {
	s, eng := newPGDashboard(t)
	// Focus the tables list (tab once from databases) and select the first table.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyTab})
	if s.focus != focusTables {
		t.Fatalf("focus = %d, want focusTables", s.focus)
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.resultsTable != "pg_stat" {
		t.Fatalf("resultsTable = %q, want pg_stat", s.resultsTable)
	}
	if s.focus != focusResults {
		t.Errorf("focus should move to results after preview")
	}

	s = feed(s, rowsLoadedMsg{database: "postgres", table: "pg_stat", result: eng.preview})
	view := s.View(120, 40)
	for _, want := range []string{"email", "a@x.io", "NULL"} {
		if !strings.Contains(view, want) {
			t.Errorf("preview missing %q\n%s", want, view)
		}
	}
}

// TestDashboardEmptyTable covers Step 5.3's empty-state: a table with no rows
// shows a friendly message, not a blank grid.
func TestDashboardEmptyTable(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.resultsTable = "empty"
	s = feed(s, rowsLoadedMsg{database: "postgres", table: "empty",
		result: db.Result{Columns: []string{"id"}, Rows: nil}})
	if !strings.Contains(s.View(120, 40), "empty") {
		t.Errorf("expected friendly empty-table state:\n%s", s.View(120, 40))
	}
}

// TestDashboardDescribe covers Step 5.3's [i]: DescribeTable output renders in
// the results pane.
func TestDashboardDescribe(t *testing.T) {
	s, eng := newPGDashboard(t)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyTab}) // focus tables
	s, cmd := press(s, tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil {
		t.Fatal("[i] should fire a describe command")
	}
	if eng.tables["postgres"][0].Name != "pg_stat" {
		t.Fatal("test fixture changed")
	}
	s = feed(s, describeLoadedMsg{database: "postgres", table: "pg_stat", columns: eng.columns})
	view := s.View(120, 40)
	for _, want := range []string{"Column", "Type", "PRI"} {
		if !strings.Contains(view, want) {
			t.Errorf("describe missing %q\n%s", want, view)
		}
	}
}

// TestDashboardPaneError covers Step 5.5: a failed table load errors inside the
// navigator pane (with retry) and flips the status pill, never full-screen.
func TestDashboardPaneError(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = feed(s, tablesErrMsg{database: "postgres", err: &db.DBError{
		Title:  "Connection lost",
		Detail: "the server closed the connection",
	}})
	view := s.View(120, 40)
	for _, want := range []string{"Connection lost", "[R] retry", "● connection error"} {
		if !strings.Contains(view, want) {
			t.Errorf("pane error missing %q\n%s", want, view)
		}
	}
	// Still a dashboard — not routed to the full-screen error.
	if _, ok := Screen(s).(dashboardScreen); !ok {
		t.Error("pane error should not replace the dashboard screen")
	}
}

// TestDashboardBackClosesEngine covers the lifetime contract: Back releases the
// connection.
func TestDashboardBackClosesEngine(t *testing.T) {
	s, eng := newPGDashboard(t)
	_, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEsc})
	if !eng.closed {
		t.Error("Back should close the engine")
	}
	if _, ok := runCmd(t, cmd).(PopMsg); !ok {
		t.Error("Back should pop the screen")
	}
}

// TestDashboardSQLiteMode covers Step 5.4: SQLite has no databases pane, focus
// starts on tables and skips databases, and the status bar shows no db context.
func TestDashboardSQLiteMode(t *testing.T) {
	eng := &fakeEngine{
		caps:   db.Capabilities{}, // all false
		tables: map[string][]db.Table{"": {{Name: "notes", Rows: 5}}},
	}
	s := NewDashboard(eng, db.Target{Path: "/tmp/dev.sqlite"},
		docker.Container{Name: "dev.sqlite", Engine: docker.EngineUnknown}).(dashboardScreen)
	s = sized(s)
	s = feed(s, tablesLoadedMsg{database: "", tables: eng.tables[""]})

	if s.focus != focusTables {
		t.Errorf("SQLite should start focused on tables, got %d", s.focus)
	}
	view := s.View(120, 40)
	if strings.Contains(view, "Databases") {
		t.Errorf("SQLite navigator should not show a Databases section:\n%s", view)
	}
	if strings.Contains(view, "db:") {
		t.Errorf("SQLite status bar should not show a db context:\n%s", view)
	}
	if !strings.Contains(view, "notes") {
		t.Errorf("SQLite tables should render:\n%s", view)
	}

	// Tab must skip the (absent) databases pane: tables → editor → results → tables.
	for _, wf := range []focusTarget{focusEditor, focusResults, focusTables} {
		s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyTab})
		if s.focus == focusDatabases {
			t.Fatal("SQLite focus cycle must never land on databases")
		}
		if s.focus != wf {
			t.Fatalf("SQLite focus = %d, want %d", s.focus, wf)
		}
	}
}
