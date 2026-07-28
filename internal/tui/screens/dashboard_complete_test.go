package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// ctrlSpace is the autocomplete trigger (v2 2.5).
var ctrlSpace = tea.KeyPressMsg{Code: ' ', Mod: tea.ModCtrl}

// editorWith focuses the editor and loads sql with the cursor at the end, ready
// to trigger completion on the trailing prefix.
func editorWith(s dashboardScreen, sql string) dashboardScreen {
	s, _ = press(s, tea.KeyPressMsg{Code: 'e', Text: "e"})
	s.editor.SetValue(sql)
	s.editor.MoveToEnd()
	return s
}

// TestCompleteMatchesTablePrefix covers 2.5: ctrl+space offers table names that
// match the word under the cursor (the fake pg engine exposes users/orders).
func TestCompleteMatchesTablePrefix(t *testing.T) {
	s, _ := newPGDashboard(t)
	// newPGDashboard seeds tables for "postgres"; confirm at least one exists.
	if len(s.tables) == 0 {
		t.Skip("fake engine exposes no tables to complete")
	}
	name := s.tables[0].Name
	s = editorWith(s, "select * from "+name[:1])
	s, _ = press(s, ctrlSpace)

	if s.mode != modeComplete {
		t.Fatalf("ctrl+space should open the completion picker, mode=%d", s.mode)
	}
	if !strings.Contains(s.View(120, 40), name) {
		t.Errorf("completion should offer table %q\n%s", name, s.View(120, 40))
	}
}

// TestCompleteInsertsSelection covers 2.5: Enter replaces the prefix with the
// chosen candidate and returns to the editor.
func TestCompleteInsertsSelection(t *testing.T) {
	s, _ := newPGDashboard(t)
	// Cache a known column so the candidate set is deterministic.
	s.cacheColumns("customers", []string{"customer_id"})
	s = editorWith(s, "select customer_")
	s, _ = press(s, ctrlSpace)
	if s.mode != modeComplete {
		t.Fatalf("expected the completion picker, mode=%d", s.mode)
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})

	if s.mode != modeBrowse {
		t.Errorf("accepting should close the picker, mode=%d", s.mode)
	}
	if got := s.editor.Value(); got != "select customer_id" {
		t.Errorf("completion should replace the prefix, got %q", got)
	}
}

// TestCompleteKeywords covers 2.5: with no schema prefix match, SQL keywords are
// still offered (e.g. "sel" → SELECT).
func TestCompleteKeywords(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editorWith(s, "sel")
	s, _ = press(s, ctrlSpace)
	if s.mode != modeComplete {
		t.Fatalf("expected the completion picker, mode=%d", s.mode)
	}
	if !strings.Contains(s.View(120, 40), "SELECT") {
		t.Errorf("keyword completion should offer SELECT\n%s", s.View(120, 40))
	}
}

// TestCompleteNoMatchNotice covers 2.5: a prefix that matches nothing shows a
// notice rather than an empty picker.
func TestCompleteNoMatchNotice(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editorWith(s, "zzznope")
	s, _ = press(s, ctrlSpace)

	if s.mode != modeBrowse {
		t.Errorf("no candidates should not open a picker, mode=%d", s.mode)
	}
	if !strings.Contains(s.View(120, 40), "No completions") {
		t.Errorf("expected a no-completions notice:\n%s", s.View(120, 40))
	}
}

// TestCompleteCancel covers 2.5: esc closes the picker without touching the text.
func TestCompleteCancel(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = editorWith(s, "sel")
	s, _ = press(s, ctrlSpace)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc})

	if s.mode != modeBrowse {
		t.Errorf("esc should close the picker, mode=%d", s.mode)
	}
	if got := s.editor.Value(); got != "sel" {
		t.Errorf("cancel should leave the text unchanged, got %q", got)
	}
}

// TestCacheColumnsFromDescribe covers 2.5: a describe result warms the column
// cache, making those columns available to autocomplete.
func TestCacheColumnsFromDescribe(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.resultsTable = "widgets" // the describe reply only applies to the current table
	s = feed(s, describeLoadedMsg{
		database: s.currentDB,
		table:    "widgets",
		columns:  []db.Column{{Name: "widget_id"}, {Name: "widget_name"}},
	})
	got := s.completionCandidates("widget_")
	if len(got) < 2 {
		t.Fatalf("describe should cache both columns for completion, got %+v", got)
	}
	names := got[0].text + " " + got[1].text
	if !strings.Contains(names, "widget_id") || !strings.Contains(names, "widget_name") {
		t.Errorf("cached columns missing from candidates: %+v", got)
	}
}
