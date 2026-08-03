package explain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// scan builds a full-scan node of a given size, the shape most hints key off.
func scan(on string, est, act int64) *Node {
	n := newNode("Full scan")
	n.On = on
	n.EstRows, n.ActRows = est, act
	n.note(NoteFullScan)
	return n
}

// TestRenderTree covers v4 2.2: the report's shape — header, indented steps, and
// only the numbers the engine actually reported.
func TestRenderTree(t *testing.T) {
	root := newNode("Hash Join")
	root.EstRows, root.Cost = 850, 42.5
	child := scan("orders", 1200, Unknown)
	child.Cost = 18.5
	child.Detail = "status = 'open'"
	root.Children = []*Node{child}

	out := Render(Plan{
		Kind:      db.KindPostgres,
		Statement: "SELECT *\n  FROM orders",
		Root:      root,
	})

	for _, want := range []string{
		"Plan for: SELECT * FROM orders", // the statement is flattened onto one line
		"PostgreSQL, estimates only (nothing was executed)",
		"-> Hash Join  (est 850 rows, cost 42.50)",
		"    -> Full scan on orders  (est 1,200 rows, cost 18.50)",
		"       status = 'open'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "actual") {
		t.Errorf("an unmeasured plan must not claim actuals:\n%s", out)
	}
}

