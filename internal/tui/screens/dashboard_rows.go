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

// --- Delete row (v3 2.2) ---------------------------------------------------

// deleteRowState backs the delete-this-row confirm: the generated DELETE plus the
// context needed to run it.
type deleteRowState struct {
	database, table string
	sql             string
}

// deletePrepMsg carries a table's primary key back so the browser can build a
// row-scoped DELETE — or decline when there's no key.
type deletePrepMsg struct {
	database, table string
	cols            []string
	row             []any
	keyCols         []string
	err             *db.DBError
}

// startDeleteRow begins deleting the selected preview row. Like editing, it needs
// a table row preview and learns the primary key first.
func (s dashboardScreen) startDeleteRow() (dashboardScreen, tea.Cmd) {
	if s.focus != focusResults || s.results != resultsRows {
		return s, nil
	}
	cols := s.preview.Columns
	if len(cols) == 0 || s.cellRow >= len(s.preview.Rows) {
		return s, nil
	}
	return s, prepareDeleteCmd(s.engine, s.currentDB, s.resultsTable, cols, s.preview.Rows[s.cellRow])
}

// prepareDeleteCmd reads the table's primary key so the caller can build a
// single-row DELETE.
func prepareDeleteCmd(engine db.Engine, database, table string, cols []string, row []any) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		desc, err := engine.DescribeTable(ctx, database, table)
		if err != nil {
			return deletePrepMsg{err: asDBError(err)}
		}
		return deletePrepMsg{database: database, table: table, cols: cols, row: row, keyCols: primaryKeyCols(desc)}
	}
}

// openDeleteRow opens the confirm once the key is known, or explains why it can't
// (no primary key, or key columns absent from the preview).
func (s dashboardScreen) openDeleteRow(msg deletePrepMsg) (dashboardScreen, tea.Cmd) {
	if msg.err != nil {
		s.notice, s.noticeErr = "Couldn't read the table's columns: "+msg.err.Detail, true
		return s, nil
	}
	if msg.table != s.resultsTable {
		return s, nil
	}
	if len(msg.keyCols) == 0 {
		s.notice, s.noticeErr = "Can't delete: "+msg.table+" has no primary key, so DBWiz can't pin down which row to remove.", true
		return s, nil
	}
	keyVals, ok := keyValues(msg.cols, msg.row, msg.keyCols)
	if !ok {
		s.notice, s.noticeErr = "Can't delete: this table's primary-key columns aren't shown in the preview.", true
		return s, nil
	}
	sql, err := db.BuildDelete(s.engine.Kind(), msg.database, msg.table, msg.keyCols, keyVals)
	if err != nil {
		s.notice, s.noticeErr = "Can't build the delete: "+err.Error(), true
		return s, nil
	}
	s.deleteRow = deleteRowState{database: msg.database, table: msg.table, sql: sql}
	s.mode = modeDeleteRow
	return s, nil
}

// updateDeleteRow handles the confirm: enter deletes, esc cancels.
func (s dashboardScreen) updateDeleteRow(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Back):
		s.mode = modeBrowse
		return s, nil
	case msg.String() == "enter":
		d := s.deleteRow
		s.mode = modeBrowse
		s.working = true
		return s, tea.Batch(s.spinner.Tick, execMutationCmd(s.engine, d.database, d.sql, "Deleted a row from "+d.table))
	}
	return s, nil
}

// deleteRowView renders the confirm in the danger palette, showing the exact
// DELETE that will run.
func (s dashboardScreen) deleteRowView(width int) string {
	inner := clamp(width-8, 20, 100)
	title := styles.ErrorTitle.Render("Delete this row from " + s.deleteRow.table + "?")
	sqlBlock := styles.Hint.Render("Will run:") + "\n" + styles.DangerText.Width(inner).Render(s.deleteRow.sql)
	help := styles.Hint.Render("enter delete · esc cancel")
	return styles.OverlayBox.Render(lipgloss.JoinVertical(lipgloss.Left, title, "", sqlBlock, "", help))
}

// --- Insert row (v3 2.2) ---------------------------------------------------

// insFieldMode is how one insert-form field contributes to the statement: use the
// column's database default (omit it), a typed value, or an explicit NULL.
type insFieldMode int

const (
	insDefault insFieldMode = iota
	insValue
	insNull
)

// insertField is one column's entry in the generated insert form.
type insertField struct {
	col   string
	input textinput.Model
	mode  insFieldMode
}

// insertRowState backs the generated insert form (v3 2.2): one field per column,
// a cursor, and an inline error for a rejected submit.
type insertRowState struct {
	database, table string
	kind            db.Kind
	fields          []insertField
	cursor          int
	err             string
}

// insertPrepMsg carries a table's columns back so the browser can generate its
// insert form.
type insertPrepMsg struct {
	database, table string
	columns         []db.Column
	err             *db.DBError
}

// startInsertRow begins inserting into the table currently in context (the
// selected table, or the one being previewed). It works from the tables or
// results pane so a new row is always one key away once a table is in view.
func (s dashboardScreen) startInsertRow() (dashboardScreen, tea.Cmd) {
	table := s.resultsTable
	if s.focus == focusTables {
		if t, ok := s.selectedTable(); ok {
			table = t.Name
		}
	}
	if table == "" {
		return s, nil
	}
	return s, prepareInsertCmd(s.engine, s.currentDB, table)
}

// prepareInsertCmd reads a table's columns so the caller can build a field per
// column.
func prepareInsertCmd(engine db.Engine, database, table string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		cols, err := engine.DescribeTable(ctx, database, table)
		if err != nil {
			return insertPrepMsg{err: asDBError(err)}
		}
		return insertPrepMsg{database: database, table: table, columns: cols}
	}
}

