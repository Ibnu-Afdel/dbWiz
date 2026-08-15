package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
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

// --- v5 3.1: background whole-schema column prefetch ---

// TestPrefetchWarmsColumnCacheForUnbrowsedTable covers 3.1: after a table
// list loads, both tables' columns are cached without either ever having
// been browsed or described directly.
func TestPrefetchWarmsColumnCacheForUnbrowsedTable(t *testing.T) {
	s, eng := newPGDashboard(t)
	s.currentDB = "appdb"
	eng.columnsByTable = map[string][]db.Column{
		"users":  {{Name: "id"}, {Name: "email"}},
		"orders": {{Name: "id"}, {Name: "total"}},
	}

	next, cmd := s.Update(tablesLoadedMsg{database: "appdb", tables: eng.tables["appdb"]})
	s = next.(dashboardScreen)
	s = feed(s, runCmd(t, cmd))

	if cols := s.columnCache["users"]; len(cols) != 2 {
		t.Fatalf("prefetch should have cached users' columns unbrowsed, got %+v", cols)
	}
	if cols := s.columnCache["orders"]; len(cols) != 2 {
		t.Fatalf("prefetch should have cached orders' columns unbrowsed, got %+v", cols)
	}
}

// TestPrefetchSkipsAlreadyCachedTables covers 3.1: a table already in the
// cache (browsed, described, or an earlier prefetch) isn't re-described —
// repeat loads of the same database (Keys.Refresh) don't cost extra.
func TestPrefetchSkipsAlreadyCachedTables(t *testing.T) {
	s, eng := newPGDashboard(t)
	s.currentDB = "appdb"
	eng.columnsByTable = map[string][]db.Column{
		"users":  {{Name: "id"}},
		"orders": {{Name: "id"}},
	}
	s.cacheColumns("users", []string{"id", "email"}) // already cached, e.g. from browsing

	next, cmd := s.Update(tablesLoadedMsg{database: "appdb", tables: eng.tables["appdb"]})
	s = next.(dashboardScreen)
	_ = feed(s, runCmd(t, cmd))

	if eng.describeCalls != 1 {
		t.Fatalf("expected exactly one DescribeTable call (orders only), got %d", eng.describeCalls)
	}
	if eng.lastDescribeTbl != "orders" {
		t.Errorf("expected the uncached table to be described, got %q", eng.lastDescribeTbl)
	}
}

// TestPrefetchDropsStaleReply covers 3.1's staleness guard: a prefetch reply
// for a database the user has since left is discarded, not merged.
func TestPrefetchDropsStaleReply(t *testing.T) {
	s, _ := newPGDashboard(t) // currentDB stays "postgres"
	s = feed(s, schemaPrefetchMsg{database: "appdb", columns: map[string][]string{"users": {"id"}}})
	if _, ok := s.columnCache["users"]; ok {
		t.Error("a prefetch reply for a database we've left should be dropped")
	}
}

// --- v5 3.2: qualified table.column completion ---

// appdbWithColumns builds a dashboard on "appdb" (users, orders) with the fake
// engine ready to describe either table distinctly, for the qualified-
// completion tests.
func appdbWithColumns(t *testing.T) (dashboardScreen, *fakeEngine) {
	t.Helper()
	s, eng := newPGDashboard(t)
	s.currentDB = "appdb"
	s.tables = eng.tables["appdb"]
	eng.columnsByTable = map[string][]db.Column{
		"users":  {{Name: "id"}, {Name: "email"}},
		"orders": {{Name: "id"}, {Name: "total"}},
	}
	return s, eng
}

