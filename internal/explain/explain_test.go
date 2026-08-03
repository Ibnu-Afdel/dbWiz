package explain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// fakeRunner answers one canned plan and records the statement it was asked to
// run, which is how the per-engine EXPLAIN dialects are checked without a server.
type fakeRunner struct {
	kind   db.Kind
	result db.Result
	err    error
	asked  string
}

func (f *fakeRunner) Kind() db.Kind { return f.kind }

func (f *fakeRunner) Query(_ context.Context, sql string) (db.Result, error) {
	f.asked = sql
	if f.err != nil {
		return db.Result{}, f.err
	}
	return f.result, nil
}

// textResult is a plan handed back the way Postgres and MySQL hand one back: one
// row, one column, the whole document inside it.
func textResult(s string) db.Result {
	return db.Result{Columns: []string{"QUERY PLAN"}, Rows: [][]any{{s}}}
}

const pgSeqScanJSON = `[
  {
    "Plan": {
      "Node Type": "Hash Join",
      "Join Type": "Left",
      "Total Cost": 42.5,
      "Plan Rows": 850,
      "Actual Rows": 12,
      "Actual Loops": 1,
      "Actual Total Time": 3.5,
      "Hash Cond": "(o.user_id = u.id)",
      "Plans": [
        {
          "Node Type": "Seq Scan",
          "Relation Name": "orders",
          "Total Cost": 18.5,
          "Plan Rows": 1200,
          "Actual Rows": 1200,
          "Actual Loops": 1,
          "Actual Total Time": 1.25,
          "Filter": "(status = 'open'::text)"
        },
        {
          "Node Type": "Index Scan",
          "Relation Name": "users",
          "Index Name": "users_pkey",
          "Total Cost": 8.3,
          "Plan Rows": 1,
          "Actual Rows": 1,
          "Actual Loops": 1,
          "Index Cond": "(id = 7)"
        }
      ]
    },
    "Planning Time": 0.35,
    "Execution Time": 4.2
  }
]`

// TestExplainPostgres covers v4 2.1 on the reference engine: the JSON tree
// becomes the neutral model, keeping estimates and measurements apart.
func TestExplainPostgres(t *testing.T) {
	r := &fakeRunner{kind: db.KindPostgres, result: textResult(pgSeqScanJSON)}

	plan, err := Explain(context.Background(), r, "SELECT * FROM orders o LEFT JOIN users u ON u.id = o.user_id", Options{Analyze: true})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}

	if want := "EXPLAIN (FORMAT JSON, ANALYZE) SELECT"; !strings.HasPrefix(r.asked, want) {
		t.Errorf("ran %q, want it to start with %q", r.asked, want)
	}
	if plan.Root.Op != "Left Hash Join" {
		t.Errorf("root op = %q, want the join type folded in", plan.Root.Op)
	}
	if plan.Planning != millis(0.35) || plan.Execution != millis(4.2) {
		t.Errorf("planning/execution = %v/%v, want 0.35ms/4.2ms", plan.Planning, plan.Execution)
	}
	if len(plan.Root.Children) != 2 {
		t.Fatalf("root has %d children, want 2", len(plan.Root.Children))
	}

	scan := plan.Root.Children[0]
	// Postgres keeps its own vocabulary — "Seq Scan" is the term every Postgres
	// article uses. MySQL's "ALL" is the one that gets translated, because it
	// reads as harmless and means the opposite.
	if scan.Label() != "Seq Scan on orders" {
		t.Errorf("scan label = %q, want Postgres's own wording", scan.Label())
	}
	if scan.EstRows != 1200 || scan.ActRows != 1200 {
		t.Errorf("scan rows est/actual = %d/%d, want 1200/1200", scan.EstRows, scan.ActRows)
	}
	if scan.Cost != 18.5 || scan.Time != millis(1.25) || scan.Loops != 1 {
		t.Errorf("scan cost/time/loops = %v/%v/%d", scan.Cost, scan.Time, scan.Loops)
	}
	if !scan.Has(NoteFullScan) {
		t.Errorf("a Seq Scan should carry the full-scan note, got %v", scan.Notes)
	}
	if !strings.Contains(scan.Detail, "status = 'open'") {
		t.Errorf("scan detail = %q, want the filter", scan.Detail)
	}

	idx := plan.Root.Children[1]
	if idx.On != "users" || !strings.Contains(idx.Detail, "using users_pkey") {
		t.Errorf("index node = %q / %q, want it to name the relation and the index", idx.On, idx.Detail)
	}
	if idx.Has(NoteFullScan) {
		t.Error("an index scan is not a full scan")
	}
}

