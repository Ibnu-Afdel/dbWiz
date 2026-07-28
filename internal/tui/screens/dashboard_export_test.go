package screens

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// results-export / clipboard keys (v2 2.3).
var altE = tea.KeyPressMsg{Code: 'e', Mod: tea.ModAlt}

// withQueryResult drops a query result into the pane and focuses the results
// grid, so export/copy have a real grid to act on.
func withQueryResult(s dashboardScreen, cols []string, rows [][]any) dashboardScreen {
	s = feed(s, queryDoneMsg{seq: s.querySeq, verb: "SELECT", result: db.Result{Columns: cols, Rows: rows}})
	s.focus = focusResults
	return s
}

// TestExportChooserOpens covers 2.3: alt+e over a real result opens the CSV/JSON
// chooser with the row count.
func TestExportChooserOpens(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = withQueryResult(s, []string{"id"}, [][]any{{int64(1)}, {int64(2)}})
	s, _ = press(s, altE)

	if s.mode != modeExport {
		t.Fatalf("alt+e should open the export chooser, mode=%d", s.mode)
	}
	view := s.View(120, 40)
	for _, want := range []string{"Export results", "CSV", "JSON", "2 rows"} {
		if !strings.Contains(view, want) {
			t.Errorf("export chooser missing %q\n%s", want, view)
		}
	}
}

// TestExportEmptyShowsNotice covers 2.3: with no result to export, alt+e nudges
// the user instead of opening an empty chooser.
func TestExportEmptyShowsNotice(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = press(s, altE)

	if s.mode != modeBrowse {
		t.Errorf("empty export should not open a chooser, mode=%d", s.mode)
	}
	if !strings.Contains(s.View(120, 40), "Nothing to export") {
		t.Errorf("empty export should show a notice:\n%s", s.View(120, 40))
	}
}

// TestExportWritesCSV covers 2.3: choosing CSV writes a timestamped file into the
// working directory and reports its path, with the header and rows intact.
func TestExportWritesCSV(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	s, _ := newPGDashboard(t)
	s = withQueryResult(s, []string{"id", "name"}, [][]any{{int64(1), "alice"}})
	s, _ = press(s, altE)
	// CSV is the default highlight — enter exports it.
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	s = feed(s, cmd()) // run the write cmd, feed exportDoneMsg back

	files := globExt(t, dir, ".csv")
	if len(files) != 1 {
		t.Fatalf("want one .csv written, got %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "id,name") || !strings.Contains(string(data), "alice") {
		t.Errorf("csv content wrong:\n%s", data)
	}
	if !strings.Contains(s.View(120, 40), "Exported 1 rows") {
		t.Errorf("export should toast the outcome:\n%s", s.View(120, 40))
	}
}

// TestExportWritesJSON covers 2.3: moving the cursor to JSON and confirming writes
// a .json file.
func TestExportWritesJSON(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	s, _ := newPGDashboard(t)
	s = withQueryResult(s, []string{"id"}, [][]any{{int64(7)}})
	s, _ = press(s, altE)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown}) // CSV → JSON
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	s = feed(s, cmd())

	files := globExt(t, dir, ".json")
	if len(files) != 1 {
		t.Fatalf("want one .json written, got %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), `"id": 7`) {
		t.Errorf("json content wrong:\n%s", data)
	}
}

// TestExportCancel covers 2.3: esc closes the chooser without writing anything.
func TestExportCancel(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	s, _ := newPGDashboard(t)
	s = withQueryResult(s, []string{"id"}, [][]any{{int64(1)}})
	s, _ = press(s, altE)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc})

	if s.mode != modeBrowse {
		t.Errorf("esc should close the chooser, mode=%d", s.mode)
	}
	if got := globExt(t, dir, ".csv"); len(got) != 0 {
		t.Errorf("cancel must not write a file, got %v", got)
	}
}

// TestCopyCellYanksToClipboard covers 2.3: y in the results pane emits an OSC 52
// clipboard set for the selected cell and toasts the copy.
func TestCopyCellYanksToClipboard(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = withQueryResult(s, []string{"id", "name"}, [][]any{{int64(1), "alice"}})
	// Cursor starts at row0/col0 = id "1".
	s, cmd := press(s, tea.KeyPressMsg{Code: 'y', Text: "y"})

	if cmd == nil || !isClipboard(cmd(), "1") {
		t.Errorf("y should copy the selected cell value to the clipboard")
	}
	if !strings.Contains(s.View(120, 40), "Copied cell") {
		t.Errorf("copy should toast:\n%s", s.View(120, 40))
	}
}

// TestCopyRowYanksTabSeparated covers 2.3: Y copies the whole row, tab-separated.
func TestCopyRowYanksTabSeparated(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = withQueryResult(s, []string{"id", "name"}, [][]any{{int64(1), "alice"}})
	s, cmd := press(s, tea.KeyPressMsg{Code: 'Y', Text: "Y"})

	if cmd == nil || !isClipboard(cmd(), "1\talice") {
		t.Errorf("Y should copy the whole row tab-separated")
	}
}

// TestCopyIgnoredOutsideResults covers 2.3's guard: y/Y do nothing when the
// results pane isn't focused, so the bare letters stay harmless elsewhere.
func TestCopyIgnoredOutsideResults(t *testing.T) {
	s, _ := newPGDashboard(t) // starts on a browse pane (databases)
	s = feed(s, queryDoneMsg{seq: s.querySeq, verb: "SELECT", result: db.Result{Columns: []string{"id"}, Rows: [][]any{{int64(1)}}}})
	// focus is NOT results here.
	_, cmd := press(s, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Errorf("y off the results pane should be a no-op")
	}
}

// --- helpers ---

// chdir switches to dir for the test and restores the old cwd after.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// globExt lists files in dir with the given extension.
func globExt(t *testing.T, dir, ext string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"+ext))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// isClipboard reports whether msg is a bubbletea clipboard-set carrying want.
// tea.SetClipboard yields an unexported string-typed message, so the test matches
// on its formatted value rather than the concrete type.
func isClipboard(msg tea.Msg, want string) bool {
	return fmt.Sprintf("%s", msg) == want
}
