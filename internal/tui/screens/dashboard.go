package screens

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// focusTarget is which pane currently has keyboard focus. Tab cycles through the
// panes the engine actually offers: SQLite has no databases pane, so it is
// skipped there (Capabilities-driven, per Phase 5.4).
type focusTarget int

const (
	focusDatabases focusTarget = iota
	focusTables
	focusUsers
	focusEditor
	focusResults
)

// adminMode is the dashboard's current interaction mode. Admin actions run as
// modal overlays over the browser rather than separate stacked screens, so the
// engine, the lists, and the post-action refresh all stay in one place.
type adminMode int

const (
	modeBrowse   adminMode = iota // the three-pane browser
	modeForm                      // a create form (database or user)
	modeConfirm                   // a type-the-name destructive confirm
	modeGrant                     // the grant/revoke picker
	modeCell                      // the results cell-detail overlay (full value)
	modeHistory                   // the searchable per-target query-history picker (v2 2.1)
	modeSaved                     // the searchable saved/favourite-query picker (v2 2.2)
	modeExport                    // the CSV/JSON results-export chooser (v2 2.3)
	modeComplete                  // the schema-aware autocomplete picker (v2 2.5)
	modeEditCell                  // the results cell editor that generates an UPDATE (v3 2.1)
)

// formPurpose records which create command a submitted form should run.
type formPurpose int

const (
	purposeCreateDB formPurpose = iota
	purposeCreateUser
	purposeEditUser  // v2 3.2: edit an existing user's flags/password
	purposeSaveQuery // v2 2.2: name the editor's statement as a saved query
)

// confirmKind records which destructive command an accepted confirm should run.
type confirmKind int

const (
	confirmDropDB confirmKind = iota
	confirmDropUser
)

// resultsView is what the bottom-right pane is showing.
type resultsView int

const (
	resultsIdle     resultsView = iota // nothing selected yet
	resultsRows                        // PreviewRows output
	resultsDescribe                    // DescribeTable output
	resultsQuery                       // ad-hoc Query output (Phase 7)
)

