package screens

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/sqlfmt"
)

// formatEditor reformats the editor's statement in place (⌥f, v5 2.2) —
// clause-per-line, keywords upper-cased, via internal/sqlfmt. It's reachable
// both while the editor has focus (dashboard_query.go's handleEditorKey) and
// from the browse panes (dashboard.go), the same reach Plan already has,
// since both act on "whatever's in the editor" rather than the focused pane.
//
// A no-op reformat (statement is already formatted) says so rather than
// silently doing nothing — the same instinct as openComplete's "no
// completions" notice: a key press that visibly changed nothing should say
// why, not look like it didn't register.
func (s dashboardScreen) formatEditor() (dashboardScreen, tea.Cmd) {
	statement := s.editor.Value()
	if strings.TrimSpace(statement) == "" {
		s.notice, s.noticeErr = "Write a statement in the editor first — ⌥f then reformats it.", true
		return s, nil
	}
	formatted := sqlfmt.Format(statement)
	if formatted == statement {
		s.notice, s.noticeErr = "Already formatted.", false
		return s, nil
	}
	s.editor.SetValue(formatted)
	s.editor.MoveToEnd()
	s.notice, s.noticeErr = "", false
	return s, s.syncEditorFocus()
}
