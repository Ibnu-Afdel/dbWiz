package screens

import (
	"context"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// cellEditState backs the results cell editor (v3 2.1): the value input plus the
// identity of the row being changed — its primary-key columns and their values —
// so DBWiz can generate an UPDATE that touches exactly one row.
type cellEditState struct {
	input    textinput.Model
	database string
	table    string
	column   string   // the column the UPDATE sets
	keyCols  []string // primary-key column names
	keyVals  []any    // their values in the selected row
	kind     db.Kind
	origNull bool // the cell was NULL before editing
	setNull  bool // the pending value is NULL (rather than the input's text)
}

// editPrepMsg carries the primary-key columns discovered for a table (via
// DescribeTable) back to the dashboard so it can open the cell editor — or
// decline, when the table has no primary key. It echoes the cell context it was
// launched with so a grid that changed under the async describe is detected.
type editPrepMsg struct {
	database, table string
	cols            []string
	row             []any
	cellCol         int
	keyCols         []string
	err             *db.DBError
}

// mutationDoneMsg reports a completed data mutation (update/insert/delete/
// truncate — v3 2.x): a notice to show and the rows affected. The dashboard
// reloads the current preview so the grid reflects the change.
type mutationDoneMsg struct {
	affected int64
	notice   string
}

// startCellEdit begins editing the selected cell. It only acts on a table row
// preview (not a describe or an ad-hoc query result), and kicks off an async
// DescribeTable to learn the table's primary key before opening the editor.
func (s dashboardScreen) startCellEdit() (dashboardScreen, tea.Cmd) {
	if s.focus != focusResults || s.results != resultsRows {
		return s, nil
	}
	cols := s.preview.Columns
	if len(cols) == 0 || s.cellRow >= len(s.preview.Rows) || s.cellCol >= len(cols) {
		return s, nil
	}
	return s, prepareEditCmd(s.engine, s.currentDB, s.resultsTable, cols, s.preview.Rows[s.cellRow], s.cellCol)
}

// prepareEditCmd reads the table's columns and extracts its primary-key column
// names (Key == "PRI" across all engines) so the caller can build a row-scoped
// UPDATE. Run off the Update goroutine like every other DB read.
func prepareEditCmd(engine db.Engine, database, table string, cols []string, row []any, cellCol int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		desc, err := engine.DescribeTable(ctx, database, table)
		if err != nil {
			return editPrepMsg{err: asDBError(err)}
		}
		return editPrepMsg{database: database, table: table, cols: cols, row: row, cellCol: cellCol, keyCols: primaryKeyCols(desc)}
	}
}

// openCellEdit opens the editor once the primary key is known, or explains why it
// can't: no primary key (nothing identifies the row), or the key columns aren't in
// the preview. A stale reply (the grid moved on) is dropped.
func (s dashboardScreen) openCellEdit(msg editPrepMsg) (dashboardScreen, tea.Cmd) {
	if msg.err != nil {
		s.notice, s.noticeErr = "Couldn't read the table's columns: "+msg.err.Detail, true
		return s, nil
	}
	if msg.table != s.resultsTable || msg.cellCol >= len(msg.cols) {
		return s, nil // the grid changed under the describe
	}
	if len(msg.keyCols) == 0 {
		s.notice, s.noticeErr = "Can't edit: "+msg.table+" has no primary key, so DBWiz can't pin down which row to change.", true
		return s, nil
	}
	keyVals, ok := keyValues(msg.cols, msg.row, msg.keyCols)
	if !ok {
		s.notice, s.noticeErr = "Can't edit: this table's primary-key columns aren't shown in the preview.", true
		return s, nil
	}

	cur := msg.row[msg.cellCol]
	in := textinput.New()
	in.Prompt = "› "
	if cur != nil {
		in.SetValue(cellText(cur))
	}
	in.Focus()
	in.CursorEnd()

	s.cellEdit = cellEditState{
		input:    in,
		database: msg.database,
		table:    msg.table,
		column:   msg.cols[msg.cellCol],
		keyCols:  msg.keyCols,
		keyVals:  keyVals,
		kind:     s.engine.Kind(),
		origNull: cur == nil,
		setNull:  cur == nil,
	}
	s.mode = modeEditCell
	return s, nil
}

// updateCellEdit handles keys while the cell editor is open: esc cancels, enter
// runs the generated UPDATE, alt+n toggles the NULL choice, and anything else
// edits the value (which clears the NULL choice).
func (s dashboardScreen) updateCellEdit(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Back):
		s.mode = modeBrowse
		return s, nil
	case msg.String() == "alt+n":
		s.cellEdit.setNull = !s.cellEdit.setNull
		return s, nil
	case msg.String() == "enter":
		return s.runCellEdit()
	}
	var cmd tea.Cmd
	s.cellEdit.input, cmd = s.cellEdit.input.Update(msg)
	s.cellEdit.setNull = false // typing a value overrides the NULL choice
	return s, cmd
}

