package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestTruncateFlow covers v3 2.3: [T] shows a confirm with the TRUNCATE and enter
// runs it.
func TestTruncateFlow(t *testing.T) {
	s, eng := previewing(t)

	s, _ = press(s, tea.KeyPressMsg{Code: 'T', Text: "T"})
	if s.mode != modeConfirmSQL {
		t.Fatalf("truncate should open a confirm, mode=%d", s.mode)
	}
	if s.confirmSQL.sql != `TRUNCATE TABLE "t"` {
		t.Fatalf("generated statement: %s", s.confirmSQL.sql)
	}
	if !strings.Contains(s.View(120, 40), "TRUNCATE TABLE") {
		t.Error("the TRUNCATE should be shown before it runs")
	}

	s2, _ := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s2.mode != modeBrowse || !s2.working {
		t.Fatalf("enter should run; mode=%d working=%v", s2.mode, s2.working)
	}
	runCmd(t, execMutationCmd(eng, "postgres", s.confirmSQL.sql, "note"))
	if eng.lastMutationSQL != `TRUNCATE TABLE "t"` {
		t.Errorf("executed: %s", eng.lastMutationSQL)
	}
}

// TestRowCount covers the exact COUNT(*): [#] marks work in flight and runs the
// COUNT, and the reply reports the total.
func TestRowCount(t *testing.T) {
	s, eng := previewing(t)

	s, _ = press(s, tea.KeyPressMsg{Code: '#', Text: "#"})
	if !s.working {
		t.Fatal("count should mark work in flight")
	}
	// The count command runs a COUNT(*) via ExecMutation.
	if _, ok := runCmd(t, countRowsCmd(eng, "postgres", "t")).(countMsg); !ok {
		t.Fatal("countRowsCmd should return a countMsg")
	}
	if !strings.Contains(eng.lastMutationSQL, "COUNT(*)") {
		t.Errorf("count SQL not run: %s", eng.lastMutationSQL)
	}

	s = feed(s, countMsg{table: "t", n: 42})
	if s.working {
		t.Error("count reply should clear the in-flight flag")
	}
	if !strings.Contains(s.notice, "42 row") {
		t.Errorf("count notice: %q", s.notice)
	}
}

// TestFilterApplies covers the quick WHERE bar: [/] opens it, a condition applies
// and runs a filtered SELECT, and it shows in the active filter state.
func TestFilterApplies(t *testing.T) {
	s, eng := previewing(t)

	s, _ = press(s, tea.KeyPressMsg{Code: '/', Text: "/"})
	if s.mode != modeFilter {
		t.Fatalf("filter bar should open, mode=%d", s.mode)
	}
	s = typeInto(s, "id > 5")
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.mode != modeBrowse {
		t.Fatalf("apply should close the bar, mode=%d", s.mode)
	}
	if s.activeFilter != "id > 5" || s.activeFilterTable != "t" {
		t.Fatalf("filter not recorded: %q on %q", s.activeFilter, s.activeFilterTable)
	}
	// The applied filter runs a WHERE-filtered SELECT (delivered as a row load).
	if _, ok := runCmd(t, filteredPreviewCmd(eng, "postgres", "t", s.activeFilter)).(rowsLoadedMsg); !ok {
		t.Fatal("filteredPreviewCmd should deliver rows")
	}
	if !strings.Contains(eng.lastMutationSQL, "WHERE id > 5") {
		t.Errorf("filtered SELECT not run: %s", eng.lastMutationSQL)
	}
}

// TestFilterClearedOnTableSwitch: selecting a different table drops the filter.
func TestFilterClearedOnTableSwitch(t *testing.T) {
	s, _ := previewing(t)
	s.activeFilter, s.activeFilterTable = "id > 5", "t"
	s.focus = focusTables
	// Selecting a table clears the filter (previewing() seeds tables in "postgres").
	if len(s.tables) == 0 {
		t.Skip("no tables seeded")
	}
	s.tblCursor = 0
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.activeFilter != "" {
		t.Errorf("switching tables should clear the filter, got %q", s.activeFilter)
	}
}
