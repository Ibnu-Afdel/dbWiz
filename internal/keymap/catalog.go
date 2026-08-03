package keymap

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
)

// Entry is one key in the catalogue: the binding itself, plus a sentence saying
// what it does and — where it matters — *where* it works. Several keys are
// deliberately reused in places that can't overlap (y copies a cell in the
// results grid and yanks a URL on the databases pane), so the sentence is what
// makes that legible rather than confusing.
type Entry struct {
	Binding key.Binding
	About   string
}

// Key is the key column as shown in help ("⌥p", "^R/F5").
func (e Entry) Key() string { return e.Binding.Help().Key }

// Label is the short description the footer uses ("query plan").
func (e Entry) Label() string { return e.Binding.Help().Desc }

// Group is a titled section of the catalogue.
type Group struct {
	Title   string
	Entries []Entry
}

// Groups is the full catalogue, in the order a newcomer meets the app: the keys
// that always work, then moving around, then each pillar.
//
// This is the single source for the F1 overlay and for `dbwiz keys`; both read
// the same bindings the handlers match on, so a key can't be documented as
// something it isn't.
func Groups() []Group {
	return []Group{
		{
			Title: "Always available",
			Entries: []Entry{
				{Keys.Help, "Expand the bottom bar to the keys that work right here."},
				{Keys.KeyList, "Open this list of every key."},
				{Keys.Quit, "Quit DBWiz from anywhere, including mid-operation."},
				{Keys.NewTab, "Open another tab to work on a second target at once."},
				{Keys.CloseTab, "Close the current tab, releasing its connection. Closing the last one quits."},
				{Keys.SwitchTab, "Jump to a tab by number (only once more than one is open)."},
			},
		},
		{
			Title: "Moving around",
			Entries: []Entry{
				{Keys.Up, "Move the selection up."},
				{Keys.Down, "Move the selection down."},
				{Keys.Select, "Open what's selected — a database, a table, a menu row."},
				{Keys.Back, "Go back one step. In the SQL editor it returns to the panes instead."},
				{Keys.Focus, "Cycle focus between the dashboard's panes."},
				{Keys.Refresh, "Reload the focused list from the server."},
			},
		},
		{
			Title: "Finding a target",
			Entries: []Entry{
				{Keys.Rescan, "On the home screen: scan Docker again for database containers."},
				{Keys.Start, "On the home screen: start the selected stopped container."},
				{Keys.Retry, "On an error screen: retry whatever just failed."},
				{Keys.Info, "On an error screen: show the details behind it. On the tables pane: describe the selected table."},
			},
		},
		{
			Title: "Browsing data",
			Entries: []Entry{
				{Keys.Filter, "Add a WHERE condition to the preview without writing a query."},
				{Keys.RowCount, "Count the table's rows exactly, rather than the stored estimate."},
				{Keys.CopyCell, "On the results pane: copy the selected cell to the clipboard (works over SSH)."},
				{Keys.CopyRow, "On the results pane: copy the whole selected row."},
				{Keys.Export, "Export the current result to a CSV or JSON file."},
			},
		},
		{
			Title: "Changing data",
			Entries: []Entry{
				{Keys.EditCell, "Edit the selected cell; the UPDATE is shown before it runs."},
				{Keys.InsertRow, "Add a row through a generated form, one field per column."},
				{Keys.DeleteRow, "Delete the selected row, after showing you the DELETE."},
				{Keys.Truncate, "Empty the table completely. Asks for confirmation first."},
			},
		},
		{
			Title: "Writing SQL",
			Entries: []Entry{
				{Keys.Edit, "Jump into the SQL editor from anywhere on the dashboard."},
				{Keys.Run, "Run the statement in the editor."},
				{Keys.Cancel, "While a statement runs: cancel it, server-side."},
				{Keys.Complete, "Complete a table or column name from what browsing already loaded."},
				{Keys.History, "Step back and forward through statements you've run."},
				{Keys.HistoryList, "Search your history for this target."},
				{Keys.SaveQuery, "Save the current statement under a name."},
				{Keys.SavedList, "Open your saved queries."},
			},
		},
		{
			Title: "Databases and users",
			Entries: []Entry{
				{Keys.Create, "Create a database or a user, depending on which pane has focus."},
				{Keys.Delete, "Drop the selected database or user. You have to type its name to confirm."},
				{Keys.Grant, "Grant or revoke a user's permissions on a database."},
				{Keys.EditUser, "Change the selected user's flags or password."},
				{Keys.YankURL, "On the databases pane: copy a ready-to-paste connection URL — same key as copy-cell, and the focused pane decides which one happens."},
				{Keys.Backup, "Dump the selected database to a file, or restore one into it."},
				{Keys.SchemaDiff, "Compare this database's structure with another on the same server."},
			},
		},
	}
}

// Render prints the catalogue as plain aligned text — what `dbwiz keys` writes.
// It stays unstyled so it survives being piped into a file or a pager.
func Render() string {
	var b strings.Builder
	b.WriteString("DBWiz keys\n")

	width := 0
	for _, g := range Groups() {
		for _, e := range g.Entries {
			width = max(width, len(e.Key()))
		}
	}

	for _, g := range Groups() {
		fmt.Fprintf(&b, "\n%s\n", g.Title)
		for _, e := range g.Entries {
			fmt.Fprintf(&b, "  %-*s  %s\n", width, e.Key(), e.About)
		}
	}

	b.WriteString("\nInside DBWiz, ? expands the bottom bar to just the keys that work where you\n")
	b.WriteString("are, and F1 shows this same list.\n")
	return b.String()
}
