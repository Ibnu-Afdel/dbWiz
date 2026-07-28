package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// altH is the searchable-history open key (v2 2.1).
var altH = tea.KeyPressMsg{Code: 'h', Mod: tea.ModAlt}

// runAll executes a command and any commands a BatchMsg fans out, so a test can
// drive the side effects (like the best-effort history persist) that runQuery
// batches alongside the async query.
func runAll(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			runAll(c)
		}
	}
}

// seedHistoryStore records statements for the dashboard's target directly in the
// (hermetic) store, so the overlay has something to show without running queries.
func seedHistoryStore(t *testing.T, key string, sqls ...string) {
	t.Helper()
	for _, sql := range sqls {
		if err := state.AddHistory(key, sql); err != nil {
			t.Fatal(err)
		}
	}
}

// TestHistoryOverlayOpensFromEditor covers 2.1: alt+h from the SQL editor opens
// the searchable overlay showing the target's stored statements, newest first.
func TestHistoryOverlayOpensFromEditor(t *testing.T) {
	s, _ := newPGDashboard(t)
	seedHistoryStore(t, s.historyKey, "SELECT 1", "SELECT 2")

	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"}) // focus editor
	s, _ = press(s, altH)

	if s.mode != modeHistory {
		t.Fatalf("alt+h should open the history overlay, mode = %d", s.mode)
	}
	view := s.View(120, 40)
	for _, want := range []string{"Query history", "SELECT 1", "SELECT 2"} {
		if !strings.Contains(view, want) {
			t.Errorf("history overlay missing %q\n%s", want, view)
		}
	}
}

// TestHistoryOverlayOpensFromBrowse covers 2.1: alt+h works from a browse pane
// too, not only the editor.
func TestHistoryOverlayOpensFromBrowse(t *testing.T) {
	s, _ := newPGDashboard(t) // starts on a browse pane
	seedHistoryStore(t, s.historyKey, "SELECT now()")

	s, _ = press(s, altH)
	if s.mode != modeHistory {
		t.Fatalf("alt+h from browse should open the overlay, mode = %d", s.mode)
	}
}

// TestHistoryEmptyShowsNotice covers 2.1: with nothing stored yet, alt+h shows a
// plain-language notice instead of an empty box, and stays in browse mode.
func TestHistoryEmptyShowsNotice(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = press(s, altH)

	if s.mode != modeBrowse {
		t.Errorf("empty history should not open an overlay, mode = %d", s.mode)
	}
	if !strings.Contains(s.View(120, 40), "No query history yet") {
		t.Errorf("empty history should show a notice:\n%s", s.View(120, 40))
	}
}

// TestHistoryPickLoadsIntoEditor covers 2.1's confirmed behaviour: Enter loads
// the highlighted statement into the editor (focused) and never auto-runs it.
func TestHistoryPickLoadsIntoEditor(t *testing.T) {
	s, _ := newPGDashboard(t)
	seedHistoryStore(t, s.historyKey, "SELECT 1", "SELECT newest")

	s, _ = press(s, altH)
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})

	if s.mode != modeBrowse {
		t.Errorf("Enter should close the overlay, mode = %d", s.mode)
	}
	if s.focus != focusEditor {
		t.Errorf("Enter should focus the editor, focus = %d", s.focus)
	}
	if got := s.editor.Value(); got != "SELECT newest" {
		t.Errorf("editor = %q, want the picked (newest) statement", got)
	}
	if s.querying {
		t.Error("picking a statement must not run it")
	}
	_ = cmd
}

// TestHistoryEscKeepsEditor covers 2.1: esc closes the overlay without touching
// whatever was already in the editor.
func TestHistoryEscKeepsEditor(t *testing.T) {
	s, _ := newPGDashboard(t)
	seedHistoryStore(t, s.historyKey, "SELECT 1")

	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"})
	s.editor.SetValue("SELECT draft")
	s, _ = press(s, altH)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc})

	if s.mode != modeBrowse {
		t.Errorf("esc should close the overlay, mode = %d", s.mode)
	}
	if got := s.editor.Value(); got != "SELECT draft" {
		t.Errorf("esc should leave the editor unchanged, got %q", got)
	}
}

// TestHistoryFilterCapturesText covers 2.1's searchability: '/' starts the list's
// filter input, which must capture text so the root doesn't steal digits as
// tab-switches.
func TestHistoryFilterCapturesText(t *testing.T) {
	s, _ := newPGDashboard(t)
	seedHistoryStore(t, s.historyKey, "SELECT users", "SELECT orders")

	s, _ = press(s, altH)
	if s.CapturesText() {
		t.Fatal("history overlay should not capture text before filtering starts")
	}
	s, _ = press(s, tea.KeyPressMsg{Code: '/', Text: "/"})
	if !s.historyList.settingFilter() {
		t.Fatal("'/' should start the history filter")
	}
	if !s.CapturesText() {
		t.Error("an active history filter must capture text")
	}
}

// TestHistoryPersistedOnRun covers 2.1: running a statement writes it to the
// cross-session store under the target's key.
func TestHistoryPersistedOnRun(t *testing.T) {
	s, _ := newPGDashboard(t)
	key := s.historyKey

	s = editing(s, "SELECT 99")
	_, cmd := runF5(s)
	runAll(cmd) // execute the batched best-effort persist

	got := state.History(key)
	if len(got) == 0 || got[0].SQL != "SELECT 99" {
		t.Fatalf("run should persist the statement, store = %+v", got)
	}
}

// TestHistoryRedactsPasswordOnDisk covers the password-hygiene guarantee: a
// CREATE USER … PASSWORD statement is masked before it reaches the persisted
// store, while the in-memory session ring keeps the raw text for exact recall.
func TestHistoryRedactsPasswordOnDisk(t *testing.T) {
	s, _ := newPGDashboard(t)
	key := s.historyKey
	const raw = "CREATE USER app WITH PASSWORD 'hunter2'"

	s = editing(s, raw)
	s, cmd := runF5(s)
	runAll(cmd)

	got := state.History(key)
	if len(got) == 0 {
		t.Fatal("statement should have been persisted")
	}
	if strings.Contains(got[0].SQL, "hunter2") {
		t.Errorf("persisted history must not contain the password: %q", got[0].SQL)
	}
	// The just-run in-memory ring keeps the raw statement so ctrl+p recalls it whole.
	if n := len(s.history); n == 0 || s.history[n-1] != raw {
		t.Errorf("session ring should keep the raw statement, got %v", s.history)
	}
}

// TestHistorySeededFromStore covers 2.1: a dashboard reopened on the same target
// recalls its persisted statements immediately via the ctrl+p cycle.
func TestHistorySeededFromStore(t *testing.T) {
	s, eng := newPGDashboard(t)
	seedHistoryStore(t, s.historyKey, "SELECT seeded")

	// Rebuild the dashboard for the same target: NewDashboard seeds the in-memory
	// ring from the store, so ctrl+p works before running anything this session.
	s2 := NewDashboard(eng, s.target, s.container).(dashboardScreen)
	s2 = sized(s2)
	s2, _ = press(s2, tea.KeyPressMsg{Code: 'e', Text: "e"})
	s2, _ = press(s2, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})

	if got := s2.editor.Value(); got != "SELECT seeded" {
		t.Errorf("ctrl+p on a reopened target = %q, want the seeded statement", got)
	}
}