// openInsertRow builds the form from the table's columns, each starting at its
// database default (so a serial key is left alone unless the user fills it in).
func (s dashboardScreen) openInsertRow(msg insertPrepMsg) (dashboardScreen, tea.Cmd) {
	if msg.err != nil {
		s.notice, s.noticeErr = "Couldn't read the table's columns: "+msg.err.Detail, true
		return s, nil
	}
	if len(msg.columns) == 0 {
		s.notice, s.noticeErr = "Can't insert: "+msg.table+" has no columns.", true
		return s, nil
	}
	fields := make([]insertField, len(msg.columns))
	for i, c := range msg.columns {
		in := textinput.New()
		in.Prompt = "› "
		in.Placeholder = c.Type
		fields[i] = insertField{col: c.Name, input: in}
	}
	var cmd tea.Cmd
	if len(fields) > 0 {
		cmd = fields[0].input.Focus()
	}
	s.insertRow = insertRowState{database: msg.database, table: msg.table, kind: s.engine.Kind(), fields: fields}
	s.mode = modeInsertRow
	return s, cmd
}

// updateInsertRow drives the form: up/down (or tab) move between fields, alt+n
// sets the focused field to NULL, alt+d resets it to the database default, enter
// submits, and any other key edits the focused value.
func (s dashboardScreen) updateInsertRow(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.mode = modeBrowse
		return s, nil
	case "enter":
		return s.runInsertRow()
	case "up", "shift+tab":
		return s, s.moveInsertFocus(-1)
	case "down", "tab":
		return s, s.moveInsertFocus(+1)
	case "alt+n":
		s.insertRow.fields[s.insertRow.cursor].mode = insNull
		return s, nil
	case "alt+d":
		f := &s.insertRow.fields[s.insertRow.cursor]
		f.mode = insDefault
		f.input.SetValue("")
		return s, nil
	}
	f := &s.insertRow.fields[s.insertRow.cursor]
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	if f.input.Value() != "" {
		f.mode = insValue
	} else if f.mode == insValue {
		f.mode = insDefault
	}
	return s, cmd
}

// moveInsertFocus blurs the current field and focuses the next, wrapping.
func (s *dashboardScreen) moveInsertFocus(delta int) tea.Cmd {
	s.insertRow.fields[s.insertRow.cursor].input.Blur()
	s.insertRow.cursor = wrapCursor(s.insertRow.cursor+delta, len(s.insertRow.fields))
	return s.insertRow.fields[s.insertRow.cursor].input.Focus()
}

// runInsertRow collects the provided fields into an INSERT and runs it. Fields
// left at their default are omitted so the database fills them in; at least one
// value is required.
func (s dashboardScreen) runInsertRow() (dashboardScreen, tea.Cmd) {
	var cols []string
	var vals []*string
	for _, f := range s.insertRow.fields {
		switch f.mode {
		case insValue:
			v := f.input.Value()
			cols = append(cols, f.col)
			vals = append(vals, &v)
		case insNull:
			cols = append(cols, f.col)
			vals = append(vals, nil)
		}
	}
	if len(cols) == 0 {
		s.insertRow.err = "Enter at least one value (or NULL) to insert."
		return s, nil
	}
	sql, err := db.BuildInsert(s.insertRow.kind, s.insertRow.database, s.insertRow.table, cols, vals)
	if err != nil {
		s.insertRow.err = err.Error()
		return s, nil
	}
	table := s.insertRow.table
	database := s.insertRow.database
	s.mode = modeBrowse
	s.working = true
	return s, tea.Batch(s.spinner.Tick, execMutationCmd(s.engine, database, sql, "Inserted a row into "+table))
}

// insertRowView renders the form: one row per column showing its current
// contribution (a value, NULL, or the database default), the focused field
// highlighted, plus a live preview of the INSERT.
func (s dashboardScreen) insertRowView(width int) string {
	ins := s.insertRow
	inner := clamp(width-8, 24, 100)

	lines := make([]string, 0, len(ins.fields)+4)
	lines = append(lines, styles.Title.Render("Insert into "+ins.table), "")
	for i, f := range ins.fields {
		marker := "  "
		if i == ins.cursor {
			marker = styles.Selected.Render("▸ ")
		}
		var val string
		switch f.mode {
		case insValue:
			val = f.input.View()
		case insNull:
			val = styles.WarningText.Render("(NULL)")
		default:
			val = styles.Hint.Render("(default)")
		}
		lines = append(lines, marker+styles.Item.Render(f.col)+"  "+val)
	}
	if ins.err != "" {
		lines = append(lines, "", styles.DangerText.Render("✗ "+ins.err))
	}
	if sql, err := s.insertRowSQL(); err == nil {
		lines = append(lines, "", styles.Hint.Render("Will run:"), styles.Selected.Width(inner).Render(sql))
	}
	lines = append(lines, "", styles.Hint.Render("enter insert · ⌥n NULL · ⌥d default · ↑/↓ move · esc cancel"))
	return styles.OverlayBox.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// insertRowSQL builds the INSERT preview from the currently-provided fields, or
// reports no columns so the view can omit the preview until something is entered.
func (s dashboardScreen) insertRowSQL() (string, error) {
	var cols []string
	var vals []*string
	for _, f := range s.insertRow.fields {
		switch f.mode {
		case insValue:
			v := f.input.Value()
			cols = append(cols, f.col)
			vals = append(vals, &v)
		case insNull:
			cols = append(cols, f.col)
			vals = append(vals, nil)
		}
	}
	return db.BuildInsert(s.insertRow.kind, s.insertRow.database, s.insertRow.table, cols, vals)
}
