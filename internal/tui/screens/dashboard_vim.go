package screens

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/textarea"
)

// editorMode is the modal editor's current mode (v2 2.4). Only meaningful when
// vim is enabled; a non-vim editor is permanently in insert-equivalent typing.
type editorMode int

const (
	editorInsert editorMode = iota // keys type into the textarea (default, non-vim)
	editorNormal                   // keys are motions/commands; no text is inserted
)

// vimState is the modal editor's small piece of state (v2 2.4): whether the modal
// editor is enabled at all, which mode it's in, and any pending first key of a
// two-key command (d for dd, g for gg). It's deliberately tiny — this is a
// pragmatic vim subset for short SQL, not a full editor.
type vimState struct {
	enabled bool
	mode    editorMode
	pending byte // 0, 'd', or 'g'
}

// newVimState builds the initial modal state. When enabled the editor opens in
// normal mode (authentic modal behaviour: press i/a to type); otherwise it stays
// in the plain insert-equivalent that never intercepts keys.
func newVimState(enabled bool) vimState {
	if enabled {
		return vimState{enabled: true, mode: editorNormal}
	}
	return vimState{}
}

// toNormal returns to normal mode (clearing any pending key) when the modal
// editor is enabled. It's called whenever focus (re)enters the editor, so a fresh
// entry is always modal, mirroring how vim lands you in normal mode.
func (v vimState) toNormal() vimState {
	if v.enabled {
		v.mode = editorNormal
		v.pending = 0
	}
	return v
}

// handleVimEditorKey routes a key through the modal editor (v2 2.4). It is only
// reached when vim is enabled; insert mode behaves like the plain editor (text
// plus the history cycle), while normal mode interprets motions and commands.
func (s dashboardScreen) handleVimEditorKey(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	if s.vim.mode == editorInsert {
		switch msg.String() {
		case "esc":
			s.vim = s.vim.toNormal()
			return s, nil
		case "ctrl+p":
			s.historyOlder()
			return s, nil
		case "ctrl+n":
			s.historyNewer()
			return s, nil
		}
		var cmd tea.Cmd
		s.editor, cmd = s.editor.Update(msg)
		return s, cmd
	}
	return s.handleVimNormalKey(msg)
}

// handleVimNormalKey interprets one key in normal mode: two-key sequences first
// (dd, gg), then motions, insert-entry commands, and edits. An unrecognised key
// is a no-op — normal mode never inserts text.
func (s dashboardScreen) handleVimNormalKey(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	k := msg.String()

	// Resolve a pending two-key command.
	switch s.vim.pending {
	case 'd':
		s.vim.pending = 0
		if k == "d" {
			s.vimDeleteLine()
		}
		return s, nil
	case 'g':
		s.vim.pending = 0
		if k == "g" {
			s.editor.MoveToBegin()
		}
		return s, nil
	}

	switch k {
	case "esc":
		// An exit hatch: esc in normal mode leaves the editor for the navigator,
		// the same as the non-vim editor's esc. Re-enter with e/tab (back in normal).
		s.focus = s.focusOrder()[0]
		return s, s.syncEditorFocus()

	// --- motions ---
	case "h", "left":
		s.editor.SetCursorColumn(s.editor.Column() - 1)
	case "l", "right":
		s.editor.SetCursorColumn(s.editor.Column() + 1)
	case "j", "down":
		s.editor.CursorDown()
	case "k", "up":
		s.editor.CursorUp()
	case "0":
		s.editor.CursorStart()
	case "$":
		s.editor.CursorEnd()
	case "w":
		s.vimWord(true)
	case "b":
		s.vimWord(false)
	case "G":
		s.editor.MoveToEnd()
	case "g":
		s.vim.pending = 'g'

	// --- enter insert mode ---
	case "i":
		s.vim.mode = editorInsert
	case "a":
		s.editor.SetCursorColumn(s.editor.Column() + 1)
		s.vim.mode = editorInsert
	case "I":
		s.editor.CursorStart()
		s.vim.mode = editorInsert
	case "A":
		s.editor.CursorEnd()
		s.vim.mode = editorInsert
	case "o":
		s.vimOpenLine(true)
		s.vim.mode = editorInsert
	case "O":
		s.vimOpenLine(false)
		s.vim.mode = editorInsert

	// --- edits ---
	case "x":
		s.vimDeleteChar()
	case "D":
		s.vimDeleteToEnd()
	case "d":
		s.vim.pending = 'd'
	}
	return s, nil
}

// --- edit primitives (the textarea has no delete/open API, so these rewrite the
// value and reposition the cursor by logical line/column) ---

// moveCursorTo repositions the textarea cursor at a logical (line, col), used
// after a SetValue (which parks the cursor at the buffer end). It walks down from
// the top because the textarea only exposes relative line moves plus an absolute
// column set.
func moveCursorTo(ed *textarea.Model, line, col int) {
	ed.MoveToBegin()
	for range line {
		ed.CursorDown()
	}
	ed.SetCursorColumn(col)
}