// TestQualifiedCompletionNarrowsToOneTable covers 3.2's cached path: with
// both tables' columns already cached, "orders.t" offers only orders' "total"
// — not users' columns, not table names, not keywords (which "t" alone would
// also match, e.g. TABLE).
func TestQualifiedCompletionNarrowsToOneTable(t *testing.T) {
	s, _ := appdbWithColumns(t)
	s.cacheColumns("users", []string{"id", "email"})
	s.cacheColumns("orders", []string{"id", "total"})
	s = editorWith(s, "select orders.t")

	s, _ = press(s, ctrlSpace)
	if s.mode != modeComplete {
		t.Fatalf("expected the completion picker to open immediately (cached), mode=%d", s.mode)
	}
	view := s.View(120, 40)
	if !strings.Contains(view, "total") {
		t.Errorf("expected orders.total offered:\n%s", view)
	}
	if strings.Contains(view, "TABLE") {
		t.Errorf("qualified completion should not fall back to keywords:\n%s", view)
	}

	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := s.editor.Value(); got != "select orders.total" {
		t.Errorf("accepting should replace only the fragment after the dot, got %q", got)
	}
}

// TestQualifiedCompletionFetchesUncachedTable covers 3.2's fallback path: a
// table the prefetch/cache hasn't reached yet is described on demand — one
// round trip — and the picker opens once that reply lands.
func TestQualifiedCompletionFetchesUncachedTable(t *testing.T) {
	s, eng := appdbWithColumns(t)
	s = editorWith(s, "select orders.t")

	s, cmd := press(s, ctrlSpace)
	if s.mode == modeComplete {
		t.Fatal("the picker shouldn't open before the on-demand describe returns")
	}
	if cmd == nil {
		t.Fatal("expected a describe command for the uncached table")
	}
	s = feed(s, runCmd(t, cmd))

	if s.mode != modeComplete {
		t.Fatalf("expected the picker to open once the describe lands, mode=%d", s.mode)
	}
	if !strings.Contains(s.View(120, 40), "total") {
		t.Errorf("expected orders.total offered:\n%s", s.View(120, 40))
	}
	if eng.lastDescribeTbl != "orders" {
		t.Errorf("expected orders to be described on demand, got %q", eng.lastDescribeTbl)
	}
}

// TestQualifiedCompletionFallsBackWhenQualifierUnknown covers 3.2's other
// edge: a qualifier that isn't a real table (schema-qualified name, decimal
// fraction, ...) falls back to plain unqualified completion instead of an
// empty box.
func TestQualifiedCompletionFallsBackWhenQualifierUnknown(t *testing.T) {
	s, _ := appdbWithColumns(t)
	s = editorWith(s, "nope.se") // "nope" matches no known table

	s, _ = press(s, ctrlSpace)
	if s.mode != modeComplete {
		t.Fatalf("expected the ordinary keyword completion to still open, mode=%d", s.mode)
	}
	if !strings.Contains(s.View(120, 40), "SELECT") {
		t.Errorf("expected the unqualified fallback to offer SELECT:\n%s", s.View(120, 40))
	}
}

// --- v5 3.3: engine-aware function candidates ---

// TestFunctionCandidatesAreEngineAware covers 3.3: Postgres offers a Postgres
// function (COALESCE) and not a MySQL-only one (DATE_FORMAT); a SQLite engine
// offers STRFTIME instead.
func TestFunctionCandidatesAreEngineAware(t *testing.T) {
	s, _ := newPGDashboard(t) // fake engine reports KindPostgres
	got := s.completionCandidates("coal")
	if len(got) == 0 || got[0].text != "COALESCE" || got[0].kind != "function" {
		t.Fatalf("expected COALESCE offered as a function for Postgres, got %+v", got)
	}
	if got := s.completionCandidates("date_format"); len(got) != 0 {
		t.Errorf("DATE_FORMAT is MySQL-only, should not be offered on Postgres: %+v", got)
	}

	eng := &fakeEngine{caps: db.Capabilities{MultipleDatabases: false}} // Kind() → SQLite
	sq := NewDashboard(eng, db.Target{Database: "test.db"}, docker.Container{Name: "test"}).(dashboardScreen)
	got = sq.completionCandidates("strf")
	if len(got) == 0 || got[0].text != "STRFTIME" || got[0].kind != "function" {
		t.Fatalf("expected STRFTIME offered as a function for SQLite, got %+v", got)
	}
}
