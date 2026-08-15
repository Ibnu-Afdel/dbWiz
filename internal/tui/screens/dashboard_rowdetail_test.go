package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// viewRowKey opens the row-detail overlay (v5 1.2).
var viewRowKey = tea.KeyPressMsg{Code: 'v', Text: "v"}

// TestRowDetailShowsEveryColumn covers 5.2: v on a results row opens every
// column at full value — the answer to a table too wide to read across the grid,
// which truncates each cell and hides the rest behind horizontal scroll.
func TestRowDetailShowsEveryColumn(t *testing.T) {
	s, _ := newPGDashboard(t)
	long := strings.Repeat("lorem ipsum dolor sit amet ", 20)
	s.results = resultsQuery
	s.queryResult = db.Result{
		Columns: []string{"id", "description"},
		Rows:    [][]any{{"1", long}},
	}
	s.focus = focusResults
	s = sized(s)

	s, _ = press(s, viewRowKey)
	if s.mode != modeRowDetail {
		t.Fatalf("v should open the row-detail overlay, mode = %d", s.mode)
	}
	view := s.View(120, 40)
	for _, want := range []string{"id", "1", "description", "lorem ipsum"} {
		if !strings.Contains(view, want) {
			t.Errorf("row detail missing %q\n%s", want, view)
		}
	}
	if !strings.Contains(view, "row 1 of 1") {
		t.Errorf("row detail should say which row it's showing:\n%s", view)
	}
}

// TestRowDetailNullField covers 5.2: a NULL column reads as NULL, not a blank
// line — the same convention the grid and the single-cell overlay already use.
func TestRowDetailNullField(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.results = resultsQuery
	s.queryResult = db.Result{Columns: []string{"note"}, Rows: [][]any{{nil}}}
	s.focus = focusResults
	s = sized(s)

	s, _ = press(s, viewRowKey)
	if !strings.Contains(s.View(120, 40), "NULL") {
		t.Error("a NULL column should show NULL in the row-detail overlay")
	}
}

// TestRowDetailEsc covers 5.2: esc returns to the browser.
func TestRowDetailEsc(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.results = resultsQuery
	s.queryResult = db.Result{Columns: []string{"id"}, Rows: [][]any{{"1"}}}
	s.focus = focusResults
	s = sized(s)

	s, _ = press(s, viewRowKey)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc})
	if s.mode != modeBrowse {
		t.Errorf("esc should close the row-detail overlay, mode = %d", s.mode)
	}
}

// TestRowDetailOnlyFromResults covers 5.2's scope: v only opens the overlay from
// the results pane — elsewhere it's a silent no-op, matching Find's shape.
func TestRowDetailOnlyFromResults(t *testing.T) {
	s, _ := newPGDashboard(t) // focus starts on databases
	s, _ = press(s, viewRowKey)
	if s.mode != modeBrowse {
		t.Errorf("v outside the results pane should be a no-op, mode = %d", s.mode)
	}
}