// TestExplainPostgresEstimatesOnly: without --analyze nothing is measured, and
// the measurements must stay Unknown rather than reading as zero.
func TestExplainPostgresEstimatesOnly(t *testing.T) {
	r := &fakeRunner{kind: db.KindPostgres, result: textResult(pgSeqScanJSON)}

	plan, err := Explain(context.Background(), r, "SELECT 1", Options{})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if strings.Contains(r.asked, "ANALYZE") {
		t.Errorf("ran %q, want no ANALYZE without the flag", r.asked)
	}
	if plan.Analyzed {
		t.Error("plan should not claim to be measured")
	}
	for _, n := range plan.Nodes() {
		if n.ActRows != Unknown || n.Time != 0 {
			t.Errorf("%s reports measurements it never took: rows=%d time=%v", n.Label(), n.ActRows, n.Time)
		}
	}
}

// TestExplainPostgresSortSpill covers the note that backs the disk-sort hint.
func TestExplainPostgresSortSpill(t *testing.T) {
	raw := `[{"Plan":{"Node Type":"Sort","Total Cost":100,"Plan Rows":50000,` +
		`"Sort Key":["created_at"],"Sort Method":"external merge","Sort Space Type":"Disk"}}]`
	r := &fakeRunner{kind: db.KindPostgres, result: textResult(raw)}

	plan, err := Explain(context.Background(), r, "SELECT * FROM events ORDER BY created_at", Options{})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !plan.Root.Has(NoteDiskSort) {
		t.Errorf("an external merge sort should be noted, got %v", plan.Root.Notes)
	}
	if !strings.Contains(plan.Root.Detail, "sort key: created_at") {
		t.Errorf("detail = %q, want the sort key", plan.Root.Detail)
	}
}

const mysqlJSON = `{
  "query_block": {
    "select_id": 1,
    "cost_info": {"query_cost": "12.35"},
    "ordering_operation": {
      "using_filesort": true,
      "table": {
        "table_name": "orders",
        "access_type": "ALL",
        "rows_examined_per_scan": 5000,
        "filtered": "10.00",
        "cost_info": {"read_cost": "9.10"},
        "attached_condition": "(orders.status = 'open')"
      }
    }
  }
}`

// TestExplainMySQLJSON covers the FORMAT=JSON walker: the block wrapper, the
// ordering step's filesort flag, and a full table scan spelled "ALL".
func TestExplainMySQLJSON(t *testing.T) {
	r := &fakeRunner{kind: db.KindMySQL, result: db.Result{Columns: []string{"EXPLAIN"}, Rows: [][]any{{[]byte(mysqlJSON)}}}}

	plan, err := Explain(context.Background(), r, "SELECT * FROM orders ORDER BY created_at", Options{})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if r.asked != "EXPLAIN FORMAT=JSON SELECT * FROM orders ORDER BY created_at" {
		t.Errorf("ran %q", r.asked)
	}
	if plan.Root.Op != "Query block" || plan.Root.Cost != 12.35 {
		t.Errorf("root = %q cost %v, want the query block and its cost", plan.Root.Op, plan.Root.Cost)
	}
	if len(plan.Root.Children) != 1 {
		t.Fatalf("query block has %d children, want the ordering step", len(plan.Root.Children))
	}
	order := plan.Root.Children[0]
	if order.Op != "Ordering" || !order.Has(NoteFilesort) {
		t.Errorf("ordering step = %q notes %v", order.Op, order.Notes)
	}
	if len(order.Children) != 1 {
		t.Fatalf("ordering step has %d children, want the table", len(order.Children))
	}
	table := order.Children[0]
	if table.Label() != "Full scan on orders" {
		t.Errorf("table label = %q, want ALL translated into words", table.Label())
	}
	if table.EstRows != 5000 || !table.Has(NoteFullScan) {
		t.Errorf("table rows = %d notes %v", table.EstRows, table.Notes)
	}
	if !strings.Contains(table.Detail, "orders.status") {
		t.Errorf("table detail = %q, want the attached condition", table.Detail)
	}
}

