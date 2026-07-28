package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// vimDashboard builds a dashboard with the modal editor enabled, focused on the
// editor in normal mode with sql already loaded. It flips the package vimEditor
// flag (as ApplyConfig would) and restores it after the test.
func vimDashboard(t *testing.T, sql string) dashboardScreen {
	t.Helper()
	old := vimEditor
	vimEditor = true
	t.Cleanup(func() { vimEditor = old })

	s, _ := newPGDashboard(t)
	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"}) // focus editor → normal mode
	s.editor.SetValue(sql)
	moveCursorTo(&s.editor, 0, 0)
	return s
}

// typeRunes sends each rune as a key press (used to type in insert mode).
func typeRunes(s dashboardScreen, text string) dashboardScreen {
	for _, r := range text {
		s, _ = press(s, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return s
}

// TestVimStartsInNormalMode covers 2.4: with vim on, focusing the editor lands in
// normal mode, and a plain letter is a command — it does not type.
func TestVimStartsInNormalMode(t *testing.T) {
	s := vimDashboard(t, "select 1")
	if s.vim.mode != editorNormal {
		t.Fatalf("editor should open in normal mode, got %d", s.vim.mode)
	}
	before := s.editor.Value()
	s, _ = press(s, tea.KeyPressMsg{Code: 'x', Text: "x"}) // x = delete char, not insert 'x'
	if strings.Contains(s.editor.Value(), "xselect") {
		t.Error("a letter in normal mode must not be typed as text")
	}
	_ = before
}

// TestVimInsertAndEscape covers 2.4: i enters insert mode where letters type, and
// esc returns to normal.
func TestVimInsertAndEscape(t *testing.T) {
	s := vimDashboard(t, "")
	s, _ = press(s, tea.KeyPressMsg{Code: 'i', Text: "i"})
	if s.vim.mode != editorInsert {
		t.Fatalf("i should enter insert mode, got %d", s.vim.mode)
	}
	s = typeRunes(s, "hello")
	if s.editor.Value() != "hello" {
		t.Errorf("insert-mode typing failed, value = %q", s.editor.Value())
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc})
	if s.vim.mode != editorNormal {
		t.Errorf("esc should return to normal mode, got %d", s.vim.mode)
	}
}

// TestVimMotionsHJKL covers 2.4: hjkl move the cursor without editing.
func TestVimMotionsHJKL(t *testing.T) {
	s := vimDashboard(t, "abc\ndef")
	// From (0,0): l l → (0,2); j → (1,2); h → (1,1); k → (0,1).
	for _, k := range []rune{'l', 'l'} {
		s, _ = press(s, tea.KeyPressMsg{Code: k, Text: string(k)})
	}
	if s.editor.Line() != 0 || s.editor.Column() != 2 {
		t.Fatalf("ll → (%d,%d), want (0,2)", s.editor.Line(), s.editor.Column())
	}
	s, _ = press(s, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if s.editor.Line() != 1 {
		t.Errorf("j should move down a line, got line %d", s.editor.Line())
	}
	s, _ = press(s, tea.KeyPressMsg{Code: 'h', Text: "h"})
	if s.editor.Column() != 1 {
		t.Errorf("h should move left, col %d", s.editor.Column())
	}
	s, _ = press(s, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if s.editor.Line() != 0 {
		t.Errorf("k should move up, line %d", s.editor.Line())
	}
}

// TestVimLineEnds covers 2.4: 0 and $ jump to line start/end.
func TestVimLineEnds(t *testing.T) {
	s := vimDashboard(t, "select * from t")
	s, _ = press(s, tea.KeyPressMsg{Code: '$', Text: "$"})
	if s.editor.Column() != len([]rune("select * from t")) {
		t.Errorf("$ should go to line end, col %d", s.editor.Column())
	}
	s, _ = press(s, tea.KeyPressMsg{Code: '0', Text: "0"})
	if s.editor.Column() != 0 {
		t.Errorf("0 should go to line start, col %d", s.editor.Column())
	}
}

// TestVimWordMotion covers 2.4: w advances to the next word start, b goes back.
func TestVimWordMotion(t *testing.T) {
	s := vimDashboard(t, "select id from users")
	s, _ = press(s, tea.KeyPressMsg{Code: 'w', Text: "w"}) // → "id" at col 7
	if s.editor.Column() != 7 {
		t.Fatalf("w → col %d, want 7 (start of 'id')", s.editor.Column())
	}
	s, _ = press(s, tea.KeyPressMsg{Code: 'w', Text: "w"}) // → "from" at col 10
	if s.editor.Column() != 10 {
		t.Fatalf("second w → col %d, want 10 (start of 'from')", s.editor.Column())
	}
	s, _ = press(s, tea.KeyPressMsg{Code: 'b', Text: "b"}) // ← back to "id" at col 7
	if s.editor.Column() != 7 {
		t.Errorf("b → col %d, want 7", s.editor.Column())
	}
}

// TestVimGgG covers 2.4: gg jumps to the top, G to the bottom.
func TestVimGgG(t *testing.T) {
	s := vimDashboard(t, "one\ntwo\nthree")
	s, _ = press(s, tea.KeyPressMsg{Code: 'G', Text: "G"})
	if s.editor.Line() != 2 {
		t.Fatalf("G should go to the last line, got %d", s.editor.Line())
	}
	// gg is two keys.
	s, _ = press(s, tea.KeyPressMsg{Code: 'g', Text: "g"})
	s, _ = press(s, tea.KeyPressMsg{Code: 'g', Text: "g"})
	if s.editor.Line() != 0 {
		t.Errorf("gg should go to the first line, got %d", s.editor.Line())
	}
}

// TestVimDeleteChar covers 2.4: x deletes the character under the cursor.
func TestVimDeleteChar(t *testing.T) {
	s := vimDashboard(t, "select")
	s, _ = press(s, tea.KeyPressMsg{Code: 'x', Text: "x"}) // delete 's'
	if s.editor.Value() != "elect" {
		t.Errorf("x should delete the char under the cursor, got %q", s.editor.Value())
	}
}

// TestVimDeleteToEnd covers 2.4: D deletes from the cursor to the end of line.
func TestVimDeleteToEnd(t *testing.T) {
	s := vimDashboard(t, "select * from t")
	// move to col 7 (the '*') via two words, then D.
	s, _ = press(s, tea.KeyPressMsg{Code: 'w', Text: "w"})
	s, _ = press(s, tea.KeyPressMsg{Code: 'D', Text: "D"})
	if s.editor.Value() != "select " {
		t.Errorf("D should cut to end of line, got %q", s.editor.Value())
	}
}

// TestVimDeleteLine covers 2.4: dd removes the current line.
func TestVimDeleteLine(t *testing.T) {
	s := vimDashboard(t, "line one\nline two")
	s, _ = press(s, tea.KeyPressMsg{Code: 'd', Text: "d"})
	s, _ = press(s, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if s.editor.Value() != "line two" {
		t.Errorf("dd should delete the current line, got %q", s.editor.Value())
	}
}

// TestVimOpenLine covers 2.4: o opens a line below and enters insert mode.
func TestVimOpenLine(t *testing.T) {
	s := vimDashboard(t, "first")
	s, _ = press(s, tea.KeyPressMsg{Code: 'o', Text: "o"})
	if s.vim.mode != editorInsert {
		t.Fatalf("o should enter insert mode, got %d", s.vim.mode)
	}
	s = typeRunes(s, "second")
	if s.editor.Value() != "first\nsecond" {
		t.Errorf("o should open a line below, got %q", s.editor.Value())
	}
}

// TestVimStatusShowsMode covers 2.4: the editor pane advertises the current mode.
func TestVimStatusShowsMode(t *testing.T) {
	s := vimDashboard(t, "select 1")
	if !strings.Contains(s.View(120, 40), "NORMAL") {
		t.Errorf("editor status should show the NORMAL mode tag:\n%s", s.View(120, 40))
	}
	s, _ = press(s, tea.KeyPressMsg{Code: 'i', Text: "i"})
	if !strings.Contains(s.View(120, 40), "INSERT") {
		t.Errorf("editor status should show the INSERT mode tag:\n%s", s.View(120, 40))
	}
}

// TestVimReentryIsNormal covers 2.4: leaving the editor and coming back lands in
// normal mode again, even if you left mid-insert.
func TestVimReentryIsNormal(t *testing.T) {
	s := vimDashboard(t, "x")
	s, _ = press(s, tea.KeyPressMsg{Code: 'i', Text: "i"}) // insert
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyTab})     // tab cycles focus away
	if s.focus == focusEditor {
		t.Fatal("tab should have moved focus off the editor")
	}
	// Cycle back to the editor with e.
	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"})
	if s.vim.mode != editorNormal {
		t.Errorf("re-entering the editor should be normal mode, got %d", s.vim.mode)
	}
}

// TestVimDisabledUnchanged covers 2.4's opt-in guarantee: with vim off, a letter
// types into the editor as before (no modal interception).
func TestVimDisabledUnchanged(t *testing.T) {
	s, _ := newPGDashboard(t) // vimEditor defaults off
	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"})
	s = typeRunes(s, "select")
	if s.editor.Value() != "select" {
		t.Errorf("with vim off, letters should type normally, got %q", s.editor.Value())
	}
}
