package screens

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// historyMax bounds the in-memory statement ring (Step 7.6). It is a session
// convenience, not persistence — the ring is dropped on quit and nothing is
// written to disk.
const historyMax = 50

// querySoftTimeout is when a still-running statement starts nudging the user to
// wait or cancel (Step 7.5). It is a soft hint only: the query is never killed
// automatically — cancellation stays in the user's hands via [esc].
const querySoftTimeout = 10 * time.Second

// newSQLEditor builds the Query pane's textarea: a compact multi-line editor
// (no line numbers, per D10 — no vim modes in v1) that starts blurred and is
// focused when the user tabs/[e]s into the pane.
func newSQLEditor() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "SELECT * FROM …   (Ctrl+R or F5 to run)"
	ta.Prompt = "│ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0 // height is driven by the pane layout, not a fixed cap
	ta.Blur()
	return ta
}

// focusEditor moves focus to the Query pane and focuses the textarea, so [e]
// jumps straight into editing from anywhere on the dashboard (Step 7.1).
func (s dashboardScreen) focusEditor() (dashboardScreen, tea.Cmd) {
	s.focus = focusEditor
	return s, s.syncEditorFocus()
}

// syncEditorFocus focuses the textarea when the Query pane holds focus and no
// query is running, and blurs it otherwise. Returning the blink command keeps
// the cursor animating while editing.
func (s *dashboardScreen) syncEditorFocus() tea.Cmd {
	if s.focus == focusEditor && !s.querying {
		// (Re)entering the editor lands in normal mode when vim is on — this is the
		// single choke point every editor-entry path funnels through, so a fresh
		// entry is always modal (v2 2.4). A no-op when vim is disabled.
		s.vim = s.vim.toNormal()
		return s.editor.Focus()
	}
	s.editor.Blur()
	return nil
}

// resizeEditor sets the textarea's width/height to match the current Query pane
// layout so wrapping and the visible viewport stay correct as the window or
// focus changes.
func (s *dashboardScreen) resizeEditor() {
	d := s.dims()
	s.editor.SetWidth(max(d.rightInner, 1))
	s.editor.SetHeight(max(d.editorTextH, 1))
}

// handleEditorKey routes a key while the Query pane has focus: run, leave,
// history, or otherwise edit the textarea. Plain enter inserts a newline (the
// editor is multi-line); run is the distinct ctrl+enter / F5 (Step 7.2).
func (s dashboardScreen) handleEditorKey(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	// These app-level keys work in both the plain and modal editors. They're all
	// modifier/function combos, so they never clash with a vim normal-mode letter.
	switch {
	case key.Matches(msg, Keys.Run):
		return s.runQuery()
	case key.Matches(msg, Keys.Focus):
		// Tab keeps cycling panes app-wide rather than indenting; the editor
		// isn't a code formatter.
		s.focus = s.nextFocus()
		return s, s.syncEditorFocus()
	case key.Matches(msg, Keys.HistoryList):
		// Checked before the textarea fall-through so alt+h opens the search overlay
		// instead of typing.
		return s.openHistory()
	case key.Matches(msg, Keys.SaveQuery):
		// alt+w saves the statement under a name; checked before the fall-through so
		// the modifier combo doesn't reach the textarea as text.
		return s.openSaveQuery()
	case key.Matches(msg, Keys.SavedList):
		return s.openSaved()
	case key.Matches(msg, Keys.Export):
		// Export the last result without leaving the editor — handy right after a
		// run. Checked before the fall-through so alt+e doesn't reach the textarea.
		return s.openExport()
	case key.Matches(msg, Keys.Complete):
		// Schema-aware autocomplete for the word under the cursor (v2 2.5).
		return s.openComplete()
	}
	// The modal editor owns esc (insert→normal, normal→leave), the history cycle,
	// motions, and typing (v2 2.4).
	if s.vim.enabled {
		return s.handleVimEditorKey(msg)
	}
	// Plain editor (default): esc leaves for the navigator, ctrl+p/n cycle history,
	// everything else is text.
	switch {
	case key.Matches(msg, Keys.Back):
		// esc leaves the editor for the navigator rather than backing out of the
		// dashboard — exiting the whole screen is [b].
		s.focus = s.focusOrder()[0]
		return s, s.syncEditorFocus()
	case msg.String() == "ctrl+p":
		s.historyOlder()
		return s, nil
	case msg.String() == "ctrl+n":
		s.historyNewer()
		return s, nil
	}
	var cmd tea.Cmd
	s.editor, cmd = s.editor.Update(msg)
	return s, cmd
}

