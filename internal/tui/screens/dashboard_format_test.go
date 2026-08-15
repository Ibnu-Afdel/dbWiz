package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var altF = tea.KeyPressMsg{Code: 'f', Mod: tea.ModAlt}

// TestFormatReformatsEditorStatement covers v5 2.2: ⌥f reformats the editor's
// statement in place, without leaving the editor.
func TestFormatReformatsEditorStatement(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = s.focusEditor()
	s.editor.SetValue("select id, name from users where active = true")

	s, cmd := press(s, altF)
	if cmd == nil {
		t.Fatal("⌥f should return the (re)focus command")
	}
	got := s.editor.Value()
	want := "SELECT id, name\nFROM users\nWHERE active = TRUE"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if s.focus != focusEditor {
		t.Errorf("⌥f should stay in the editor, focus=%v", s.focus)
	}
}

// TestFormatWorksFromBrowsePanes mirrors ⌥p's reach (dashboard.go's
// browse-mode switch): the key acts on "whatever's in the editor" regardless
// of which pane currently has focus.
func TestFormatWorksFromBrowsePanes(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.editor.SetValue("select id from t")
	s.focus = focusTables

	s, _ = press(s, altF)
	if s.editor.Value() != "SELECT id\nFROM t" {
		t.Fatalf("got %q", s.editor.Value())
	}
}

// TestFormatOnEmptyEditorShowsNotice covers the guard: nothing to format is
// said, not silently ignored.
func TestFormatOnEmptyEditorShowsNotice(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = s.focusEditor()

	s, _ = press(s, altF)
	if !s.noticeErr || !strings.Contains(s.notice, "editor first") {
		t.Fatalf("expected a guard notice, got notice=%q noticeErr=%v", s.notice, s.noticeErr)
	}
}

// TestFormatAlreadyFormattedSaysSo: a no-op reformat still visibly responds,
// the same instinct as openComplete's "no completions" notice.
func TestFormatAlreadyFormattedSaysSo(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = s.focusEditor()
	s.editor.SetValue("SELECT id\nFROM t")

	s, _ = press(s, altF)
	if s.noticeErr || s.notice != "Already formatted." {
		t.Fatalf("expected the already-formatted notice, got notice=%q noticeErr=%v", s.notice, s.noticeErr)
	}
}
