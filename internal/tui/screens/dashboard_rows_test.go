package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// TestDeleteRowFlow covers v3 2.2: [x] on a preview row finds the primary key,
// shows the generated DELETE to confirm, and enter runs it.
func TestDeleteRowFlow(t *testing.T) {
	s, eng := previewing(t)

	s, cmd := press(s, tea.KeyPressMsg{Code: 'x', Text: "x"})
	msg := runCmd(t, cmd)
	prep, ok := msg.(deletePrepMsg)
	if !ok {
		t.Fatalf("want deletePrepMsg, got %T", msg)
	}
	s = feed(s, prep)
	if s.mode != modeConfirmSQL {
		t.Fatalf("delete confirm should open, mode=%d", s.mode)
	}
	if s.confirmSQL.sql != `DELETE FROM "t" WHERE "id" = '1'` {
		t.Fatalf("generated DELETE: %s", s.confirmSQL.sql)
	}
	if !strings.Contains(s.View(120, 40), `DELETE FROM "t"`) {
		t.Error("the DELETE should be shown before it runs")
	}

	s2, _ := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s2.mode != modeBrowse || !s2.working {
		t.Fatalf("enter should run and return to browse; mode=%d working=%v", s2.mode, s2.working)
	}
	runCmd(t, execMutationCmd(eng, "postgres", s.confirmSQL.sql, "note"))
	if eng.lastMutationSQL != `DELETE FROM "t" WHERE "id" = '1'` {
		t.Errorf("executed SQL: %s", eng.lastMutationSQL)
	}
}

// TestDeleteRowNoPrimaryKeyRefuses covers the graceful refusal.
func TestDeleteRowNoPrimaryKeyRefuses(t *testing.T) {
	s, eng := previewing(t)
	eng.columns = []db.Column{{Name: "id"}, {Name: "name"}} // no PRI

	s, cmd := press(s, tea.KeyPressMsg{Code: 'x', Text: "x"})
	s = feed(s, runCmd(t, cmd))
	if s.mode == modeConfirmSQL {
		t.Fatal("delete must not proceed without a primary key")
	}
	if !s.noticeErr || !strings.Contains(s.notice, "no primary key") {
		t.Errorf("expected a no-primary-key notice, got %q", s.notice)
	}
}

// TestInsertRowFlow covers the generated insert form: only the columns the user
// fills in are named (so a serial key takes its default), and enter runs the
// INSERT.
func TestInsertRowFlow(t *testing.T) {
	s, eng := previewing(t)

	s, cmd := press(s, tea.KeyPressMsg{Code: 'n', Text: "n"})
	msg := runCmd(t, cmd)
	prep, ok := msg.(insertPrepMsg)
	if !ok {
		t.Fatalf("want insertPrepMsg, got %T", msg)
	}
	s = feed(s, prep)
	if s.mode != modeInsertRow || len(s.insertRow.fields) != 2 {
		t.Fatalf("insert form should open with a field per column; mode=%d fields=%d", s.mode, len(s.insertRow.fields))
	}

	// Move to the "name" field (skip the serial "id") and type a value.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown})
	s = typeInto(s, "widget")

	sql, err := s.insertRowSQL()
	if err != nil {
		t.Fatalf("insertRowSQL: %v", err)
	}
	if sql != `INSERT INTO "t" ("name") VALUES ('widget')` {
		t.Fatalf("generated INSERT: %s", sql)
	}

	s2, _ := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s2.mode != modeBrowse || !s2.working {
		t.Fatalf("enter should run and return to browse; mode=%d working=%v", s2.mode, s2.working)
	}
	runCmd(t, execMutationCmd(eng, "postgres", sql, "note"))
	if eng.lastMutationSQL != sql {
		t.Errorf("executed SQL: %s", eng.lastMutationSQL)
	}
}

// TestInsertRowNeedsAValue covers the empty-submit guard.
func TestInsertRowNeedsAValue(t *testing.T) {
	s, _ := previewing(t)
	s, cmd := press(s, tea.KeyPressMsg{Code: 'n', Text: "n"})
	s = feed(s, runCmd(t, cmd))

	// Submit with every field still at its default.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.mode != modeInsertRow {
		t.Fatal("an empty insert should stay on the form, not submit")
	}
	if !strings.Contains(s.insertRow.err, "at least one value") {
		t.Errorf("expected an inline error, got %q", s.insertRow.err)
	}
}