// runQuery launches the editor's statement asynchronously (Step 7.2). It records
// the statement in history, blurs the editor for the duration, and stores a
// cancel func so [esc] can kill it (Step 7.5). A blank editor is a no-op. The
// monotonic querySeq tags the launch so a superseded reply is dropped.
func (s dashboardScreen) runQuery() (dashboardScreen, tea.Cmd) {
	sql := strings.TrimSpace(s.editor.Value())
	if sql == "" {
		return s, nil
	}
	s.pushHistory(sql)

	ctx, cancel := context.WithCancel(context.Background())
	s.queryCancel = cancel
	s.querySeq++
	s.querying = true
	s.queryStart = time.Now()
	s.queryErr = nil
	s.editor.Blur()
	return s, tea.Batch(
		s.spinner.Tick,
		runQueryCmd(ctx, s.engine, sql, s.querySeq, firstKeyword(sql)),
		// Persist the statement to the cross-session store (v2 2.1); best-effort and
		// off the Update goroutine so it never blocks the run.
		persistHistoryCmd(s.historyKey, sql),
	)
}

// cancelQuery cancels the in-flight statement. The cancellation propagates
// through ctx to the driver (killing it server-side); the resulting canceled
// error comes back as queryErrMsg and clears the running state.
func (s dashboardScreen) cancelQuery() (dashboardScreen, tea.Cmd) {
	if s.queryCancel != nil {
		s.queryCancel()
	}
	return s, nil
}

// applyQueryDone records a completed statement's result into the shared results
// pane (Step 7.3) and re-focuses the editor so the user can iterate. A stale
// reply (seq mismatch) is ignored.
func (s dashboardScreen) applyQueryDone(msg queryDoneMsg) (dashboardScreen, tea.Cmd) {
	if msg.seq != s.querySeq {
		return s, nil
	}
	s.clearQueryCancel()
	s.querying = false
	s.queryErr = nil
	s.queryResult = msg.result
	s.queryVerb = msg.verb
	s.results, s.resultOffset, s.colOffset = resultsQuery, 0, 0
	return s, s.syncEditorFocus()
}

// applyQueryErr routes a failed statement (Step 7.4): user errors (bad SQL,
// constraint, missing object, cancellation) render inline under the editor;
// system errors (connection lost, timeout) go to the full-screen error screen
// so the user isn't left staring at a dead dashboard.
func (s dashboardScreen) applyQueryErr(msg queryErrMsg) (dashboardScreen, tea.Cmd) {
	if msg.seq != s.querySeq {
		return s, nil
	}
	s.clearQueryCancel()
	s.querying = false
	if isSystemQueryErr(msg.err) {
		return s, Push(NewErrorFromDB(msg.err, retrySpec{}))
	}
	s.queryErr = msg.err
	return s, s.syncEditorFocus()
}

// clearQueryCancel releases the running statement's cancel func once it has
// resolved, so a stored cancel never lingers past the query it belonged to.
func (s *dashboardScreen) clearQueryCancel() {
	if s.queryCancel != nil {
		s.queryCancel()
		s.queryCancel = nil
	}
}

// isSystemQueryErr reports whether a query failure is an environment/connection
// problem (route to the error screen) rather than something about the statement
// itself (render inline). Cancellation counts as inline: it's an expected,
// user-initiated outcome.
func isSystemQueryErr(e *db.DBError) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case db.DBErrConnRefused, db.DBErrTimeout, db.DBErrAuthFailed, db.DBErrInternal:
		return true
	}
	return false
}

// --- session history (Step 7.6) ---

// pushHistory appends a submitted statement to the in-memory ring, de-duplicating
// an immediate repeat, and resets the browse position to "editing fresh text".
func (s *dashboardScreen) pushHistory(sql string) {
	if n := len(s.history); n > 0 && s.history[n-1] == sql {
		s.historyIdx = -1
		return
	}
	s.history = append(s.history, sql)
	if len(s.history) > historyMax {
		s.history = s.history[len(s.history)-historyMax:]
	}
	s.historyIdx = -1
}

// historyOlder loads the previous statement into the editor (ctrl+p), stepping
// back through the ring from the most recent.
func (s *dashboardScreen) historyOlder() {
	if len(s.history) == 0 {
		return
	}
	if s.historyIdx == -1 {
		s.historyIdx = len(s.history)
	}
	if s.historyIdx > 0 {
		s.historyIdx--
	}
	s.editor.SetValue(s.history[s.historyIdx])
	s.editor.MoveToEnd()
}

// historyNewer moves toward the most recent statement (ctrl+n); stepping past
// the newest clears the editor back to a fresh line.
func (s *dashboardScreen) historyNewer() {
	if s.historyIdx == -1 {
		return
	}
	s.historyIdx++
	if s.historyIdx >= len(s.history) {
		s.historyIdx = -1
		s.editor.Reset()
		return
	}
	s.editor.SetValue(s.history[s.historyIdx])
	s.editor.MoveToEnd()
}

// firstKeyword returns the leading SQL keyword of text (upper-cased) so an exec
// result can read "UPDATE — 3 rows". It skips leading whitespace and comments,
// reusing the db package's understanding of statement shape indirectly by just
// taking the first word.
func firstKeyword(text string) string {
	f := strings.Fields(text)
	if len(f) == 0 {
		return ""
	}
	return strings.ToUpper(f[0])
}