// TestRenderMeasured covers the measured header, the actual-rows group and the
// whole-statement footer.
func TestRenderMeasured(t *testing.T) {
	root := scan("events", 100, 98000)
	root.Time = millis(12.5)
	root.Loops = 4

	out := Render(Plan{
		Kind:      db.KindPostgres,
		Statement: "SELECT * FROM events",
		Analyzed:  true,
		Root:      root,
		Planning:  millis(0.35),
		Execution: millis(14.2),
	})

	for _, want := range []string{
		"measured (the statement was run)",
		"est 100 rows",
		"actual 98,000 rows in 12.5 ms",
		"4 loops",
		"Total: planning 0.35 ms, execution 14.2 ms",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

// TestRenderSQLiteWithoutNumbers: SQLite reports no measurements at all, and the
// report must simply leave them out rather than print zeroes.
func TestRenderSQLiteWithoutNumbers(t *testing.T) {
	root := newNode("Full scan")
	root.On = "notes"
	root.note(NoteFullScan)

	out := Render(Plan{Kind: db.KindSQLite, Statement: "SELECT * FROM notes", Root: root})

	if strings.Contains(out, "est ") || strings.Contains(out, "cost ") {
		t.Errorf("nothing was reported, so nothing should be printed:\n%s", out)
	}
	if !strings.Contains(out, "-> Full scan on notes\n") {
		t.Errorf("the step should render bare:\n%s", out)
	}
}

// TestHintFullScan is the headline hint: a big table read end to end.
func TestHintFullScan(t *testing.T) {
	p := Plan{Kind: db.KindPostgres, Root: scan("orders", 50000, Unknown)}

	hints := Hints(p)
	if len(hints) != 1 {
		t.Fatalf("got %d hints, want one about the scan: %+v", len(hints), hints)
	}
	if !strings.Contains(hints[0].Issue, "Every row of orders is read (50,000 rows)") {
		t.Errorf("issue = %q", hints[0].Issue)
	}
	if !strings.Contains(hints[0].Advice, "index") {
		t.Errorf("advice = %q, want it to point at an index", hints[0].Advice)
	}
}

// TestHintFullScanIgnoresSmallTables: scanning a small table is the right plan,
// and warning about it would teach the wrong lesson.
func TestHintFullScanIgnoresSmallTables(t *testing.T) {
	p := Plan{Kind: db.KindPostgres, Root: scan("countries", 250, Unknown)}
	if hints := Hints(p); len(hints) != 0 {
		t.Errorf("a 250-row scan should not be flagged, got %+v", hints)
	}
}

// TestHintFullScanWithoutRowCounts: SQLite reports no sizes, so the shape of the
// plan has to be enough on its own.
func TestHintFullScanWithoutRowCounts(t *testing.T) {
	p := Plan{Kind: db.KindSQLite, Root: scan("notes", Unknown, Unknown)}

	hints := Hints(p)
	if len(hints) != 1 {
		t.Fatalf("got %d hints, want one: %+v", len(hints), hints)
	}
	if strings.Contains(hints[0].Issue, "(") {
		t.Errorf("issue = %q, want no row count invented", hints[0].Issue)
	}
}

// TestHintStaleEstimate covers the second rule, and that it stays quiet on an
// unmeasured plan where there is nothing to compare against.
func TestHintStaleEstimate(t *testing.T) {
	root := newNode("Index Scan")
	root.On = "events"
	root.EstRows, root.ActRows = 100, 90000

	measured := Hints(Plan{Kind: db.KindPostgres, Analyzed: true, Root: root})
	if len(measured) != 1 {
		t.Fatalf("got %d hints, want the estimate one: %+v", len(measured), measured)
	}
	if !strings.Contains(measured[0].Issue, "expected 100 rows here but got 90,000") {
		t.Errorf("issue = %q", measured[0].Issue)
	}
	if !strings.Contains(measured[0].Advice, "ANALYZE events") {
		t.Errorf("advice = %q, want the engine's own refresh command", measured[0].Advice)
	}

	if hints := Hints(Plan{Kind: db.KindPostgres, Root: root}); len(hints) != 0 {
		t.Errorf("estimates alone can't be wrong about themselves, got %+v", hints)
	}
}

// TestHintStaleEstimateEngineWording: the refresh command differs per engine, and
// advice that names the wrong one is worse than none.
func TestHintStaleEstimateEngineWording(t *testing.T) {
	root := newNode("Full scan")
	root.On = "events"
	root.EstRows, root.ActRows = 100, 90000
	root.note(NoteFullScan)

	tests := []struct {
		kind db.Kind
		want string
	}{
		{db.KindPostgres, "ANALYZE events"},
		{db.KindMySQL, "ANALYZE TABLE events"},
		{db.KindMariaDB, "ANALYZE TABLE events"},
		{db.KindSQLite, "`ANALYZE`"},
	}
	for _, tt := range tests {
		var found string
		for _, h := range Hints(Plan{Kind: tt.kind, Analyzed: true, Root: root}) {
			if strings.Contains(h.Advice, "statistics") {
				found = h.Advice
			}
		}
		if !strings.Contains(found, tt.want) {
			t.Errorf("%s advice = %q, want %q", tt.kind, found, tt.want)
		}
	}
}

// TestHintSmallNumbersStayQuiet: a 10x miss on tiny numbers is noise.
func TestHintSmallNumbersStayQuiet(t *testing.T) {
	root := newNode("Index Scan")
	root.On = "users"
	root.EstRows, root.ActRows = 3, 40

	if hints := Hints(Plan{Kind: db.KindPostgres, Analyzed: true, Root: root}); len(hints) != 0 {
		t.Errorf("3 rows vs 40 means nothing, got %+v", hints)
	}
}

// TestHintSortAndTempTable covers the remaining two rules and their per-engine
// wording.
func TestHintSortAndTempTable(t *testing.T) {
	sort := newNode("Sort")
	sort.note(NoteDiskSort)
	pg := Hints(Plan{Kind: db.KindPostgres, Root: sort})
	if len(pg) != 1 || !strings.Contains(pg[0].Advice, "work_mem") {
		t.Errorf("Postgres disk-sort advice = %+v, want work_mem named", pg)
	}

	my := newNode("Ordering")
	my.note(NoteDiskSort)
	mysql := Hints(Plan{Kind: db.KindMySQL, Root: my})
	if len(mysql) != 1 || !strings.Contains(mysql[0].Advice, "sort_buffer_size") {
		t.Errorf("MySQL disk-sort advice = %+v, want sort_buffer_size named", mysql)
	}

	tmp := newNode("Grouping")
	tmp.note(NoteTempTable)
	tmp.note(NoteFilesort)
	both := Hints(Plan{Kind: db.KindMySQL, Root: tmp})
	if len(both) != 2 {
		t.Errorf("a filesort and a temp table are two separate things, got %+v", both)
	}
}

// TestHintsFollowTreeOrder: the hints are read next to the plan above them, so
// they have to appear in the same order.
func TestHintsFollowTreeOrder(t *testing.T) {
	root := scan("first", 5000, Unknown)
	root.Children = []*Node{scan("second", 6000, Unknown)}

	hints := Hints(Plan{Kind: db.KindPostgres, Root: root})
	if len(hints) != 2 {
		t.Fatalf("got %d hints, want one per scan", len(hints))
	}
	if !strings.Contains(hints[0].Issue, "first") || !strings.Contains(hints[1].Issue, "second") {
		t.Errorf("hints out of tree order: %+v", hints)
	}
}

// TestRenderIncludesHints: the report carries its own findings, so piping it
// somewhere doesn't lose them.
func TestRenderIncludesHints(t *testing.T) {
	out := Render(Plan{Kind: db.KindPostgres, Statement: "SELECT * FROM orders", Root: scan("orders", 90000, Unknown)})
	if !strings.Contains(out, "What stands out") || !strings.Contains(out, "Every row of orders") {
		t.Errorf("report should end with its hints:\n%s", out)
	}
}

// TestRenderNoHintsSection: a clean plan gets no empty heading.
func TestRenderNoHintsSection(t *testing.T) {
	root := newNode("Index Scan")
	root.On = "users"
	root.EstRows = 1
	out := Render(Plan{Kind: db.KindPostgres, Statement: "SELECT * FROM users WHERE id = 1", Root: root})
	if strings.Contains(out, "What stands out") {
		t.Errorf("nothing stands out, so the section should be absent:\n%s", out)
	}
	if !strings.Contains(out, "est 1 row") || strings.Contains(out, "est 1 rows") {
		t.Errorf("one row is singular:\n%s", out)
	}
}

// TestPlanJSON pins the machine-readable shape: milliseconds rather than
// nanoseconds, unknowns omitted rather than emitted as -1, and the hints included
// so a script sees what a person sees.
func TestPlanJSON(t *testing.T) {
	root := scan("orders", 90000, 90000)
	root.Time = millis(12.5)

	data, err := json.Marshal(Plan{
		Kind:      db.KindPostgres,
		Statement: "SELECT * FROM orders",
		Command:   "EXPLAIN (FORMAT JSON, ANALYZE) SELECT * FROM orders",
		Analyzed:  true,
		Root:      root,
		Execution: millis(14.2),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)

	for _, want := range []string{
		`"engine":"PostgreSQL"`,
		`"analyzed":true`,
		`"execution_ms":14.2`,
		`"time_ms":12.5`,
		`"est_rows":90000`,
		`"notes":["full scan"]`,
		`"hints":[`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("JSON missing %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-1") {
		t.Errorf("unknown measurements should be omitted, not emitted as -1:\n%s", got)
	}
	if strings.Contains(got, `"loops"`) {
		t.Errorf("an unreported loop count should be omitted:\n%s", got)
	}
}

// TestHumanInt covers the grouping used all over the report.
func TestHumanInt(t *testing.T) {
	tests := map[int64]string{0: "0", 12: "12", 999: "999", 1000: "1,000", 1234567: "1,234,567", -4500: "-4,500"}
	for in, want := range tests {
		if got := humanInt(in); got != want {
			t.Errorf("humanInt(%d) = %q, want %q", in, got, want)
		}
	}
}
