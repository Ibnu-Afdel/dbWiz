package screens

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"

	"github.com/Ibnu-Afdel/dbwiz/internal/state"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// scopeGlobal / scopeTarget label a saved query's reach in the picker and let the
// dashboard route a delete to the right store scope. "this target" reads clearly
// in the list; the empty state key is what maps it back to global (see AddSaved).
const (
	scopeGlobal = "global"
	scopeTarget = "this target"
)

// savedQueryItem adapts one named query to the list's DefaultItem shape. Title is
// the name (what you pick by); Description shows the scope and the statement's
// first line so you can tell two similarly-named queries apart; FilterValue spans
// the name and the SQL so the fuzzy filter finds a query by either.
type savedQueryItem struct {
	name  string
	sql   string
	scope string
	at    int64
}

func (i savedQueryItem) Title() string       { return i.name }
func (i savedQueryItem) FilterValue() string { return i.name + " " + i.sql }
func (i savedQueryItem) Description() string {
	return i.scope + " · " + firstLine(i.sql)
}

// savedResult is what a key press did to the saved-queries overlay.
type savedResult int

const (
	savedPending  savedResult = iota // handled internally (nav / filter typing)
	savedCanceled                    // esc — close without changing the editor
	savedPicked                      // enter — load the highlighted query
	savedDelete                      // d/x — forget the highlighted query
)

// savedModel is the searchable, fuzzy-filtered saved-query picker (v2 2.2). Like
// the history overlay it wraps bubbles/list to reuse its type-to-filter, and is
// rebuilt from the store each time it opens so it always reflects the latest
// saves (including ones made this session).
type savedModel struct {
	list list.Model
}

// newSavedModel builds the overlay from the collected items (global first, then
// this target's), each newest-first — the order the list should show.
func newSavedModel(items []savedQueryItem, width, height int) savedModel {
	li := make([]list.Item, len(items))
	for i, it := range items {
		li[i] = it
	}
	l := list.New(li, list.NewDefaultDelegate(), width, height)
	l.Title = "Saved queries"
	l.SetShowHelp(false) // the app renders its own help bar
	l.SetShowStatusBar(false)
	return savedModel{list: l}
}

// collectSaved gathers a target's pickable queries: the global scope first
// (usable everywhere), then this target's own, each newest-first as the store
// returns them. It reads the store, so it belongs off the Update goroutine —
// callers invoke it from a tea.Cmd or the overlay-open path (a user action, not a
// hot loop).
func collectSaved(key string) []savedQueryItem {
	var items []savedQueryItem
	for _, q := range state.GlobalSaved() {
		items = append(items, savedQueryItem{name: q.Name, sql: q.SQL, scope: scopeGlobal, at: q.At})
	}
	for _, q := range state.Saved(key) {
		items = append(items, savedQueryItem{name: q.Name, sql: q.SQL, scope: scopeTarget, at: q.At})
	}
	return items
}

// openSaved opens the picker over this target's saved queries (alt+s). With none
// saved yet it shows a brief notice pointing at the save key instead of an empty
// box, so the key always does something legible.
func (s dashboardScreen) openSaved() (dashboardScreen, tea.Cmd) {
	items := collectSaved(s.historyKey)
	if len(items) == 0 {
		s.notice, s.noticeErr = "No saved queries yet — write a statement and press ⌥w to save it.", false
		return s, nil
	}
	w, h := s.savedOverlaySize()
	s.savedList = newSavedModel(items, w, h)
	s.mode = modeSaved
	return s, nil
}

// savedOverlaySize sizes the picker to the window, matching the history overlay's
// bounds so both overlays feel the same.
func (s dashboardScreen) savedOverlaySize() (int, int) {
	return clamp(s.width-8, 20, 100), clamp(s.height-8, 5, 24)
}

// update handles one key. While the filter input is active every key belongs to
// the list; only once filtering has settled do esc cancel, enter pick, and d/x
// delete — the same two-stage flow as the history overlay.
func (m savedModel) update(msg tea.KeyPressMsg) (savedModel, savedResult, tea.Cmd) {
	if !m.list.SettingFilter() {
		switch msg.String() {
		case "esc":
			return m, savedCanceled, nil
		case "enter":
			return m, savedPicked, nil
		case "d", "x":
			return m, savedDelete, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, savedPending, cmd
}

// selected returns the highlighted saved query, or false when the list is empty.
func (m savedModel) selected() (savedQueryItem, bool) {
	if it, ok := m.list.SelectedItem().(savedQueryItem); ok {
		return it, true
	}
	return savedQueryItem{}, false
}

// settingFilter reports whether the filter input is capturing keys, so the
// dashboard can leave digits for it (CapturesText).
func (m savedModel) settingFilter() bool { return m.list.SettingFilter() }

func (m savedModel) View(width int) string {
	return styles.Screen.Render(m.list.View())
}

// --- saving ---

// newSaveQueryForm builds the little "name this query" overlay (v2 2.2): a name
// field with live "enter a name" validation, plus a toggle that promotes the
// query to the global scope so it's offered on every connection.
func newSaveQueryForm() formModel {
	name := textinput.New()
	name.Prompt = "› "
	name.Placeholder = "top customers"
	name.Focus()

	return formModel{
		title:  "Save query",
		submit: "save",
		fields: []formField{
			{key: "name", label: "Name", kind: fieldText, input: name},
			{key: "global", label: "Available from any target (global)", kind: fieldToggle},
		},
		validate: validateSaveQuery,
	}
}

// validateSaveQuery requires a non-empty name; the SQL comes from the editor, so
// there's nothing else to check here.
func validateSaveQuery(f formModel) (errText, warnText string) {
	if f.value("name") == "" {
		return "enter a name for this query", ""
	}
	return "", ""
}

// openSaveQuery starts the save flow for whatever is in the editor (alt+w). An
// empty editor can't be saved, so it nudges the user instead of opening an empty
// form. It reuses the create-form overlay (modeForm) with the save purpose; the
// SQL is read back from the editor at submit time.
func (s dashboardScreen) openSaveQuery() (dashboardScreen, tea.Cmd) {
	if strings.TrimSpace(s.editor.Value()) == "" {
		s.notice, s.noticeErr = "Nothing to save — write a statement in the editor first.", false
		return s, nil
	}
	s.mode, s.formPurpose, s.notice = modeForm, purposeSaveQuery, ""
	s.form = newSaveQueryForm()
	return s, s.form.Init()
}

// saveQueryFromForm records the editor's statement under the name (and scope) the
// save form collected. It's an instant local write, so — unlike the admin
// mutations — it doesn't flip on the "working…" spinner; the write itself runs
// off the Update goroutine via persistSavedCmd. The editor is left untouched and
// re-focused so the user can keep iterating on the same statement.
func (s dashboardScreen) saveQueryFromForm() (dashboardScreen, tea.Cmd) {
	name := s.form.value("name")
	sql := strings.TrimSpace(s.editor.Value())
	key, scope := s.historyKey, scopeTarget
	if s.form.toggle("global") {
		key, scope = "", scopeGlobal
	}
	s.notice, s.noticeErr = fmt.Sprintf("Saved query %q (%s).", name, scope), false
	return s, tea.Batch(persistSavedCmd(key, name, sql), s.syncEditorFocus())
}