// TestExplainMariaDBAnalyze: MariaDB measures inside the same JSON shape, and
// spells the estimate "rows" rather than "rows_examined_per_scan".
func TestExplainMariaDBAnalyze(t *testing.T) {
	raw := `{"query_block":{"select_id":1,"table":{"table_name":"users","access_type":"ALL",` +
		`"rows":900,"r_rows":1000,"r_total_time_ms":2.5}}}`
	r := &fakeRunner{kind: db.KindMariaDB, result: textResult(raw)}

	plan, err := Explain(context.Background(), r, "SELECT * FROM users", Options{Analyze: true})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !strings.HasPrefix(r.asked, "ANALYZE FORMAT=JSON ") {
		t.Errorf("ran %q, want MariaDB's ANALYZE spelling", r.asked)
	}
	table := plan.Root.Children[0]
	if table.EstRows != 900 || table.ActRows != 1000 || table.Time != millis(2.5) {
		t.Errorf("table est/act/time = %d/%d/%v", table.EstRows, table.ActRows, table.Time)
	}
}

const mysqlTree = `-> Nested loop inner join  (cost=2.05 rows=3) (actual time=0.043..0.049 rows=2 loops=1)
    -> Filter: (users.age > 30)  (cost=1.05 rows=3) (actual time=0.030..0.035 rows=2 loops=1)
        -> Table scan on users  (cost=1.05 rows=8) (actual time=0.020..0.025 rows=8 loops=1)
    -> Index lookup on o using fk_user (user_id=users.id)  (cost=0.35 rows=1) (actual time=0.005..0.006 rows=1 loops=2)`

// TestExplainMySQLTree covers the measured plan, which MySQL answers as indented
// text rather than JSON — the indentation is the tree.
func TestExplainMySQLTree(t *testing.T) {
	r := &fakeRunner{kind: db.KindMySQL, result: textResult(mysqlTree)}

	plan, err := Explain(context.Background(), r, "SELECT * FROM users JOIN orders o ON o.user_id = users.id", Options{Analyze: true})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !strings.HasPrefix(r.asked, "EXPLAIN ANALYZE ") {
		t.Errorf("ran %q, want MySQL's measured spelling", r.asked)
	}

	root := plan.Root
	if root.Op != "Nested loop inner join" || root.ActRows != 2 || root.Loops != 1 {
		t.Errorf("root = %q rows %d loops %d", root.Op, root.ActRows, root.Loops)
	}
	if len(root.Children) != 2 {
		t.Fatalf("root has %d children, want the filter and the lookup", len(root.Children))
	}

	filter := root.Children[0]
	if filter.Op != "Filter" || filter.Detail != "(users.age > 30)" {
		t.Errorf("filter = %q / %q", filter.Op, filter.Detail)
	}
	if len(filter.Children) != 1 {
		t.Fatalf("the scan should nest under the filter, got %d children", len(filter.Children))
	}
	scan := filter.Children[0]
	if scan.Label() != "Table scan on users" || !scan.Has(NoteFullScan) {
		t.Errorf("scan = %q notes %v", scan.Label(), scan.Notes)
	}
	if scan.EstRows != 8 || scan.ActRows != 8 || scan.Cost != 1.05 || scan.Time != millis(0.025) {
		t.Errorf("scan est/act/cost/time = %d/%d/%v/%v", scan.EstRows, scan.ActRows, scan.Cost, scan.Time)
	}

	lookup := root.Children[1]
	if lookup.Op != "Index lookup" || lookup.On != "o" || !strings.Contains(lookup.Detail, "using fk_user") {
		t.Errorf("lookup = %q / %q / %q", lookup.Op, lookup.On, lookup.Detail)
	}
	if lookup.Loops != 2 {
		t.Errorf("lookup loops = %d, want 2", lookup.Loops)
	}
}

// sqliteResult builds the row set SQLite's EXPLAIN QUERY PLAN returns.
func sqliteResult(rows ...[]any) db.Result {
	return db.Result{Columns: []string{"id", "parent", "notused", "detail"}, Rows: rows}
}