// runCellEdit builds the UPDATE from the current input and runs it. The exact SQL
// was on screen (the editor renders it live), so this is the "shown before
// execute" step completing.
func (s dashboardScreen) runCellEdit() (dashboardScreen, tea.Cmd) {
	sql, err := s.cellEdit.sql()
	if err != nil {
		s.mode = modeBrowse
		s.notice, s.noticeErr = "Can't build the update: "+err.Error(), true
		return s, nil
	}
	ce := s.cellEdit
	s.mode = modeBrowse
	s.working = true
	notice := "Updated " + ce.column + " in " + ce.table
	return s, tea.Batch(s.spinner.Tick, execMutationCmd(s.engine, ce.database, sql, notice))
}

// sql renders the UPDATE for the pending value (NULL or the input's text).
func (ce cellEditState) sql() (string, error) {
	var newVal *string
	if !ce.setNull {
		v := ce.input.Value()
		newVal = &v
	}
	return db.BuildUpdate(ce.kind, ce.database, ce.table, ce.keyCols, ce.keyVals, ce.column, newVal)
}

// execMutationCmd runs a pre-built data-modifying statement against database off
// the Update goroutine, reporting the rows affected (or a typed error) back. It's
// shared by every v3 2.x write action.
func execMutationCmd(engine db.Engine, database, sql, notice string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		res, err := engine.ExecMutation(ctx, database, sql)
		if err != nil {
			return adminErrMsg{err: asDBError(err)}
		}
		return mutationDoneMsg{affected: res.RowsAffected, notice: notice}
	}
}

// applyMutationDone shows the result of a write and reloads the current row
// preview so the grid reflects the change (a describe or query result is left as
// is — the write targeted a table's rows).
func (s dashboardScreen) applyMutationDone(msg mutationDoneMsg) (dashboardScreen, tea.Cmd) {
	s.working = false
	notice := msg.notice
	if notice == "" {
		notice = "Done"
	}
	s.notice, s.noticeErr = notice, false
	if s.resultsTable != "" && s.results == resultsRows {
		s.resLoading, s.resErr = true, nil
		return s, tea.Batch(s.spinner.Tick, previewRowsCmd(s.engine, s.currentDB, s.resultsTable))
	}
	return s, nil
}

// cellEditView renders the editor overlay: the target column, the value input (or
// a NULL indicator), and — crucially — the exact UPDATE that will run, shown
// before the user commits to it (v3 2.1).
func (s dashboardScreen) cellEditView(width int) string {
	ce := s.cellEdit
	inner := clamp(width-8, 20, 100)

	title := styles.Title.Render("Edit " + ce.table + "." + ce.column)
	var valueLine string
	if ce.setNull {
		valueLine = styles.WarningText.Render("(NULL)") + styles.Hint.Render("  — type a value to replace it")
	} else {
		valueLine = ce.input.View()
	}

	sql, err := ce.sql()
	shown := sql
	if err != nil {
		shown = err.Error()
	}
	sqlBlock := styles.Hint.Render("Will run:") + "\n" + styles.Selected.Width(inner).Render(shown)

	help := styles.Hint.Render("enter run · ⌥n toggle NULL · esc cancel")
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", valueLine, "", sqlBlock, "", help)
	return styles.OverlayBox.Render(body)
}

// primaryKeyCols returns the primary-key column names from a described table.
// All three engines label PK columns "PRI" in DescribeTable, so this is the one
// place the row-mutation flows (edit/delete) learn a table's identity.
func primaryKeyCols(desc []db.Column) []string {
	var out []string
	for _, c := range desc {
		if c.Key == "PRI" {
			out = append(out, c.Name)
		}
	}
	return out
}

// keyValues pulls the values of keyCols out of a row, matching by the preview's
// column names. It reports ok=false if any key column isn't present in the
// preview, so the caller declines rather than build a bad WHERE.
func keyValues(cols []string, row []any, keyCols []string) ([]any, bool) {
	idx := make(map[string]int, len(cols))
	for i, c := range cols {
		idx[c] = i
	}
	out := make([]any, len(keyCols))
	for i, k := range keyCols {
		j, ok := idx[k]
		if !ok || j >= len(row) {
			return nil, false
		}
		out[i] = row[j]
	}
	return out, true
}