// dashboardScreen is the Phase 5 three-pane browser: a left navigator
// (databases → tables), a top-right query editor placeholder (Phase 7), and a
// bottom-right results grid showing row previews or a table's columns. It owns
// the live engine's lifetime, closing it on Back. Every list load is async with
// its own spinner and inline, pane-level error — a killed container never
// dead-ends the whole screen (Phase 5.5).
type dashboardScreen struct {
	engine    db.Engine
	target    db.Target
	container docker.Container
	caps      db.Capabilities

	width, height int
	focus         focusTarget

	// hintDismissed suppresses the one-time beginner hint in the status bar once
	// the user presses their first key on this dashboard (Step 9.2).
	hintDismissed bool

	spinner spinner.Model

	// Navigator: databases (server engines only) then tables under currentDB.
	databases []db.Database
	dbCursor  int
	currentDB string // the database tables are currently listed from

	tables    []db.Table
	tblCursor int

	// Users section (server engines with Capabilities.Users only).
	users      []db.User
	userCursor int

	// Admin overlay state. mode drives which of these is live; notice is the
	// transient result toast (noticeErr colours it as a failure). openWithCreate
	// makes the dashboard pop the create-database form on first appearance — the
	// home "Create new database" route (Step 6.7).
	mode           adminMode
	form           formModel
	formPurpose    formPurpose
	editUserName   string // subject of an open edit-user form (v2 3.2)
	confirm        confirmModel
	confirmKind    confirmKind
	confirmTarget  string
	grant          grantModel
	notice         string
	noticeErr      bool
	working        bool // an admin mutation is in flight
	openWithCreate bool

	// Results: either a row preview or a describe, for the table named in
	// resultsTable, plus scroll offsets into whichever is showing. resultOffset
	// is the top visible row; colOffset is the left visible column (horizontal
	// scroll, so wide tables aren't just cut off).
	results      resultsView
	resultsTable string
	preview      db.Result
	columns      []db.Column
	resultOffset int
	colOffset    int

	// Cell cursor + detail overlay: cellRow/cellCol is the selected cell in the
	// results grid (highlighted when the pane is focused); Enter opens cellVP, a
	// scrollable viewport showing that cell's full, untruncated value so long
	// text is readable in place. cellColName titles the overlay.
	cellRow     int
	cellCol     int
	cellVP      viewport.Model
	cellColName string

	// Query editor (Phase 7). editor is the multi-line SQL input; a submitted
	// statement runs async under queryCancel with the monotonic querySeq so a
	// late reply for a superseded run is dropped. queryErr renders inline under
	// the editor (user errors only — system errors route to the error screen).
	// history is an in-memory ring of submitted statements; historyIdx is the
	// browse position (-1 = not browsing, editing fresh text).
	editor      textarea.Model
	querying    bool
	queryStart  time.Time
	queryCancel context.CancelFunc
	querySeq    int
	queryErr    *db.DBError
	queryResult db.Result
	queryVerb   string
	history     []string
	historyIdx  int
	// historyKey identifies this target in the cross-session history store; it is
	// the key both the seed (in NewDashboard) and every persist write use so the
	// same connection accumulates one history across sessions (v2 Step 2.1).
	historyKey string
	// historyList is the searchable/fuzzy-filtered overlay opened with alt+h
	// (modeHistory). It is (re)built from the store each time it opens.
	historyList historyModel
	// savedList is the searchable saved/favourite-query picker opened with alt+s
	// (modeSaved), rebuilt from the store each time it opens (v2 2.2).
	savedList savedModel
	// export is the CSV/JSON chooser opened with alt+e over the current results
	// grid (modeExport, v2 2.3).
	export exportModel
	// vim is the modal-editor state (v2 2.4): enabled from config, plus the current
	// mode. Off by default, in which case the editor never intercepts keys.
	vim vimState

	// cellEdit backs the results cell editor (v3 2.1): the value input plus the
	// row identity (primary-key columns/values) needed to generate a safe UPDATE.
	cellEdit cellEditState

	// columnCache maps a table name to its column names, warmed as the user
	// previews or describes tables. It's the "cached metadata" the editor's
	// schema-aware autocomplete draws on (v2 2.5) — no extra queries, just what
	// browsing already fetched. completeList is the completion picker overlay
	// (modeComplete); completeLine/Start/End mark the word-prefix it will replace.
	columnCache   map[string][]string
	completeList  completeModel
	completeLine  int
	completeStart int
	completeEnd   int

	// Per-pane async state. A load sets its loading flag; the reply clears it or
	// sets its err. Errors render inside the pane with an [R] retry, never
	// full-screen.
	dbLoading    bool
	tblLoading   bool
	resLoading   bool
	usersLoading bool
	dbErr        *db.DBError
	tblErr       *db.DBError
	resErr       *db.DBError
	usersErr     *db.DBError
}

// NewDashboard builds the browser around a live engine connection. currentDB
// starts at the target's initial database (the maintenance DB for server
// engines; empty for SQLite).
func NewDashboard(engine db.Engine, target db.Target, c docker.Container) Screen {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = styles.Selected

	key := historyKeyFor(c, target)
	s := dashboardScreen{
		engine:     engine,
		target:     target,
		container:  c,
		caps:       engine.Capabilities(),
		spinner:    sp,
		currentDB:  target.Database,
		editor:     newSQLEditor(),
		historyIdx: -1,
		historyKey: key,
		// The modal editor reads the shared config preference (v2 2.4); off by
		// default, in which case it behaves exactly like the plain textarea.
		vim: newVimState(vimEditor),
		// Seed the in-memory cycle ring from the persisted store so ctrl+p/ctrl+n
		// recall this target's past statements immediately on reconnect (v2 2.1).
		history: seedHistory(key),
	}
	// The loading flags are set here, not in Init: Init runs on a value copy, so
	// flags set there wouldn't reach the stored model and the spinner would never
	// animate during the first load. Focus starts on the first pane the engine
	// actually offers.
	s.tblLoading = true
	if s.caps.MultipleDatabases {
		s.dbLoading = true
	} else {
		s.focus = focusTables
	}
	if s.caps.Users {
		s.usersLoading = true
	}
	return s
}

// Title labels the tab this dashboard lives in with the connected target's name
// (a container name, or a SQLite file's base name). It satisfies screens.Titled.
func (s dashboardScreen) Title() string { return s.container.Name }

// CapturesText reports whether the dashboard is currently taking free text, so
// the root leaves digit keys for it instead of switching tabs: while a create
// form or type-the-name confirm is open, or while the SQL editor has focus. It
// satisfies screens.TextInputer.
func (s dashboardScreen) CapturesText() bool {
	if s.mode == modeForm || s.mode == modeConfirm || s.mode == modeEditCell {
		return true
	}
	if s.mode == modeHistory {
		return s.historyList.settingFilter()
	}
	if s.mode == modeSaved {
		return s.savedList.settingFilter()
	}
	if s.mode == modeComplete {
		return s.completeList.settingFilter()
	}
	return s.mode == modeBrowse && s.focus == focusEditor
}

