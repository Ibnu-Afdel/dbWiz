package screens

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/list"

	"github.com/Ibnu-Afdel/dbwiz/internal/state"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// historyItem adapts one persisted statement to the list's DefaultItem shape.
// Title is the statement's first line (so a multi-line query stays one row);
// FilterValue is the whole statement, so the built-in fuzzy filter searches
// across every line, not just the visible title; Description is when it last ran.
type historyItem struct {
	sql string
	at  int64
}

func (i historyItem) Title() string       { return firstLine(i.sql) }
func (i historyItem) FilterValue() string { return i.sql }
func (i historyItem) Description() string { return relativeTime(i.at) }

// historyResult is what a key press did to the history overlay.
type historyResult int

const (
	historyPending  historyResult = iota // handled internally (nav / filter typing)
	historyCanceled                      // esc — close without changing the editor
	historyPicked                        // enter — load the highlighted statement
)

// historyModel is the searchable, fuzzy-filtered query-history overlay (v2 2.1).
// It wraps bubbles/list to reuse its type-to-filter, mirroring the container
// picker. It is rebuilt from the store each time it opens, so it always shows the
// latest statements (including ones run this session).
type historyModel struct {
	list list.Model
}

// newHistoryModel builds the overlay from a target's stored statements, which
// arrive newest-first from state.History — the order the list should show.
func newHistoryModel(entries []state.HistoryEntry, width, height int) historyModel {
	items := make([]list.Item, len(entries))
	for i, e := range entries {
		items[i] = historyItem{sql: e.SQL, at: e.At}
	}
	l := list.New(items, list.NewDefaultDelegate(), width, height)
	l.Title = "Query history"
	l.SetShowHelp(false) // the app renders its own help bar
	l.SetShowStatusBar(false)
	return historyModel{list: l}
}

// openHistory opens the searchable overlay over this target's stored statements
// (alt+h). With no history yet it shows a brief notice instead of an empty box,
// so the key always does something legible.
func (s dashboardScreen) openHistory() (dashboardScreen, tea.Cmd) {
	entries := state.History(s.historyKey)
	if len(entries) == 0 {
		s.notice, s.noticeErr = "No query history yet for this target — run a statement first.", false
		return s, nil
	}
	w := clamp(s.width-8, 20, 100)
	h := clamp(s.height-8, 5, 24)
	s.historyList = newHistoryModel(entries, w, h)
	s.mode = modeHistory
	return s, nil
}

// update handles one key. While the filter input is active every key (including
// enter, which applies the filter) belongs to the list; only once filtering has
// settled does esc cancel and enter pick — the same two-stage flow as the picker.
func (m historyModel) update(msg tea.KeyPressMsg) (historyModel, historyResult, tea.Cmd) {
	if !m.list.SettingFilter() {
		switch msg.String() {
		case "esc":
			return m, historyCanceled, nil
		case "enter":
			return m, historyPicked, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, historyPending, cmd
}

// selected returns the highlighted statement, or "" when the list is empty.
func (m historyModel) selected() (string, bool) {
	if it, ok := m.list.SelectedItem().(historyItem); ok {
		return it.sql, true
	}
	return "", false
}

// settingFilter reports whether the filter text input is currently capturing
// keys, so the dashboard can tell the root to leave digits for it (CapturesText).
func (m historyModel) settingFilter() bool { return m.list.SettingFilter() }

func (m historyModel) View(width int) string {
	return styles.Screen.Render(m.list.View())
}

// firstLine returns the first non-empty line of s, trimmed and collapsed to a
// single spaced line, so a multi-line statement reads as one list row.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return strings.Join(strings.Fields(t), " ")
		}
	}
	return strings.TrimSpace(s)
}

// relativeTime renders a coarse "how long ago" from a Unix timestamp for the
// history list. It stays coarse on purpose — the user wants "recent vs old", not
// a clock.
func relativeTime(unix int64) string {
	if unix == 0 {
		return ""
	}
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
}
