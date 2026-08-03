package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

var altP = tea.KeyPressMsg{Code: 'p', Mod: tea.ModAlt}

// planFixture is a measured-shaped Postgres plan: a big sequential scan the
// planner badly under-estimated, so one fixture exercises the tree and both
// hints.
const planFixture = `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"orders",` +
	`"Total Cost":1850.0,"Plan Rows":100,"Actual Rows":90000,"Actual Loops":1,` +
	`"Actual Total Time":45.5,"Filter":"(status = 'open'::text)"},` +
	`"Planning Time":0.3,"Execution Time":52.1}]`

// planning puts the dashboard in the editor with a statement typed and the fake
// engine ready to answer with a plan.
func planning(t *testing.T) (dashboardScreen, *fakeEngine) {
	t.Helper()
	s, eng := newPGDashboard(t)
	eng.queryResult = db.Result{Columns: []string{"QUERY PLAN"}, Rows: [][]any{{planFixture}}}
	s, _ = s.focusEditor()
	s.editor.SetValue("SELECT * FROM orders WHERE status = 'open'")
	return s, eng
}

// TestPlanOpensFromEditor covers v4 2.4: ⌥p explains the statement being written
// without leaving the editor.
func TestPlanOpensFromEditor(t *testing.T) {
	s, eng := planning(t)

	s, cmd := press(s, altP)
	if s.mode != modePlan || s.plan.phase != planRunning || !s.working {
		t.Fatalf("⌥p should start a capture; mode=%d phase=%d working=%v", s.mode, s.plan.phase, s.working)
	}
	if cmd == nil {
		t.Fatal("⌥p should return a command that does the capturing")
	}
	if s.plan.analyzed {
		t.Error("the first look must not run the statement")
	}

	msg, ok := planCmd(eng, s.plan.statement, false, s.planSeq)().(planDoneMsg)
	if !ok {
		t.Fatal("planCmd should report a planDoneMsg")
	}
	if msg.err != "" {
		t.Fatalf("capture failed: %s", msg.err)
	}
	if !strings.HasPrefix(eng.lastQuery, "EXPLAIN (FORMAT JSON) ") {
		t.Errorf("ran %q, want a plain EXPLAIN", eng.lastQuery)
	}

	s = feed(s, msg)
	if s.plan.phase != planReport || s.working {
		t.Fatalf("the report should end the capture; phase=%d working=%v", s.plan.phase, s.working)
	}

	view := s.View(120, 40)
	for _, want := range []string{
		"Query plan",
		"estimates only (nothing was executed)",
		"-> Seq Scan on orders  (est 100 rows, cost 1850.00)",
		"a run it and measure",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("report missing %q:\n%s", want, view)
		}
	}
	// The planner expects 100 rows here, so nothing stands out yet — the scan only
	// becomes interesting once measuring shows what it really read.
	if strings.Contains(view, "What stands out") {
		t.Errorf("a small estimated scan should not be flagged:\n%s", view)
	}

	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.mode != modeBrowse {
		t.Errorf("esc should close the plan, mode=%d", s.mode)
	}
}

// TestPlanOpensFromBrowsePane: the statement lives in the editor, but the key
// works from anywhere on the dashboard, like export and history.
func TestPlanOpensFromBrowsePane(t *testing.T) {
	s, _ := planning(t)
	s.focus = focusTables

	s, _ = press(s, altP)
	if s.mode != modePlan {
		t.Fatalf("⌥p should work from a browse pane, mode=%d", s.mode)
	}
	if s.plan.statement != "SELECT * FROM orders WHERE status = 'open'" {
		t.Errorf("planned %q, want the editor's statement", s.plan.statement)
	}
}

