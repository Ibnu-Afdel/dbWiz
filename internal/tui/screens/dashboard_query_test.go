package screens

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// editing focuses the Query pane via [e] and loads sql into the textarea, ready
// to run.
func editing(s dashboardScreen, sql string) dashboardScreen {
	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"})
	s.editor.SetValue(sql)
	return s
}

// runF5 presses the F5 run key and returns the resulting screen and command.
func runF5(s dashboardScreen) (dashboardScreen, tea.Cmd) {
	return press(s, tea.KeyPressMsg{Code: tea.KeyF5})
}

// TestQueryRunKeys covers the tmux-safe run bindings: ctrl+r and alt+enter must
// each execute the statement (ctrl+enter collapses to a newline under tmux, so
// these are the reliable fallbacks). Plain enter must still insert a newline.
func TestQueryRunKeys(t *testing.T) {
	run := map[string]tea.KeyPressMsg{
		"ctrl+r":    {Code: 'r', Mod: tea.ModCtrl},
		"alt+enter": {Code: tea.KeyEnter, Mod: tea.ModAlt},
		"f5":        {Code: tea.KeyF5},
	}
	for name, keyMsg := range run {
		s, _ := newPGDashboard(t)
		s = editing(s, "SELECT 1")
		s, cmd := press(s, keyMsg)
		if !s.querying || cmd == nil {
			t.Errorf("%s should run the statement (querying=%v, cmd=%v)", name, s.querying, cmd != nil)
		}
	}

	// Plain enter is a newline, not a run.
	s, _ := newPGDashboard(t)
	s = editing(s, "SELECT 1")
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.querying {
		t.Error("plain enter should insert a newline, not run the query")
	}
	if !strings.Contains(s.editor.Value(), "\n") {
		t.Error("plain enter should have added a newline to the editor")
	}
}

// TestQueryEditorFocus covers Step 7.1: [e] focuses the editor from a browse
// pane, typing lands in it, and esc returns focus to the navigator.
func TestQueryEditorFocus(t *testing.T) {
	s, _ := newPGDashboard(t) // starts on focusDatabases

	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"})
	if s.focus != focusEditor {
		t.Fatalf("[e] should focus the editor, got %d", s.focus)
	}

	// A printable key edits the textarea.
	s, _ = press(s, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := s.editor.Value(); got != "x" {
		t.Errorf("editor value = %q, want x", got)
	}

	// esc leaves the editor for the navigator (not out of the dashboard).
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEsc})
	if s.focus == focusEditor {
		t.Error("esc should move focus off the editor")
	}
	if _, ok := runCmdMaybe(cmd).(PopMsg); ok {
		t.Error("esc in the editor should not pop the dashboard")
	}
}

// TestQueryRunSelect covers Steps 7.2/7.3: F5 runs the statement asynchronously
// and its rows render in the results pane, NULLs preserved.
func TestQueryRunSelect(t *testing.T) {
	s, eng := newPGDashboard(t)
	eng.queryResult = db.Result{
		Columns:  []string{"id", "name"},
		Rows:     [][]any{{"1", "ada"}, {"2", nil}},
		Duration: 3 * time.Millisecond,
	}

	s = editing(s, "SELECT id, name FROM people")
	s, cmd := runF5(s)
	if !s.querying {
		t.Fatal("F5 should start a query")
	}
	if cmd == nil {
		t.Fatal("run should return a command")
	}

	// The async command actually calls the engine with the statement text.
	if _, ok := runCmd(t, runQueryCmd(context.Background(), eng, "SELECT id, name FROM people", 1, "SELECT")).(queryDoneMsg); !ok {
		t.Fatal("runQueryCmd should produce queryDoneMsg on success")
	}
	if eng.lastQuery != "SELECT id, name FROM people" {
		t.Errorf("engine saw %q", eng.lastQuery)
	}

	s = feed(s, queryDoneMsg{seq: s.querySeq, verb: "SELECT", result: eng.queryResult})
	if s.querying {
		t.Error("done reply should clear the running state")
	}
	view := s.View(120, 40)
	for _, want := range []string{"Results — query", "name", "ada", "NULL", "2 rows"} {
		if !strings.Contains(view, want) {
			t.Errorf("query result missing %q\n%s", want, view)
		}
	}
}

// TestQueryExecResult covers Step 7.3's exec branch: a statement with no result
// set reports the affected-row count with its verb.
func TestQueryExecResult(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "UPDATE people SET name = 'x'")
	s, _ = runF5(s)
	s = feed(s, queryDoneMsg{seq: s.querySeq, verb: "UPDATE",
		result: db.Result{RowsAffected: 3, Duration: 12 * time.Millisecond}})

	view := s.View(120, 40)
	for _, want := range []string{"UPDATE", "3 row(s) affected"} {
		if !strings.Contains(view, want) {
			t.Errorf("exec result missing %q\n%s", want, view)
		}
	}
}

// TestQueryEmptyResult covers Step 7.3's empty state: a zero-row result set says
// "0 rows" rather than rendering blank.
func TestQueryEmptyResult(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "SELECT * FROM people WHERE false")
	s, _ = runF5(s)
	s = feed(s, queryDoneMsg{seq: s.querySeq, verb: "SELECT",
		result: db.Result{Columns: []string{"id"}, Rows: nil, Duration: time.Millisecond}})
	if !strings.Contains(s.View(120, 40), "0 rows") {
		t.Errorf("empty result should say 0 rows:\n%s", s.View(120, 40))
	}
}

