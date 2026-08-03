package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// fakePlanEngine answers every statement with one canned plan and records what it
// was asked to run, which is how the EXPLAIN dialect is checked without a server.
type fakePlanEngine struct {
	db.Engine
	kind  db.Kind
	plan  string
	asked string
}

func (f *fakePlanEngine) Kind() db.Kind                            { return f.kind }
func (f *fakePlanEngine) Connect(context.Context, db.Target) error { return nil }
func (f *fakePlanEngine) Close() error                             { return nil }

func (f *fakePlanEngine) Query(_ context.Context, sql string) (db.Result, error) {
	f.asked = sql
	return db.Result{Columns: []string{"QUERY PLAN"}, Rows: [][]any{{f.plan}}}, nil
}

// pgPlanJSON is a measured Postgres plan: a big sequential scan whose estimate
// was badly wrong — both hints in one fixture.
const pgPlanJSON = `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"orders",` +
	`"Total Cost":1850.0,"Plan Rows":100,"Actual Rows":90000,"Actual Loops":1,` +
	`"Actual Total Time":45.5,"Filter":"(status = 'open'::text)"},` +
	`"Planning Time":0.3,"Execution Time":52.1}]`

// planTestSetup wires the detection and engine seams to a single fake container.
func planTestSetup(t *testing.T, engine db.Engine) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{full("pg", docker.EnginePostgres)}, nil)
	fakeEngines(t, engine)
}

// TestExplainRendersPlan covers v4 2.3: the default report is the readable tree
// plus the notes.
func TestExplainRendersPlan(t *testing.T) {
	eng := &fakePlanEngine{kind: db.KindPostgres, plan: pgPlanJSON}
	planTestSetup(t, eng)

	var out bytes.Buffer
	err := runExplain(context.Background(), &out, explainOpts{
		statement: "SELECT * FROM orders WHERE status = 'open'",
		analyze:   true,
	})
	if err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	if !strings.HasPrefix(eng.asked, "EXPLAIN (FORMAT JSON, ANALYZE) ") {
		t.Errorf("ran %q, want the measured Postgres form", eng.asked)
	}

	got := out.String()
	for _, want := range []string{
		"Plan for: SELECT * FROM orders WHERE status = 'open'",
		"PostgreSQL, measured (the statement was run)",
		"-> Seq Scan on orders",
		"actual 90,000 rows in 45.5 ms",
		"Total: planning 0.3 ms, execution 52.1 ms",
		"What stands out",
		"Every row of orders is read (90,000 rows).",
		"expected 100 rows here but got 90,000",
		"ANALYZE orders",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
}

// TestExplainEstimatesByDefault: nothing runs unless --analyze is asked for, and
// the header says which kind of numbers these are.
func TestExplainEstimatesByDefault(t *testing.T) {
	eng := &fakePlanEngine{kind: db.KindPostgres, plan: pgPlanJSON}
	planTestSetup(t, eng)

	var out bytes.Buffer
	if err := runExplain(context.Background(), &out, explainOpts{statement: "SELECT * FROM orders"}); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	if strings.Contains(eng.asked, "ANALYZE") {
		t.Errorf("ran %q, want no ANALYZE without the flag", eng.asked)
	}
	if !strings.Contains(out.String(), "estimates only (nothing was executed)") {
		t.Errorf("the header must say these are estimates:\n%s", out.String())
	}
}

// TestExplainRefusesToMeasureAWrite is the safety rail at the CLI boundary: the
// statement must not reach the server at all.
func TestExplainRefusesToMeasureAWrite(t *testing.T) {
	eng := &fakePlanEngine{kind: db.KindPostgres, plan: pgPlanJSON}
	planTestSetup(t, eng)

	var out bytes.Buffer
	err := runExplain(context.Background(), &out, explainOpts{
		statement: "DELETE FROM orders WHERE status = 'open'",
		analyze:   true,
	})
	if err == nil {
		t.Fatal("--analyze on a DELETE must be refused")
	}
	if !strings.Contains(err.Error(), "runs the statement") {
		t.Errorf("error = %q, want it to explain why", err)
	}
	if eng.asked != "" {
		t.Errorf("nothing should have reached the server, got %q", eng.asked)
	}
	if out.Len() != 0 {
		t.Errorf("no report should be printed, got %q", out.String())
	}
}

// TestExplainJSON: the machine-readable form carries the same findings the text
// report shows.
func TestExplainJSON(t *testing.T) {
	planTestSetup(t, &fakePlanEngine{kind: db.KindPostgres, plan: pgPlanJSON})

	var out bytes.Buffer
	err := runExplain(context.Background(), &out, explainOpts{
		statement: "SELECT * FROM orders",
		analyze:   true,
		asJSON:    true,
	})
	if err != nil {
		t.Fatalf("runExplain: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		`"engine": "PostgreSQL"`,
		`"analyzed": true`,
		`"op": "Seq Scan"`,
		`"on": "orders"`,
		`"actual_rows": 90000`,
		`"hints": [`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("JSON missing %s:\n%s", want, got)
		}
	}
}

// TestExplainRaw hands back the engine's own output untouched, for when DBWiz's
// reading of a plan isn't what's wanted.
func TestExplainRaw(t *testing.T) {
	planTestSetup(t, &fakePlanEngine{kind: db.KindPostgres, plan: pgPlanJSON})

	var out bytes.Buffer
	if err := runExplain(context.Background(), &out, explainOpts{statement: "SELECT 1", asRaw: true}); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != pgPlanJSON {
		t.Errorf("--raw should print the engine's output verbatim, got:\n%s", got)
	}
}

// TestExplainRejectsJSONAndRaw: the two output flags are mutually exclusive, and
// the command says so instead of silently preferring one.
func TestExplainRejectsJSONAndRaw(t *testing.T) {
	planTestSetup(t, &fakePlanEngine{kind: db.KindPostgres, plan: pgPlanJSON})

	c := NewExplainCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--json", "--raw", "SELECT 1"})
	if err := c.Execute(); err == nil {
		t.Fatal("--json with --raw should be rejected")
	}
}

// TestExplainSQLitePlan covers the engine whose plan carries no numbers at all:
// the report must still be useful, and must not invent any.
func TestExplainSQLitePlan(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{full("pg", docker.EnginePostgres)}, nil)
	fakeEngines(t, &fakeSQLitePlanEngine{})

	var out bytes.Buffer
	if err := runExplain(context.Background(), &out, explainOpts{statement: "SELECT * FROM notes"}); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "-> Full scan on notes") {
		t.Errorf("report missing the scan:\n%s", got)
	}
	if strings.Contains(got, "est ") || strings.Contains(got, "cost ") {
		t.Errorf("SQLite reports no numbers, so none should appear:\n%s", got)
	}
	if !strings.Contains(got, "Every row of notes is read.") {
		t.Errorf("the scan should still raise its hint:\n%s", got)
	}
}

// fakeSQLitePlanEngine answers in SQLite's row-set shape rather than with a
// document.
type fakeSQLitePlanEngine struct {
	db.Engine
}

func (f *fakeSQLitePlanEngine) Kind() db.Kind                            { return db.KindSQLite }
func (f *fakeSQLitePlanEngine) Connect(context.Context, db.Target) error { return nil }
func (f *fakeSQLitePlanEngine) Close() error                             { return nil }

func (f *fakeSQLitePlanEngine) Query(context.Context, string) (db.Result, error) {
	return db.Result{
		Columns: []string{"id", "parent", "notused", "detail"},
		Rows:    [][]any{{int64(2), int64(0), int64(0), "SCAN notes"}},
	}, nil
}