// TestExplainSQLite covers the odd engine out: a flat row set whose tree lives in
// the parent pointers, with no numbers anywhere.
func TestExplainSQLite(t *testing.T) {
	r := &fakeRunner{kind: db.KindSQLite, result: sqliteResult(
		[]any{int64(3), int64(0), int64(0), "SCAN notes"},
		[]any{int64(9), int64(3), int64(0), "USE TEMP B-TREE FOR ORDER BY"},
	)}

	plan, err := Explain(context.Background(), r, "SELECT * FROM notes ORDER BY created", Options{})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if r.asked != "EXPLAIN QUERY PLAN SELECT * FROM notes ORDER BY created" {
		t.Errorf("ran %q", r.asked)
	}
	if plan.Root.Label() != "Full scan on notes" || !plan.Root.Has(NoteFullScan) {
		t.Errorf("root = %q notes %v", plan.Root.Label(), plan.Root.Notes)
	}
	if len(plan.Root.Children) != 1 {
		t.Fatalf("the b-tree step should nest under its parent, got %d children", len(plan.Root.Children))
	}
	btree := plan.Root.Children[0]
	if !btree.Has(NoteTempTable) {
		t.Errorf("a temp b-tree should be noted, got %v", btree.Notes)
	}
	for _, n := range plan.Nodes() {
		if n.EstRows != Unknown || n.Cost != Unknown {
			t.Errorf("%s reports numbers SQLite never gives: rows=%d cost=%v", n.Label(), n.EstRows, n.Cost)
		}
	}
}

// TestExplainSQLiteIndexSearch: a SEARCH is not a scan, and must not raise the
// hint that a scan does.
func TestExplainSQLiteIndexSearch(t *testing.T) {
	r := &fakeRunner{kind: db.KindSQLite, result: sqliteResult(
		[]any{int64(2), int64(0), int64(0), "SEARCH notes USING INDEX idx_notes_slug (slug=?)"},
	)}

	plan, err := Explain(context.Background(), r, "SELECT * FROM notes WHERE slug = 'x'", Options{})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if plan.Root.Op != "Index search" || plan.Root.On != "notes" {
		t.Errorf("root = %q on %q", plan.Root.Op, plan.Root.On)
	}
	if plan.Root.Has(NoteFullScan) {
		t.Error("an index search must not be reported as a full scan")
	}
	if !strings.Contains(plan.Root.Detail, "idx_notes_slug") {
		t.Errorf("detail = %q, want the index name", plan.Root.Detail)
	}
}

// TestExplainRefusesAnalyzeOnWrites is the safety rail: --analyze runs the
// statement, so a statement that writes never gets measured by accident.
func TestExplainRefusesAnalyzeOnWrites(t *testing.T) {
	for _, stmt := range []string{
		"DELETE FROM users WHERE id = 1",
		"UPDATE users SET name = 'x'",
		"INSERT INTO users (name) VALUES ('x')",
		"WITH gone AS (DELETE FROM users RETURNING *) SELECT * FROM gone",
		"TRUNCATE users",
		"DROP TABLE users",
	} {
		r := &fakeRunner{kind: db.KindPostgres, result: textResult(pgSeqScanJSON)}
		if _, err := Explain(context.Background(), r, stmt, Options{Analyze: true}); err == nil {
			t.Errorf("--analyze on %q should be refused", stmt)
		}
		if r.asked != "" {
			t.Errorf("%q reached the server as %q — the refusal must come first", stmt, r.asked)
		}
	}
}

// TestExplainPlansWritesWithoutAnalyze: refusing to *measure* a write must not
// stop DBWiz from planning it, which is the safe and useful half.
func TestExplainPlansWritesWithoutAnalyze(t *testing.T) {
	r := &fakeRunner{kind: db.KindPostgres, result: textResult(pgSeqScanJSON)}
	if _, err := Explain(context.Background(), r, "DELETE FROM users WHERE id = 1", Options{}); err != nil {
		t.Fatalf("planning a write should be allowed: %v", err)
	}
	if !strings.Contains(r.asked, "DELETE") || strings.Contains(r.asked, "ANALYZE") {
		t.Errorf("ran %q, want a plain EXPLAIN of the delete", r.asked)
	}
}

// TestExplainSQLiteRefusesAnalyze: SQLite never measures, so the flag is refused
// rather than silently ignored.
func TestExplainSQLiteRefusesAnalyze(t *testing.T) {
	r := &fakeRunner{kind: db.KindSQLite, result: sqliteResult([]any{int64(2), int64(0), int64(0), "SCAN notes"})}
	_, err := Explain(context.Background(), r, "SELECT * FROM notes", Options{Analyze: true})
	if err == nil {
		t.Fatal("--analyze on SQLite should be refused")
	}
	if !strings.Contains(err.Error(), "never measures") {
		t.Errorf("error = %q, want it to explain why", err)
	}
}