// vimDeleteChar deletes the character under the cursor (x), leaving the cursor on
// what follows (or the new last char of the line).
func (s *dashboardScreen) vimDeleteChar() {
	lines := strings.Split(s.editor.Value(), "\n")
	l, c := s.editor.Line(), s.editor.Column()
	if l >= len(lines) {
		return
	}
	runes := []rune(lines[l])
	if c >= len(runes) {
		return
	}
	runes = append(runes[:c], runes[c+1:]...)
	lines[l] = string(runes)
	s.editor.SetValue(strings.Join(lines, "\n"))
	moveCursorTo(&s.editor, l, clamp(c, 0, max(len(runes)-1, 0)))
}

// vimDeleteToEnd deletes from the cursor to the end of the line (D).
func (s *dashboardScreen) vimDeleteToEnd() {
	lines := strings.Split(s.editor.Value(), "\n")
	l, c := s.editor.Line(), s.editor.Column()
	if l >= len(lines) {
		return
	}
	runes := []rune(lines[l])
	if c > len(runes) {
		c = len(runes)
	}
	lines[l] = string(runes[:c])
	s.editor.SetValue(strings.Join(lines, "\n"))
	moveCursorTo(&s.editor, l, clamp(c-1, 0, max(c-1, 0)))
}

// vimDeleteLine deletes the whole current line (dd). The last remaining line
// collapses to empty rather than removing the buffer entirely.
func (s *dashboardScreen) vimDeleteLine() {
	lines := strings.Split(s.editor.Value(), "\n")
	l := s.editor.Line()
	if l >= len(lines) {
		return
	}
	if len(lines) == 1 {
		s.editor.SetValue("")
		moveCursorTo(&s.editor, 0, 0)
		return
	}
	lines = append(lines[:l], lines[l+1:]...)
	s.editor.SetValue(strings.Join(lines, "\n"))
	moveCursorTo(&s.editor, clamp(l, 0, len(lines)-1), 0)
}

// vimOpenLine inserts a blank line below (o) or above (O) the current one and
// parks the cursor on it, ready for insert mode.
func (s *dashboardScreen) vimOpenLine(below bool) {
	lines := strings.Split(s.editor.Value(), "\n")
	l := s.editor.Line()
	at := l
	if below {
		at = l + 1
	}
	if at > len(lines) {
		at = len(lines)
	}
	lines = append(lines[:at], append([]string{""}, lines[at:]...)...)
	s.editor.SetValue(strings.Join(lines, "\n"))
	moveCursorTo(&s.editor, at, 0)
}

// vimWord moves to the start of the next word (w) or the previous word (b),
// scanning the flattened buffer so the motion crosses line boundaries the way
// vim does.
func (s *dashboardScreen) vimWord(forward bool) {
	lines := strings.Split(s.editor.Value(), "\n")
	l, c := s.editor.Line(), s.editor.Column()
	flat := []rune(strings.Join(lines, "\n"))
	off := runeOffset(lines, l, c)
	if forward {
		off = wordForwardOffset(flat, off)
	} else {
		off = wordBackOffset(flat, off)
	}
	nl, nc := offsetToLineCol(flat, off)
	moveCursorTo(&s.editor, nl, nc)
}

// runeOffset converts a logical (line, col) into an absolute rune offset in the
// buffer joined by single-rune newlines — the coordinate the word scanners use.
func runeOffset(lines []string, line, col int) int {
	off := 0
	for i := 0; i < line && i < len(lines); i++ {
		off += len([]rune(lines[i])) + 1 // +1 for the '\n'
	}
	return off + col
}

// offsetToLineCol is runeOffset's inverse: it maps an absolute rune offset back
// to a logical (line, col) by counting newlines.
func offsetToLineCol(flat []rune, off int) (line, col int) {
	off = clamp(off, 0, len(flat))
	for i := 0; i < off; i++ {
		if flat[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return line, col
}

// charClass buckets a rune for word motion: spaces, word characters (identifier
// runes), and everything else (punctuation/operators), matching vim's default
// three-way split closely enough for SQL.
type charClass int

const (
	classSpace charClass = iota
	classWord
	classPunct
)

func classOf(r rune) charClass {
	switch {
	case r == ' ' || r == '\t' || r == '\n' || r == '\r':
		return classSpace
	case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
		return classWord
	default:
		return classPunct
	}
}

// wordForwardOffset returns the offset of the next word start at or after off: it
// steps over the current run of same-class characters, then over any whitespace,
// landing on the first character of the next word/punct run.
func wordForwardOffset(text []rune, off int) int {
	n := len(text)
	if off >= n {
		return n
	}
	if c := classOf(text[off]); c != classSpace {
		for off < n && classOf(text[off]) == c {
			off++
		}
	}
	for off < n && classOf(text[off]) == classSpace {
		off++
	}
	return clamp(off, 0, max(n-1, 0))
}

// wordBackOffset returns the offset of the previous word start before off: it
// steps back over whitespace, then to the beginning of the run it lands in.
func wordBackOffset(text []rune, off int) int {
	if off <= 0 {
		return 0
	}
	off--
	for off > 0 && classOf(text[off]) == classSpace {
		off--
	}
	if off > 0 {
		c := classOf(text[off])
		for off > 0 && classOf(text[off-1]) == c {
			off--
		}
	}
	return off
}
