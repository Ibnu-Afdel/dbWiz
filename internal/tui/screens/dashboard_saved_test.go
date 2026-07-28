package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// saved-query open keys (v2 2.2): alt+s opens the picker, alt+w saves.
var (
	altS = tea.KeyPressMsg{Code: 's', Mod: tea.ModAlt}
	altW = tea.KeyPressMsg{Code: 'w', Mod: tea.ModAlt}
)

// typeText presses each rune of s in turn, driving whatever text input currently
// has focus (used to fill the save form's name field).
func typeText(s dashboardScreen, text string) dashboardScreen {
	for _, r := range text {
		s, _ = press(s, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return s
}

// saveAs drives the whole save flow from the editor: alt+w, type the name,
// optionally flip the global toggle, then enter. It runs the batched persist so
// the write actually lands in the (hermetic) store.
func saveAs(t *testing.T, s dashboardScreen, name string, global bool) dashboardScreen {
	t.Helper()
	s, _ = press(s, altW)
	if s.mode != modeForm || s.formPurpose != purposeSaveQuery {
		t.Fatalf("alt+w should open the save form, mode=%d purpose=%d", s.mode, s.formPurpose)
	}
	s = typeText(s, name)
	if global {
		// The name field is first and focused; tab down to the toggle, space it on.
		s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyTab})
		s, _ = press(s, tea.KeyPressMsg{Code: ' ', Text: " "})
	}
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	runAll(cmd)
	return s
}

// seedSavedStore records named queries for a scope directly in the (hermetic)
// store so the picker has something to show without driving the save form.
func seedSavedStore(t *testing.T, key string, pairs ...[2]string) {
	t.Helper()
	for _, p := range pairs {
		if err := state.AddSaved(key, p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSaveQueryOpensForm covers 2.2: alt+w in the editor opens the save form.
func TestSaveQueryOpensForm(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "SELECT * FROM customers")
	s, _ = press(s, altW)

	if s.mode != modeForm || s.formPurpose != purposeSaveQuery {
		t.Fatalf("alt+w should open the save form, mode=%d purpose=%d", s.mode, s.formPurpose)
	}
	if !strings.Contains(s.View(120, 40), "Save query") {
		t.Errorf("save form should render its title:\n%s", s.View(120, 40))
	}
}

// TestSaveEmptyEditorNotice covers 2.2: alt+w with nothing to save nudges the
// user instead of opening an empty form.
func TestSaveEmptyEditorNotice(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"}) // focus editor, empty
	s, _ = press(s, altW)

	if s.mode != modeBrowse {
		t.Errorf("empty editor should not open the save form, mode=%d", s.mode)
	}
	if !strings.Contains(s.View(120, 40), "Nothing to save") {
		t.Errorf("empty save should show a notice:\n%s", s.View(120, 40))
	}
}

// TestSavePersistsPerTarget covers 2.2: completing the save form writes the named
// query to the target's scope with the editor's statement.
func TestSavePersistsPerTarget(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "SELECT * FROM orders")
	s = saveAs(t, s, "orders", false)

	got := state.Saved(s.historyKey)
	if len(got) != 1 || got[0].Name != "orders" || got[0].SQL != "SELECT * FROM orders" {
		t.Fatalf("per-target save = %+v", got)
	}
	if len(state.GlobalSaved()) != 0 {
		t.Errorf("a per-target save should not land in the global scope")
	}
	if s.mode != modeBrowse {
		t.Errorf("save should return to browse, mode=%d", s.mode)
	}
	if got := s.editor.Value(); got != "SELECT * FROM orders" {
		t.Errorf("save should leave the editor untouched, got %q", got)
	}
}

// TestSavePersistsGlobal covers 2.2's global scope: flipping the toggle stores
// the query where every target can pick it.
func TestSavePersistsGlobal(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "SELECT version()")
	s = saveAs(t, s, "version", true)

	if g := state.GlobalSaved(); len(g) != 1 || g[0].Name != "version" {
		t.Fatalf("global save = %+v", g)
	}
	if len(state.Saved(s.historyKey)) != 0 {
		t.Errorf("a global save should not land in the per-target scope")
	}
}

// TestSaveRedactsPasswordOnDisk covers the password-hygiene guarantee for saved
// queries: a PASSWORD literal is masked before it reaches the store.
func TestSaveRedactsPasswordOnDisk(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editing(s, "CREATE USER app WITH PASSWORD 'hunter2'")
	s = saveAs(t, s, "make-app", false)

	got := state.Saved(s.historyKey)
	if len(got) == 0 {
		t.Fatal("query should have been saved")
	}
	if strings.Contains(got[0].SQL, "hunter2") {
		t.Errorf("saved query must not contain the password: %q", got[0].SQL)
	}
}

