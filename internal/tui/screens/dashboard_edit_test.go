package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// previewing returns a pg dashboard sitting on a row preview of table "t" with a
// primary key on "id", the results pane focused and the "name" cell selected.
func previewing(t *testing.T) (dashboardScreen, *fakeEngine) {
	t.Helper()
	s, eng := newPGDashboard(t)
	eng.preview = db.Result{Columns: []string{"id", "name"}, Rows: [][]any{{"1", "alice"}}}
	eng.columns = []db.Column{{Name: "id", Key: "PRI"}, {Name: "name"}}
	s.focus = focusResults
	s.results = resultsRows
	s.resultsTable = "t"
	s.currentDB = "postgres"
	s.preview = eng.preview
	s.cellRow, s.cellCol = 0, 1 // the "name" cell
	return s, eng
}

// TestCellEditOpensAndBuildsUpdate covers v3 2.1: [u] on a cell finds the primary
// key, opens the editor prefilled with the current value, and generates the
// row-scoped UPDATE shown before it runs.
func TestCellEditOpensAndBuildsUpdate(t *testing.T) {
	s, eng := previewing(t)

	s, cmd := press(s, tea.KeyPressMsg{Code: 'u', Text: "u"})
	msg := runCmd(t, cmd) // prepareEditCmd → editPrepMsg
	prep, ok := msg.(editPrepMsg)
	if !ok {
		t.Fatalf("want editPrepMsg, got %T", msg)
	}
	if len(prep.keyCols) != 1 || prep.keyCols[0] != "id" {
		t.Fatalf("primary key not detected: %+v", prep.keyCols)
	}

	s = feed(s, prep)
	if s.mode != modeEditCell {
		t.Fatalf("editor should open, mode=%d", s.mode)
	}
	if s.cellEdit.input.Value() != "alice" {
		t.Errorf("input should prefill with the current value, got %q", s.cellEdit.input.Value())
	}
	sql, err := s.cellEdit.sql()
	if err != nil {
		t.Fatalf("sql: %v", err)
	}
	want := `UPDATE "t" SET "name" = 'alice' WHERE "id" = '1'`
	if sql != want {
		t.Fatalf("generated UPDATE:\n got %s\nwant %s", sql, want)
	}
	if !strings.Contains(s.View(120, 40), want) {
		t.Error("the UPDATE should be shown in the overlay before running")
	}
	_ = eng
}

// TestCellEditRunsMutation covers the execute half: entering a new value and
// pressing enter runs the UPDATE via ExecMutation against the right database.
func TestCellEditRunsMutation(t *testing.T) {
	s, eng := previewing(t)
	s, cmd := press(s, tea.KeyPressMsg{Code: 'u', Text: "u"})
	s = feed(s, runCmd(t, cmd))

	s.cellEdit.input.SetValue("bob")
	sql, _ := s.cellEdit.sql()

	// Enter returns to browse with a mutation in flight.
	s2, _ := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s2.mode != modeBrowse || !s2.working {
		t.Fatalf("enter should run and return to browse; mode=%d working=%v", s2.mode, s2.working)
	}

	// The mutation command itself hits ExecMutation with the generated SQL.
	done := runCmd(t, execMutationCmd(eng, "postgres", sql, "Updated name in t"))
	if _, ok := done.(mutationDoneMsg); !ok {
		t.Fatalf("want mutationDoneMsg, got %T", done)
	}
	if eng.lastMutationDB != "postgres" {
		t.Errorf("mutation ran against %q, want postgres", eng.lastMutationDB)
	}
	if eng.lastMutationSQL != `UPDATE "t" SET "name" = 'bob' WHERE "id" = '1'` {
		t.Errorf("executed SQL: %s", eng.lastMutationSQL)
	}
}

// TestCellEditNullToggle covers setting a cell to NULL.
func TestCellEditNullToggle(t *testing.T) {
	s, _ := previewing(t)
	s, cmd := press(s, tea.KeyPressMsg{Code: 'u', Text: "u"})
	s = feed(s, runCmd(t, cmd))

	s.cellEdit.setNull = true
	sql, _ := s.cellEdit.sql()
	if !strings.Contains(sql, `SET "name" = NULL`) {
		t.Fatalf("NULL toggle should render the NULL keyword, got: %s", sql)
	}
}

// TestCellEditNoPrimaryKeyRefuses covers the graceful refusal: a table with no
// primary key can't be edited, and nothing is mutated.
func TestCellEditNoPrimaryKeyRefuses(t *testing.T) {
	s, eng := previewing(t)
	eng.columns = []db.Column{{Name: "id"}, {Name: "name"}} // no PRI

	s, cmd := press(s, tea.KeyPressMsg{Code: 'u', Text: "u"})
	s = feed(s, runCmd(t, cmd))

	if s.mode == modeEditCell {
		t.Fatal("editor must not open without a primary key")
	}
	if !s.noticeErr || !strings.Contains(s.notice, "no primary key") {
		t.Errorf("expected a no-primary-key notice, got %q (err=%v)", s.notice, s.noticeErr)
	}
	if eng.lastMutationSQL != "" {
		t.Error("nothing should have been mutated")
	}
}