// Close releases the live engine. It satisfies screens.Closer so the root can
// tear down a background target's connection when its whole tab is closed
// without the dashboard ever handling Back. Closing the engine twice is
// harmless, so this coexists with the Back path's own Close.
func (s dashboardScreen) Close() error { return s.engine.Close() }

// NewDashboardCreating is NewDashboard that opens straight into the
// create-database form — the landing point for the home "Create new database"
// route (Step 6.7).
func NewDashboardCreating(engine db.Engine, target db.Target, c docker.Container) Screen {
	s := NewDashboard(engine, target, c).(dashboardScreen)
	s.openWithCreate = true
	return s
}

func (s dashboardScreen) Init() tea.Cmd {
	cmds := []tea.Cmd{s.spinner.Tick, loadTablesCmd(s.engine, s.currentDB)}
	if s.caps.MultipleDatabases {
		cmds = append(cmds, loadDatabasesCmd(s.engine))
	}
	if s.caps.Users {
		cmds = append(cmds, loadUsersCmd(s.engine))
	}
	if s.openWithCreate {
		cmds = append(cmds, func() tea.Msg { return openCreateDBMsg{} })
	}
	return tea.Batch(cmds...)
}

func (s dashboardScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		s.resizeEditor()
		return s, nil

	case spinner.TickMsg:
		if !s.anyLoading() {
			return s, nil
		}
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd

	case databasesLoadedMsg:
		s.dbLoading, s.dbErr = false, nil
		s.databases = msg.databases
		s.dbCursor = clampCursor(s.dbCursor, len(s.databases))
		return s, nil
	case databasesErrMsg:
		s.dbLoading, s.dbErr = false, msg.err
		return s, nil

	case tablesLoadedMsg:
		if msg.database != s.currentDB {
			return s, nil // stale reply for a database we've since left
		}
		s.tblLoading, s.tblErr = false, nil
		s.tables = msg.tables
		s.tblCursor = clampCursor(s.tblCursor, len(s.tables))
		return s, nil
	case tablesErrMsg:
		if msg.database != s.currentDB {
			return s, nil
		}
		s.tblLoading, s.tblErr = false, msg.err
		s.tables = nil
		return s, nil

	case rowsLoadedMsg:
		if !s.isCurrentTable(msg.database, msg.table) {
			return s, nil
		}
		s.resLoading, s.resErr = false, nil
		s.results, s.preview, s.resultOffset, s.colOffset = resultsRows, msg.result, 0, 0
		s.cellRow, s.cellCol = 0, 0
		s.cacheColumns(msg.table, msg.result.Columns) // warm autocomplete (v2 2.5)
		return s, nil
	case rowsErrMsg:
		if !s.isCurrentTable(msg.database, msg.table) {
			return s, nil
		}
		s.resLoading, s.resErr = false, msg.err
		return s, nil

	case describeLoadedMsg:
		if !s.isCurrentTable(msg.database, msg.table) {
			return s, nil
		}
		s.resLoading, s.resErr = false, nil
		s.results, s.columns, s.resultOffset, s.colOffset = resultsDescribe, msg.columns, 0, 0
		s.cellRow, s.cellCol = 0, 0
		s.cacheColumnDefs(msg.table, msg.columns) // warm autocomplete (v2 2.5)
		return s, nil
	case describeErrMsg:
		if !s.isCurrentTable(msg.database, msg.table) {
			return s, nil
		}
		s.resLoading, s.resErr = false, msg.err
		return s, nil

	case usersLoadedMsg:
		s.usersLoading, s.usersErr = false, nil
		s.users = msg.users
		s.userCursor = clampCursor(s.userCursor, len(s.users))
		return s, nil
	case usersErrMsg:
		s.usersLoading, s.usersErr = false, msg.err
		return s, nil

	case adminDoneMsg:
		return s.applyAdminDone(msg)
	case adminErrMsg:
		s.working = false
		s.notice, s.noticeErr = adminErrText(msg.err), true
		s.mode = modeBrowse
		return s, nil
	case openCreateDBMsg:
		return s.openCreateDB()

	case editPrepMsg:
		return s.openCellEdit(msg)
	case mutationDoneMsg:
		return s.applyMutationDone(msg)

	case grantsLoadedMsg:
		// Only apply if the matrix is still open on the same user+database.
		if s.mode == modeGrant && msg.user == s.grant.subject && msg.database == s.grant.database {
			s.grant = s.grant.setHeld(msg.held)
		}
		return s, nil
	case grantsErrMsg:
		if s.mode == modeGrant && msg.user == s.grant.subject && msg.database == s.grant.database {
			s.grant = s.grant.setErr(msg.err)
		}
		return s, nil
	case grantSetMsg:
		if s.mode == modeGrant && msg.user == s.grant.subject && msg.database == s.grant.database {
			verb := "Granted"
			if !msg.grant {
				verb = "Revoked"
			}
			s.notice, s.noticeErr = verb+" "+string(msg.priv)+" on "+msg.database+" for "+msg.user, false
			// Refresh the matrix from the server so the checkmarks reflect reality.
			return s, loadGrantsCmd(s.engine, msg.user, msg.database)
		}
		return s, nil

	case queryDoneMsg:
		return s.applyQueryDone(msg)
	case queryErrMsg:
		return s.applyQueryErr(msg)

	case exportDoneMsg:
		s.notice, s.noticeErr = fmt.Sprintf("Exported %d rows to %s", msg.rows, msg.path), false
		return s, nil
	case exportErrMsg:
		s.notice, s.noticeErr = "Export failed: "+msg.err.Error(), true
		return s, nil

	case savedDeletedMsg:
		// A delete from the saved picker resolved: rebuild the overlay from the
		// refreshed list, or close it (with a notice) when nothing is left. Ignore
		// if the overlay has since closed.
		if s.mode != modeSaved {
			return s, nil
		}
		if len(msg.items) == 0 {
			s.mode = modeBrowse
			s.notice, s.noticeErr = "Saved queries are now empty.", false
			return s, nil
		}
		w, h := s.savedOverlaySize()
		s.savedList = newSavedModel(msg.items, w, h)
		return s, nil

	case tea.KeyPressMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

func (s dashboardScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	// The first key dismisses the one-time beginner hint (Step 9.2).
	s.hintDismissed = true
	// An open overlay (form/confirm/grant) captures all keys until it resolves.
	if s.mode != modeBrowse {
		return s.handleOverlayKey(msg)
	}
	// While a statement runs, only cancel is live (letter keys would land in the
	// editor otherwise). The editor pane owns all typing when it has focus.
	if s.querying {
		if key.Matches(msg, Keys.Cancel) {
			return s.cancelQuery()
		}
		return s, nil
	}
	if s.focus == focusEditor {
		return s.handleEditorKey(msg)
	}
	switch {
	case msg.String() == "b", key.Matches(msg, Keys.Back):
		_ = s.engine.Close() // own the connection: release it on the way out
		return s, Pop()
	case key.Matches(msg, Keys.Edit):
		return s.focusEditor()
	case key.Matches(msg, Keys.HistoryList):
		return s.openHistory()
	case key.Matches(msg, Keys.SavedList):
		return s.openSaved()
	case key.Matches(msg, Keys.SaveQuery):
		return s.openSaveQuery()
	case key.Matches(msg, Keys.Export):
		return s.openExport()
	case key.Matches(msg, Keys.CopyCell):
		return s.copyCell()
	case key.Matches(msg, Keys.CopyRow):
		return s.copyRow()
	case key.Matches(msg, Keys.EditCell):
		return s.startCellEdit()
	case key.Matches(msg, Keys.Focus):
		s.focus = s.nextFocus()
		return s, s.syncEditorFocus()
	case key.Matches(msg, Keys.Refresh):
		return s.refresh()
	case key.Matches(msg, Keys.Up):
		s.moveCursor(-1)
		return s, nil
	case key.Matches(msg, Keys.Down):
		s.moveCursor(+1)
		return s, nil
	case msg.String() == "left" || msg.String() == "h":
		s.moveColumn(-1)
		return s, nil
	case msg.String() == "right" || msg.String() == "l":
		s.moveColumn(+1)
		return s, nil
	case key.Matches(msg, Keys.Select):
		return s.selectFocused()
	case key.Matches(msg, Keys.Info):
		return s.describeSelected()
	case key.Matches(msg, Keys.Create):
		return s.openCreate()
	case key.Matches(msg, Keys.Delete):
		return s.openDelete()
	case key.Matches(msg, Keys.Grant):
		return s.openGrant()
	case key.Matches(msg, Keys.EditUser):
		return s.openEditUser()
	}
	return s, nil
}

// focusOrder is the tab cycle for this engine: only the panes it actually offers
// (databases needs MultipleDatabases; users needs Users). SQLite therefore
// cycles tables → editor → results.
func (s dashboardScreen) focusOrder() []focusTarget {
	order := make([]focusTarget, 0, 5)
	if s.caps.MultipleDatabases {
		order = append(order, focusDatabases)
	}
	order = append(order, focusTables)
	if s.caps.Users {
		order = append(order, focusUsers)
	}
	return append(order, focusEditor, focusResults)
}

// nextFocus advances focus through focusOrder, wrapping at the end.
func (s dashboardScreen) nextFocus() focusTarget {
	order := s.focusOrder()
	for i, f := range order {
		if f == s.focus {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

// moveCursor moves the highlighted row within whichever list has focus. In the
// results pane it scrolls the grid.
func (s *dashboardScreen) moveCursor(delta int) {
	switch s.focus {
	case focusDatabases:
		s.dbCursor = wrapCursor(s.dbCursor+delta, len(s.databases))
	case focusTables:
		s.tblCursor = wrapCursor(s.tblCursor+delta, len(s.tables))
	case focusUsers:
		s.userCursor = wrapCursor(s.userCursor+delta, len(s.users))
	case focusResults:
		s.moveCell(delta, 0)
	}
}

// moveColumn moves the results cell cursor left/right (also driving the
// horizontal scroll so wide tables can be read fully). It only acts when the
// results pane holds focus.
func (s *dashboardScreen) moveColumn(delta int) {
	if s.focus != focusResults {
		return
	}
	s.moveCell(0, delta)
}

// selectFocused acts on Enter for the focused pane: switch database (and reload
// its tables) or preview the highlighted table's rows.
func (s dashboardScreen) selectFocused() (dashboardScreen, tea.Cmd) {
	switch s.focus {
	case focusDatabases:
		if s.dbCursor >= len(s.databases) {
			return s, nil
		}
		name := s.databases[s.dbCursor].Name
		if name == s.currentDB {
			s.focus = focusTables // already here — just move into the tables list
			return s, nil
		}
		s.currentDB = name
		s.tables, s.tblCursor, s.tblErr = nil, 0, nil
		s.results, s.resErr = resultsIdle, nil // the old table belongs to the old db
		s.resultsTable = ""
		s.tblLoading = true
		s.focus = focusTables
		return s, tea.Batch(s.spinner.Tick, loadTablesCmd(s.engine, s.currentDB))
	case focusTables:
		t, ok := s.selectedTable()
		if !ok {
			return s, nil
		}
		s.resultsTable, s.resErr, s.resLoading = t.Name, nil, true
		s.focus = focusResults
		return s, tea.Batch(s.spinner.Tick, previewRowsCmd(s.engine, s.currentDB, t.Name))
	case focusResults:
		return s.openCell()
	}
	return s, nil
}

// describeSelected loads the highlighted table's columns into the results pane.
// It works from either the tables or the results pane so [i] is always at hand
// once a table is in view.
func (s dashboardScreen) describeSelected() (dashboardScreen, tea.Cmd) {
	t, ok := s.selectedTable()
	if !ok {
		return s, nil
	}
	s.resultsTable, s.resErr, s.resLoading = t.Name, nil, true
	return s, tea.Batch(s.spinner.Tick, describeTableCmd(s.engine, s.currentDB, t.Name))
}

// refresh reloads the list under the focused pane (or the results) so a manual
// [R] recovers from a transient failure or picks up outside changes.
func (s dashboardScreen) refresh() (dashboardScreen, tea.Cmd) {
	switch s.focus {
	case focusDatabases:
		s.dbLoading, s.dbErr = true, nil
		return s, tea.Batch(s.spinner.Tick, loadDatabasesCmd(s.engine))
	case focusUsers:
		s.usersLoading, s.usersErr = true, nil
		return s, tea.Batch(s.spinner.Tick, loadUsersCmd(s.engine))
	case focusResults:
		if s.resultsTable == "" {
			return s, nil
		}
		s.resLoading, s.resErr = true, nil
		if s.results == resultsDescribe {
			return s, tea.Batch(s.spinner.Tick, describeTableCmd(s.engine, s.currentDB, s.resultsTable))
		}
		return s, tea.Batch(s.spinner.Tick, previewRowsCmd(s.engine, s.currentDB, s.resultsTable))
	default: // tables (and the editor placeholder) refresh the table list
		s.tblLoading, s.tblErr = true, nil
		return s, tea.Batch(s.spinner.Tick, loadTablesCmd(s.engine, s.currentDB))
	}
}

func (s dashboardScreen) Help() []key.Binding {
	// Overlays advertise their own keys.
	switch s.mode {
	case modeForm:
		return s.form.Help()
	case modeConfirm:
		return s.confirm.Help()
	case modeGrant:
		return s.grant.Help()
	case modeCell:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "scroll")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
		}
	case modeHistory:
		return []key.Binding{
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
			Keys.Up, Keys.Down,
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "load")),
			Keys.Back,
		}
	case modeSaved:
		return []key.Binding{
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
			Keys.Up, Keys.Down,
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "load")),
			key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
			Keys.Back,
		}
	case modeExport:
		return []key.Binding{
			Keys.Up, Keys.Down,
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "export")),
			Keys.Back,
		}
	case modeComplete:
		return []key.Binding{
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
			Keys.Up, Keys.Down,
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "insert")),
			Keys.Back,
		}
	case modeEditCell:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "run update")),
			key.NewBinding(key.WithKeys("alt+n"), key.WithHelp("⌥n", "toggle NULL")),
			Keys.Back,
		}
	}
	// A running statement can only be cancelled.
	if s.querying {
		return []key.Binding{Keys.Cancel}
	}
	// The editor advertises its own run/history/saved/complete keys.
	if s.focus == focusEditor {
		return []key.Binding{Keys.Run, Keys.Complete, Keys.History, Keys.HistoryList, Keys.SaveQuery, Keys.SavedList, Keys.Focus, Keys.Back}
	}
	b := []key.Binding{Keys.Focus, Keys.Up, Keys.Down}
	switch s.focus {
	case focusDatabases:
		b = append(b, Keys.Select, Keys.Create, Keys.Delete)
	case focusTables:
		b = append(b, Keys.Select, Keys.Info)
	case focusUsers:
		b = append(b, Keys.Create, Keys.Delete, Keys.Grant, Keys.EditUser)
	case focusResults:
		b = append(b,
			key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "columns")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "inspect cell")),
			Keys.EditCell, Keys.CopyCell, Keys.CopyRow, Keys.Export,
		)
	}
	b = append(b, Keys.Edit, Keys.HistoryList, Keys.SavedList, Keys.Refresh, Keys.Back)
	return b
}