// TestSavedPickerListsBothScopes covers 2.2: alt+s opens the picker showing the
// global queries and this target's own.
func TestSavedPickerListsBothScopes(t *testing.T) {
	s, _ := newPGDashboard(t)
	seedSavedStore(t, "", [2]string{"everywhere", "SELECT 1"})
	seedSavedStore(t, s.historyKey, [2]string{"here", "SELECT 2"})

	s, _ = press(s, altS)
	if s.mode != modeSaved {
		t.Fatalf("alt+s should open the saved picker, mode=%d", s.mode)
	}
	view := s.View(120, 40)
	for _, want := range []string{"Saved queries", "everywhere", "here", scopeGlobal, scopeTarget} {
		if !strings.Contains(view, want) {
			t.Errorf("saved picker missing %q\n%s", want, view)
		}
	}
}

// TestSavedPickerEmptyShowsNotice covers 2.2: with nothing saved, alt+s shows a
// notice pointing at the save key and stays in browse mode.
func TestSavedPickerEmptyShowsNotice(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = press(s, altS)

	if s.mode != modeBrowse {
		t.Errorf("empty saved list should not open an overlay, mode=%d", s.mode)
	}
	if !strings.Contains(s.View(120, 40), "No saved queries yet") {
		t.Errorf("empty saved list should show a notice:\n%s", s.View(120, 40))
	}
}

// TestSavedPickLoadsIntoEditor covers 2.2: Enter loads the highlighted query into
// the editor (focused) and never auto-runs it — matching history's behaviour.
func TestSavedPickLoadsIntoEditor(t *testing.T) {
	s, _ := newPGDashboard(t)
	seedSavedStore(t, s.historyKey, [2]string{"orders", "SELECT * FROM orders"})

	s, _ = press(s, altS)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})

	if s.mode != modeBrowse {
		t.Errorf("Enter should close the picker, mode=%d", s.mode)
	}
	if s.focus != focusEditor {
		t.Errorf("Enter should focus the editor, focus=%d", s.focus)
	}
	if got := s.editor.Value(); got != "SELECT * FROM orders" {
		t.Errorf("editor = %q, want the picked query", got)
	}
	if s.querying {
		t.Error("picking a saved query must not run it")
	}
}

// TestSavedDeleteRemovesEntry covers 2.2: 'd' forgets the highlighted query; the
// refreshed list drops the row while a second saved query remains.
func TestSavedDeleteRemovesEntry(t *testing.T) {
	s, _ := newPGDashboard(t)
	seedSavedStore(t, s.historyKey,
		[2]string{"keep", "SELECT 1"},
		[2]string{"drop", "SELECT 2"})

	s, _ = press(s, altS)
	// "drop" is newest, so it's highlighted first.
	s, cmd := press(s, tea.KeyPressMsg{Code: 'd', Text: "d"})
	s = feed(s, cmd()) // resolve the delete → savedDeletedMsg rebuild

	if s.mode != modeSaved {
		t.Errorf("overlay should stay open while entries remain, mode=%d", s.mode)
	}
	got := state.Saved(s.historyKey)
	if len(got) != 1 || got[0].Name != "keep" {
		t.Fatalf("delete removed the wrong entry: %+v", got)
	}
	if strings.Contains(s.View(120, 40), "drop") {
		t.Errorf("deleted query should vanish from the list:\n%s", s.View(120, 40))
	}
}

// TestSavedDeleteLastClosesOverlay covers 2.2: deleting the only saved query
// closes the picker (nothing left to show) with a notice.
func TestSavedDeleteLastClosesOverlay(t *testing.T) {
	s, _ := newPGDashboard(t)
	seedSavedStore(t, s.historyKey, [2]string{"only", "SELECT 1"})

	s, _ = press(s, altS)
	s, cmd := press(s, tea.KeyPressMsg{Code: 'd', Text: "d"})
	s = feed(s, cmd())

	if s.mode != modeBrowse {
		t.Errorf("deleting the last entry should close the picker, mode=%d", s.mode)
	}
	if len(state.Saved(s.historyKey)) != 0 {
		t.Errorf("the last saved query should be gone")
	}
}

// TestSavedFilterCapturesText covers 2.2's searchability: '/' starts the filter,
// which must capture text so the root doesn't steal digits as tab-switches.
func TestSavedFilterCapturesText(t *testing.T) {
	s, _ := newPGDashboard(t)
	seedSavedStore(t, s.historyKey,
		[2]string{"users", "SELECT 1"},
		[2]string{"orders", "SELECT 2"})

	s, _ = press(s, altS)
	if s.CapturesText() {
		t.Fatal("saved picker should not capture text before filtering starts")
	}
	s, _ = press(s, tea.KeyPressMsg{Code: '/', Text: "/"})
	if !s.savedList.settingFilter() {
		t.Fatal("'/' should start the saved filter")
	}
	if !s.CapturesText() {
		t.Error("an active saved filter must capture text")
	}
}
