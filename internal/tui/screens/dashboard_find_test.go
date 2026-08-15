package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// findKey is the find-and-jump open key (v5 1.1).
var findKey = tea.KeyPressMsg{Code: 'f', Text: "f"}

// TestFindOpensOverFocusedDatabases covers 5.1: f on the databases pane opens a
// jump list of the databases currently loaded there.
func TestFindOpensOverFocusedDatabases(t *testing.T) {
	s, _ := newPGDashboard(t) // starts focused on databases

	s, _ = press(s, findKey)
	if s.mode != modeFind {
		t.Fatalf("f should open the find overlay, mode = %d", s.mode)
	}
	view := s.View(120, 40)
	for _, want := range []string{"Find a database", "postgres", "appdb"} {
		if !strings.Contains(view, want) {
			t.Errorf("find overlay missing %q\n%s", want, view)
		}
	}
}

// TestFindPickSwitchesDatabase covers 5.1's whole point: picking a name from the
// jump list does exactly what Enter on it in the navigator would — here, switch
// the current database and move focus to its tables, without hand-scrolling.
func TestFindPickSwitchesDatabase(t *testing.T) {
	s, _ := newPGDashboard(t) // currentDB = postgres

	s, _ = press(s, findKey)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown}) // postgres -> appdb
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})

	if s.mode != modeBrowse {
		t.Fatalf("picking should close the overlay, mode = %d", s.mode)
	}
	if s.currentDB != "appdb" {
		t.Errorf("currentDB = %q, want appdb", s.currentDB)
	}
	if s.focus != focusTables {
		t.Errorf("focus after pick = %d, want focusTables", s.focus)
	}
	if cmd == nil {
		t.Error("switching database should reload its tables")
	}
}

// TestFindOverTables covers 5.1 on the tables pane: opening it lists the current
// database's tables, and picking one previews it — the "hundreds of tables"
// complaint this exists for.
func TestFindOverTables(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyTab}) // databases -> tables

	s, _ = press(s, findKey)
	if !strings.Contains(s.View(120, 40), "pg_stat") {
		t.Fatalf("find-tables overlay should list the current database's tables:\n%s", s.View(120, 40))
	}
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})

	if s.mode != modeBrowse || s.focus != focusResults {
		t.Fatalf("picking a table should preview it, mode=%d focus=%d", s.mode, s.focus)
	}
	if s.resultsTable != "pg_stat" {
		t.Errorf("resultsTable = %q, want pg_stat", s.resultsTable)
	}
	if cmd == nil {
		t.Error("picking a table should kick off its preview load")
	}
}

// TestFindEscCancels covers 5.1: esc closes the overlay without moving the
// cursor it started from.
func TestFindEscCancels(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = press(s, findKey)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc})

	if s.mode != modeBrowse {
		t.Errorf("esc should close the overlay, mode = %d", s.mode)
	}
	if s.currentDB != "postgres" {
		t.Errorf("esc should not have changed currentDB, got %q", s.currentDB)
	}
}

// TestFindOnResultsIsNoop covers 5.1's scope: f only opens over a navigator
// list (databases/tables/users) — on the results pane it's a silent no-op rather
// than an empty overlay.
func TestFindOnResultsIsNoop(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.focus = focusResults
	s = sized(s)

	s, _ = press(s, findKey)
	if s.mode != modeBrowse {
		t.Errorf("f on the results pane should be a no-op, mode = %d", s.mode)
	}
}

// TestFindCapturesTextOnlyWhileFiltering mirrors the history overlay: the find
// list shouldn't swallow digits (tab-switch keys) until its own filter input is
// actually active.
func TestFindCapturesTextOnlyWhileFiltering(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = press(s, findKey)
	if s.CapturesText() {
		t.Fatal("find overlay should not capture text before filtering starts")
	}
	s, _ = press(s, tea.KeyPressMsg{Code: '/', Text: "/"})
	if !s.CapturesText() {
		t.Error("find overlay should capture text once filtering has started")
	}
}

// applyFilterCmd runs a command and feeds every message it (possibly, via a
// batch) produces back into the dashboard — including bubbles/list's
// FilterMatchesMsg, which is what actually narrows a list-backed overlay after
// typing. Without this, a test can only observe that keystrokes reached the
// filter input, not that filtering itself works — exactly the gap that let the
// list.FilterMatchesMsg routing bug through.
func applyFilterCmd(s dashboardScreen, cmd tea.Cmd) dashboardScreen {
	if cmd == nil {
		return s
	}
	msg := cmd()
	if msg == nil {
		return s
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			s = applyFilterCmd(s, c)
		}
		return s
	}
	return feed(s, msg)
}

// typeFilter presses "/" to start filtering, types query, and runs every
// resulting command (including the async FilterMatchesMsg round-trip) so the
// list is actually narrowed by the time it returns.
func typeFilter(s dashboardScreen, query string) dashboardScreen {
	var cmd tea.Cmd
	s, cmd = press(s, tea.KeyPressMsg{Code: '/', Text: "/"})
	s = applyFilterCmd(s, cmd)
	for _, r := range query {
		s, cmd = press(s, tea.KeyPressMsg{Code: r, Text: string(r)})
		s = applyFilterCmd(s, cmd)
	}
	return s
}

// TestFindFilterNarrowsList is the regression test for the bug Ibnu hit: typing
// into the "/" filter looked like it did nothing because bubbles/list's async
// FilterMatchesMsg reply was never routed back into the dashboard, so the list
// underneath never actually narrowed (it just showed the typed text). This
// drives the real round-trip and checks the visible list, not just the input.
func TestFindFilterNarrowsList(t *testing.T) {
	s, _ := newPGDashboard(t) // databases: postgres, appdb

	s, _ = press(s, findKey)
	s = typeFilter(s, "app")

	// bubbles/list highlights each matched character of a fuzzy hit with its own
	// ANSI style, which splits a plain substring check mid-word — strip styling
	// first, the same way the navigator's own cursor row does (stripStyle).
	view := stripStyle(s.View(120, 40))
	if strings.Contains(view, "postgres") {
		t.Errorf("filtering to %q should hide postgres:\n%s", "app", view)
	}
	if !strings.Contains(view, "appdb") {
		t.Errorf("filtering to %q should keep appdb:\n%s", "app", view)
	}
}

// TestFindFilterThenPick drives the whole two-stage flow end to end: type a
// filter, enter accepts it (first enter), enter again picks the highlighted
// (now sole) match — matching what Ibnu described as "entering enter does
// nothing" when the narrowing itself was broken.
func TestFindFilterThenPick(t *testing.T) {
	s, _ := newPGDashboard(t) // currentDB = postgres

	s, _ = press(s, findKey)
	s = typeFilter(s, "app")

	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter}) // accept the filter
	if s.mode != modeFind {
		t.Fatalf("the first enter should only accept the filter, mode = %d", s.mode)
	}
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter}) // pick the match

	if s.mode != modeBrowse {
		t.Fatalf("the second enter should pick and close, mode = %d", s.mode)
	}
	if s.currentDB != "appdb" {
		t.Errorf("currentDB = %q, want appdb", s.currentDB)
	}
	if cmd == nil {
		t.Error("picking appdb should reload its tables")
	}
}