// TestPlanMeasureOnDemand: [a] re-runs the same statement for real, which is what
// turns estimates into measurements.
func TestPlanMeasureOnDemand(t *testing.T) {
	s, eng := planning(t)
	s, _ = press(s, altP)
	s = feed(s, planDoneMsg{seq: s.planSeq, report: "-> Seq Scan on orders\n"})

	before := s.planSeq
	s, cmd := press(s, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !s.plan.analyzed || s.plan.phase != planRunning {
		t.Fatalf("[a] should start a measured capture; analyzed=%v phase=%d", s.plan.analyzed, s.plan.phase)
	}
	if s.planSeq == before {
		t.Error("a re-run must supersede the previous capture")
	}
	if cmd == nil {
		t.Fatal("[a] should return a capture command")
	}

	msg := planCmd(eng, s.plan.statement, true, s.planSeq)().(planDoneMsg)
	if !strings.HasPrefix(eng.lastQuery, "EXPLAIN (FORMAT JSON, ANALYZE) ") {
		t.Errorf("ran %q, want the measured form", eng.lastQuery)
	}

	s = feed(s, msg)
	view := s.View(120, 40)
	for _, want := range []string{
		"measured (the statement was run)",
		"actual 90,000 rows in 45.5 ms",
		// Measuring is what turns the scan into a finding: 90,000 rows read where
		// the planner expected 100.
		"What stands out",
		"Every row of orders is read (90,000 rows)",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("measured report missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "a run it and measure") {
		t.Errorf("a measured report has nothing left to offer:\n%s", view)
	}
}

// TestPlanRefusesToMeasureAWrite: the safety rail reaches the TUI too — the
// refusal shows in the overlay instead of the statement being run.
func TestPlanRefusesToMeasureAWrite(t *testing.T) {
	s, eng := planning(t)
	s.editor.SetValue("DELETE FROM orders WHERE status = 'open'")

	s, _ = press(s, altP)
	s = feed(s, planCmd(eng, s.plan.statement, false, s.planSeq)())
	if s.plan.err != "" {
		t.Fatalf("planning a delete is fine, got %q", s.plan.err)
	}

	eng.lastQuery = ""
	s, _ = press(s, tea.KeyPressMsg{Code: 'a', Text: "a"})
	s = feed(s, planCmd(eng, s.plan.statement, true, s.planSeq)())

	if s.plan.err == "" {
		t.Fatal("measuring a delete must be refused")
	}
	if eng.lastQuery != "" {
		t.Errorf("nothing should have reached the engine, got %q", eng.lastQuery)
	}
	if view := s.View(120, 40); !strings.Contains(view, "Couldn't plan that statement") {
		t.Errorf("the refusal should be shown:\n%s", view)
	}
}

// TestPlanNeedsAStatement: an empty editor gets an explanation, not an empty box.
func TestPlanNeedsAStatement(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = s.focusEditor()

	s, _ = press(s, altP)
	if s.mode == modePlan {
		t.Fatal("there is nothing to plan")
	}
	if !s.noticeErr || !strings.Contains(s.notice, "statement in the editor") {
		t.Errorf("expected an explanatory notice, got %q", s.notice)
	}
}

// TestPlanWaitsForARunningStatement: the editor's connection is busy, so asking
// for a plan explains rather than queueing behind it.
//
// startPlan is called directly because the dashboard already swallows every key
// but cancel while a statement runs — this guards the entry point itself, so the
// rule survives a future caller that isn't the keymap.
func TestPlanWaitsForARunningStatement(t *testing.T) {
	s, _ := planning(t)
	s.querying = true

	s, _ = s.startPlan(false)
	if s.mode == modePlan {
		t.Fatal("a plan must not start while a statement is running")
	}
	if !s.noticeErr || !strings.Contains(s.notice, "still running") {
		t.Errorf("expected an explanatory notice, got %q", s.notice)
	}
}

// TestPlanDropsStaleReport: pressing [a] supersedes the capture in flight, and
// the older report must not land on top of the newer one.
func TestPlanDropsStaleReport(t *testing.T) {
	s, _ := planning(t)
	s, _ = press(s, altP)
	first := s.planSeq
	s = feed(s, planDoneMsg{seq: first, report: "-> Seq Scan on orders\n"})

	s, _ = press(s, tea.KeyPressMsg{Code: 'a', Text: "a"}) // supersedes it
	s = feed(s, planDoneMsg{seq: first, report: "the old estimated plan"})

	if s.plan.phase != planRunning {
		t.Error("a superseded report must be dropped, not shown")
	}
	if strings.Contains(s.View(120, 40), "the old estimated plan") {
		t.Error("the stale report reached the screen")
	}
}

// TestPlanReportSurvivesLeaving: a report arriving after the user closed the
// overlay must not reopen it.
func TestPlanReportSurvivesLeaving(t *testing.T) {
	s, _ := planning(t)
	s, _ = press(s, altP)
	s = feed(s, planDoneMsg{seq: s.planSeq, report: "-> Seq Scan on orders\n"})
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.mode != modeBrowse {
		t.Fatalf("esc should close the report, mode=%d", s.mode)
	}

	s = feed(s, planDoneMsg{seq: s.planSeq, report: "late arrival"})
	if s.mode != modeBrowse {
		t.Errorf("a late report should not reopen the overlay, mode=%d", s.mode)
	}
}

// TestPlanScrolls covers the report's scrolling, which is what makes a deep plan
// readable inside the overlay.
func TestPlanScrolls(t *testing.T) {
	s, _ := planning(t)
	s, _ = press(s, altP)

	long := make([]string, 0, planRows*3)
	for range planRows * 3 {
		long = append(long, "-> step")
	}
	s = feed(s, planDoneMsg{seq: s.planSeq, report: strings.Join(long, "\n")})

	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown})
	if s.plan.offset != 1 {
		t.Errorf("down should scroll by one, offset=%d", s.plan.offset)
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if s.plan.offset != 1+planRows {
		t.Errorf("pgdown should scroll by a page, offset=%d", s.plan.offset)
	}
	for range 10 {
		s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if s.plan.offset > len(s.plan.lines)-planRows {
		t.Errorf("offset %d ran past the end of %d lines", s.plan.offset, len(s.plan.lines))
	}
}

// TestPlanLeavesTheConnectionAlone: unlike the schema comparison, an EXPLAIN is
// just a statement — the editor's database must not move under it.
func TestPlanLeavesTheConnectionAlone(t *testing.T) {
	s, eng := planning(t)
	before := s.currentDB

	s, _ = press(s, altP)
	s = feed(s, planCmd(eng, s.plan.statement, false, s.planSeq)())

	if s.currentDB != before {
		t.Errorf("current database moved from %q to %q", before, s.currentDB)
	}
	// The capture goes through Query, the same path the editor uses, and nothing
	// else — no reconnect, no database switch.
	if !strings.HasPrefix(eng.lastQuery, "EXPLAIN") {
		t.Errorf("the engine saw %q, want only the EXPLAIN", eng.lastQuery)
	}
	if eng.lastListTables != "" || eng.lastMutationSQL != "" {
		t.Errorf("a plan should touch nothing else: listTables=%q mutation=%q", eng.lastListTables, eng.lastMutationSQL)
	}
}