// TestWrites covers the read/write classification the safety rail leans on,
// including the literal-stripping that keeps a quoted word from tripping it.
func TestWrites(t *testing.T) {
	tests := []struct {
		sql  string
		want bool
	}{
		{"SELECT * FROM users", false},
		{"select id from users where deleted_at is null", false},
		{"SELECT 'delete me' AS label", false},
		{"SELECT * FROM t -- delete this later", false},
		{"SELECT * FROM t /* update soon */", false},
		{"WITH recent AS (SELECT * FROM logs) SELECT * FROM recent", false},
		{"TABLE users", false},
		{"DELETE FROM users", true},
		{"WITH gone AS (DELETE FROM users RETURNING id) SELECT * FROM gone", true},
		{"SELECT * FROM users FOR UPDATE", true},
		{"CREATE TABLE t (id int)", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := Writes(tt.sql); got != tt.want {
			t.Errorf("Writes(%q) = %v, want %v", tt.sql, got, tt.want)
		}
	}
}

// TestExplainStatementDialects pins the per-engine spelling, which is the part
// that silently breaks against the wrong server.
func TestExplainStatementDialects(t *testing.T) {
	tests := []struct {
		kind    db.Kind
		analyze bool
		want    string
	}{
		{db.KindPostgres, false, "EXPLAIN (FORMAT JSON) SELECT 1"},
		{db.KindPostgres, true, "EXPLAIN (FORMAT JSON, ANALYZE) SELECT 1"},
		{db.KindMySQL, false, "EXPLAIN FORMAT=JSON SELECT 1"},
		{db.KindMySQL, true, "EXPLAIN ANALYZE SELECT 1"},
		{db.KindMariaDB, false, "EXPLAIN FORMAT=JSON SELECT 1"},
		{db.KindMariaDB, true, "ANALYZE FORMAT=JSON SELECT 1"},
		{db.KindSQLite, false, "EXPLAIN QUERY PLAN SELECT 1"},
	}
	for _, tt := range tests {
		got, err := explainStatement(tt.kind, "SELECT 1", tt.analyze)
		if err != nil {
			t.Fatalf("%s: %v", tt.kind, err)
		}
		if got != tt.want {
			t.Errorf("%s analyze=%v: got %q, want %q", tt.kind, tt.analyze, got, tt.want)
		}
	}
}

// TestExplainEmptyStatement: an empty editor is a no-op with a readable reason,
// not an EXPLAIN of nothing.
func TestExplainEmptyStatement(t *testing.T) {
	r := &fakeRunner{kind: db.KindPostgres}
	if _, err := Explain(context.Background(), r, "   \n ", Options{}); err == nil {
		t.Fatal("an empty statement should be refused")
	}
	if r.asked != "" {
		t.Errorf("nothing should have reached the server, got %q", r.asked)
	}
}

// TestExplainNilContext mirrors the schema package: a nil context must not panic.
func TestExplainNilContext(t *testing.T) {
	var nilCtx context.Context
	r := &fakeRunner{kind: db.KindPostgres, result: textResult(pgSeqScanJSON)}
	if _, err := Explain(nilCtx, r, "SELECT 1", Options{}); err != nil {
		t.Fatalf("Explain with a nil context: %v", err)
	}
}

// TestExplainUnparseableOutput: a plan DBWiz can't read fails with a reason
// rather than a nil tree.
func TestExplainUnparseableOutput(t *testing.T) {
	r := &fakeRunner{kind: db.KindPostgres, result: textResult("not json at all")}
	if _, err := Explain(context.Background(), r, "SELECT 1", Options{}); err == nil {
		t.Fatal("unreadable output should be an error")
	}
}

// TestNodeWalkOrder: hints and rendering both rely on parents coming before
// children.
func TestNodeWalkOrder(t *testing.T) {
	root := newNode("A")
	b, c := newNode("B"), newNode("C")
	root.Children = []*Node{b, c}
	b.Children = []*Node{newNode("D")}

	var got []string
	root.Walk(func(n *Node) { got = append(got, n.Op) })
	if strings.Join(got, "") != "ABDC" {
		t.Errorf("walk order = %v, want parents before children", got)
	}
}

// TestMillisRoundTrip guards the duration helper the parsers share.
func TestMillisRoundTrip(t *testing.T) {
	if got := millis(1.5); got != 1500*time.Microsecond {
		t.Errorf("millis(1.5) = %v, want 1.5ms", got)
	}
	if got := millis(0); got != 0 {
		t.Errorf("millis(0) = %v, want 0", got)
	}
}