// --- small state helpers ---

func (s dashboardScreen) anyLoading() bool {
	return s.dbLoading || s.tblLoading || s.resLoading || s.usersLoading || s.working || s.querying
}

func (s dashboardScreen) selectedTable() (db.Table, bool) {
	if s.tblCursor < 0 || s.tblCursor >= len(s.tables) {
		return db.Table{}, false
	}
	return s.tables[s.tblCursor], true
}

func (s dashboardScreen) selectedDatabase() (db.Database, bool) {
	if s.dbCursor < 0 || s.dbCursor >= len(s.databases) {
		return db.Database{}, false
	}
	return s.databases[s.dbCursor], true
}

func (s dashboardScreen) selectedUser() (db.User, bool) {
	if s.userCursor < 0 || s.userCursor >= len(s.users) {
		return db.User{}, false
	}
	return s.users[s.userCursor], true
}

// isCurrentTable reports whether a results reply still matches what the user is
// looking at, so a late reply for an abandoned selection is dropped.
func (s dashboardScreen) isCurrentTable(database, table string) bool {
	return database == s.currentDB && table == s.resultsTable
}

func (s dashboardScreen) resultRowCount() int {
	switch s.results {
	case resultsRows:
		return len(s.preview.Rows)
	case resultsDescribe:
		return len(s.columns)
	case resultsQuery:
		return len(s.queryResult.Rows)
	}
	return 0
}

func clampCursor(cursor, n int) int {
	if cursor >= n {
		return max(n-1, 0)
	}
	if cursor < 0 {
		return 0
	}
	return cursor
}

func wrapCursor(next, n int) int {
	if n == 0 {
		return 0
	}
	return (next%n + n) % n
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
