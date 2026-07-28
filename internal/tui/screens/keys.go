package screens

import "charm.land/bubbles/v2/key"

// KeyMap is the central keymap for the whole app. Every screen draws the
// bindings it shows in the help bar from this one value, so the help text and
// the actual handlers never drift apart. Screens expose the subset relevant to
// their focus via their Help method; the root renders it with bubbles/help.
type KeyMap struct {
	// Global.
	Quit key.Binding
	Help key.Binding

	// Tabs (v2 Phase 1) — multiple targets open at once. SwitchTab jumps to a tab
	// by number; NewTab opens a fresh detection tab; CloseTab closes the active
	// one.
	NewTab    key.Binding
	SwitchTab key.Binding
	CloseTab  key.Binding

	// Navigation shared across screens.
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Back   key.Binding

	// Home-menu / detection actions.
	Rescan key.Binding
	Start  key.Binding

	// Error-screen actions.
	Retry key.Binding
	Info  key.Binding

	// Dashboard / browser actions.
	Focus   key.Binding // cycle focus between panes
	Refresh key.Binding // reload the focused list

	// Admin actions.
	Create   key.Binding // create a database/user (by focused section)
	Delete   key.Binding // delete the selected database/user (type-to-confirm)
	Grant    key.Binding // grant/revoke a user on a database
	EditUser key.Binding // edit the selected user's flags/password (v2 3.2)

	// Query pillar.
	Edit    key.Binding // focus the SQL editor from anywhere on the dashboard
	Run     key.Binding // execute the statement in the editor
	Cancel  key.Binding // cancel a running query
	History key.Binding // cycle previous statements into the editor
	// HistoryList opens the searchable/fuzzy-filtered per-target history overlay
	// (v2 2.1). Distinct from History's blind ctrl+p/ctrl+n cycle.
	HistoryList key.Binding
	// SaveQuery names the editor's statement as a saved/favourite query; SavedList
	// opens the searchable saved-query picker (v2 2.2).
	SaveQuery key.Binding
	SavedList key.Binding

	// Results export / clipboard (v2 2.3). Export opens the CSV/JSON file chooser;
	// CopyCell / CopyRow yank the selected cell or row to the clipboard (OSC 52).
	Export   key.Binding
	CopyCell key.Binding
	CopyRow  key.Binding

	// Complete opens schema-aware autocomplete for the word under the cursor in the
	// SQL editor (v2 2.5).
	Complete key.Binding

	// EditCell opens the results cell editor, which generates and runs an UPDATE
	// for the selected cell (v3 2.1). It fires only in the results pane, so a plain
	// letter is safe.
	EditCell key.Binding

	// InsertRow opens a generated insert form for the current table; DeleteRow
	// removes the selected preview row after a confirm (v3 2.2). Plain letters,
	// safe because they fire only in the browse panes.
	InsertRow key.Binding
	DeleteRow key.Binding

	// Table-level actions (v3 2.3): Truncate empties the table (confirm), RowCount
	// runs an exact COUNT(*), Filter opens a quick WHERE bar over the preview.
	Truncate key.Binding
	RowCount key.Binding
	Filter   key.Binding

	// Backup opens the dump/restore screen for the selected database (v3 2.4).
	Backup key.Binding
}

// Keys is the single instance every screen and the root model share.
var Keys = KeyMap{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	// Tab keys are chosen to survive a multiplexer: ctrl+letter combos and plain
	// digits pass through tmux untouched, unlike ctrl+tab or alt+digit (which
	// need the extended-keys protocol tmux doesn't forward). New/close use
	// ctrl+t / ctrl+w; switching is by the tab's number (1–9), which the root
	// only claims when several tabs are open and the active screen isn't taking
	// text input — so a digit still types into the SQL editor or a path field.
	NewTab: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("^t", "new tab"),
	),
	SwitchTab: key.NewBinding(
		key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"),
		key.WithHelp("1-9", "switch tab"),
	),
	CloseTab: key.NewBinding(
		key.WithKeys("ctrl+w"),
		key.WithHelp("^w", "close tab"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Rescan: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "rescan"),
	),
	Start: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "start"),
	),
	Retry: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "retry"),
	),
	Info: key.NewBinding(
		key.WithKeys("i"),
		key.WithHelp("i", "details"),
	),
	Focus: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "focus"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "refresh"),
	),
	Create: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "create"),
	),
	Delete: key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "delete"),
	),
	Grant: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "grant"),
	),
	EditUser: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "edit user"),
	),
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit SQL"),
	),
	// Run has several bindings on purpose: ctrl+enter is ideal on terminals that
	// disambiguate it, but under tmux/multiplexers without extended-keys it
	// collapses to a plain Enter (a newline), so ctrl+r and alt+enter are the
	// reliable fallbacks that always survive, with F5 for good measure.
	Run: key.NewBinding(
		key.WithKeys("ctrl+enter", "ctrl+r", "alt+enter", "f5"),
		key.WithHelp("^R/F5", "run"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	History: key.NewBinding(
		key.WithKeys("ctrl+p", "ctrl+n"),
		key.WithHelp("^p/^n", "history"),
	),
	// alt+h so it survives being pressed mid-typing in the SQL editor (a plain
	// letter would just insert text); it opens the searchable history list.
	HistoryList: key.NewBinding(
		key.WithKeys("alt+h"),
		key.WithHelp("⌥h", "search history"),
	),
	// alt+w / alt+s mirror alt+h: modifier combos so they work while typing in the
	// editor. alt+w ("write") saves the current statement; alt+s opens the saved
	// list. ctrl+s is deliberately avoided — many terminals read it as XOFF and
	// freeze the display.
	SaveQuery: key.NewBinding(
		key.WithKeys("alt+w"),
		key.WithHelp("⌥w", "save query"),
	),
	SavedList: key.NewBinding(
		key.WithKeys("alt+s"),
		key.WithHelp("⌥s", "saved queries"),
	),
	// alt+e opens the export chooser from anywhere (modifier combo so it's reachable
	// even while the editor has focus). y/Y yank the selected cell/row; they're
	// plain letters because they only fire in the results pane, never while typing.
	Export: key.NewBinding(
		key.WithKeys("alt+e"),
		key.WithHelp("⌥e", "export"),
	),
	CopyCell: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "copy cell"),
	),
	CopyRow: key.NewBinding(
		key.WithKeys("Y"),
		key.WithHelp("Y", "copy row"),
	),
	// ctrl+space is the conventional completion trigger and survives multiplexers;
	// it can't clash with typed text or a vim normal-mode letter.
	Complete: key.NewBinding(
		key.WithKeys("ctrl+space"),
		key.WithHelp("^space", "autocomplete"),
	),
	// u edits the selected cell — a plain letter, safe because it only fires in the
	// results pane where no free text is being typed.
	EditCell: key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "edit cell"),
	),
	// n inserts a new row, x deletes the selected one — plain letters that fire
	// only in the browse panes (never while typing).
	InsertRow: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "insert row"),
	),
	DeleteRow: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "delete row"),
	),
	// T truncates, # counts, / filters — table-level actions in the browse panes.
	Truncate: key.NewBinding(
		key.WithKeys("T"),
		key.WithHelp("T", "truncate"),
	),
	RowCount: key.NewBinding(
		key.WithKeys("#"),
		key.WithHelp("#", "row count"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	// B opens dump/restore for the selected database.
	Backup: key.NewBinding(
		key.WithKeys("B"),
		key.WithHelp("B", "dump/restore"),
	),
}