// TestQueryInlineError covers Step 7.4: a syntax error renders inline under the
// editor and never bounces the user off the dashboard.
func TestQueryInlineError(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "SELCT 1")
	s, _ = runF5(s)

	s = feed(s, queryErrMsg{seq: s.querySeq, err: &db.DBError{
		Kind:   db.DBErrQuerySyntax,
		Title:  "Syntax error",
		Detail: "near \"SELCT\"",
	}})
	if s.querying {
		t.Error("error reply should clear the running state")
	}
	if _, ok := Screen(s).(dashboardScreen); !ok {
		t.Error("a syntax error must not replace the dashboard")
	}
	if !strings.Contains(s.View(120, 40), "Syntax error") {
		t.Errorf("syntax error should render inline:\n%s", s.View(120, 40))
	}
}

// TestQuerySystemErrorRoutes covers Step 7.4's system-error branch: a lost
// connection routes to the full-screen error screen.
func TestQuerySystemErrorRoutes(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "SELECT 1")
	s, _ = runF5(s)

	next, cmd := s.Update(queryErrMsg{seq: s.querySeq, err: &db.DBError{
		Kind:  db.DBErrConnRefused,
		Title: "Couldn't reach the database",
	}})
	_ = next
	msg := runCmd(t, cmd)
	push, ok := msg.(PushMsg)
	if !ok {
		t.Fatalf("system error should push a screen, got %T", msg)
	}
	if _, ok := push.Screen.(errorScreen); !ok {
		t.Errorf("system error should route to the error screen, got %T", push.Screen)
	}
}

// TestQueryStaleReplyDropped covers the seq guard: a reply for a superseded run
// is ignored.
func TestQueryStaleReplyDropped(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "SELECT 1")
	s, _ = runF5(s) // querySeq == 1

	// A late reply tagged with an older seq must not surface.
	s = feed(s, queryDoneMsg{seq: 0, verb: "SELECT",
		result: db.Result{Columns: []string{"ghost"}, Rows: [][]any{{"boo"}}}})
	if strings.Contains(s.View(120, 40), "ghost") {
		t.Error("a stale query reply should be dropped")
	}
	if !s.querying {
		t.Error("a dropped stale reply should leave the query still running")
	}
}

// TestQueryCancel covers Step 7.5: esc while a query runs cancels it (storing a
// cancel func), and the canceled reply resolves inline without leaving the
// dashboard.
func TestQueryCancel(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "SELECT pg_sleep(60)")
	s, _ = runF5(s)
	if s.queryCancel == nil {
		t.Fatal("a running query should hold a cancel func")
	}

	// esc cancels rather than leaving the editor while a query is in flight.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc})
	if !s.querying {
		t.Error("query should stay running until the canceled reply arrives")
	}

	s = feed(s, queryErrMsg{seq: s.querySeq, err: &db.DBError{
		Kind:  db.DBErrCanceled,
		Title: "Cancelled",
	}})
	if s.querying {
		t.Error("canceled reply should clear the running state")
	}
	if _, ok := Screen(s).(dashboardScreen); !ok {
		t.Error("cancellation must not leave the dashboard")
	}
}

// TestQuerySoftTimeout covers Step 7.5's soft-timeout hint: a long-running query
// nudges the user to wait or cancel.
func TestQuerySoftTimeout(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "SELECT slow()")
	s, _ = runF5(s)
	s.queryStart = time.Now().Add(-(querySoftTimeout + time.Second))
	if !strings.Contains(s.View(120, 40), "still running") {
		t.Errorf("a long query should show the soft-timeout hint:\n%s", s.View(120, 40))
	}
}

// TestQueryHistory covers Step 7.6: ctrl+p/ctrl+n cycle previously-run
// statements back into the editor.
func TestQueryHistory(t *testing.T) {
	s, _ := newPGDashboard(t)

	// Run two statements, resolving each so the editor re-focuses between them.
	s = editing(s, "SELECT 1")
	s, _ = runF5(s)
	s = feed(s, queryDoneMsg{seq: s.querySeq, verb: "SELECT"})
	s.editor.SetValue("SELECT 2")
	s, _ = runF5(s)
	s = feed(s, queryDoneMsg{seq: s.querySeq, verb: "SELECT"})

	prev := func() string {
		s, _ = press(s, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
		return s.editor.Value()
	}
	next := func() string {
		s, _ = press(s, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
		return s.editor.Value()
	}

	if got := prev(); got != "SELECT 2" {
		t.Errorf("ctrl+p 1 = %q, want SELECT 2", got)
	}
	if got := prev(); got != "SELECT 1" {
		t.Errorf("ctrl+p 2 = %q, want SELECT 1", got)
	}
	if got := next(); got != "SELECT 2" {
		t.Errorf("ctrl+n = %q, want SELECT 2", got)
	}
	if got := next(); got != "" {
		t.Errorf("ctrl+n past newest should clear the editor, got %q", got)
	}
}

// TestQueryRunEmptyNoop covers the guard that a blank editor doesn't launch a
// query.
func TestQueryRunEmptyNoop(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"}) // focus editor, empty
	s, cmd := runF5(s)
	if s.querying {
		t.Error("running an empty editor should be a no-op")
	}
	_ = cmd
}

// runCmdMaybe runs a command if non-nil, returning its message or nil. Unlike
// runCmd it does not fail on a nil command — handy when a handler legitimately
// returns no command.
func runCmdMaybe(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}
