//go:build integration

package explain

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// Integration tests run only under `-tags integration` against a live server,
// configured the same way internal/db's are:
//
//	KASE_PG_HOST KASE_PG_PORT KASE_PG_USER KASE_PG_PASS KASE_PG_DB
//
// This is the only way to check the Postgres parser against a real catalog. The
// JSON field names ("Node Type", "Plan Rows", "Actual Total Time", …) are the
// riskiest assumption in the package: a fixture can only prove DBWiz reads what
// it was told to expect, not that a server says it.
func livePGEngine(t *testing.T) db.Engine {
	t.Helper()
	port, _ := strconv.Atoi(liveEnv("KASE_PG_PORT", "5432"))
	engine, err := db.New(db.KindPostgres)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	target := db.Target{
		Host:     liveEnv("KASE_PG_HOST", "127.0.0.1"),
		Port:     port,
		User:     liveEnv("KASE_PG_USER", "fawz"),
		Password: liveEnv("KASE_PG_PASS", "password"),
		Database: liveEnv("KASE_PG_DB", "postgres"),
	}
	if err := engine.Connect(context.Background(), target); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine
}

func liveEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// seedPlanTable creates a throwaway table with enough rows that a scan of it is
// worth reporting, and drops it afterwards.
func seedPlanTable(t *testing.T, engine db.Engine) string {
	t.Helper()
	name := "dbwiz_plan_test"
	ctx := context.Background()

	if _, err := engine.Query(ctx, "DROP TABLE IF EXISTS "+name); err != nil {
		t.Fatalf("clean up a previous run: %v", err)
	}
	if _, err := engine.Query(ctx, "CREATE TABLE "+name+" (id bigint, status text)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := engine.Query(ctx,
		"INSERT INTO "+name+" SELECT g, CASE WHEN g % 100 = 0 THEN 'open' ELSE 'closed' END FROM generate_series(1, 20000) g"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = engine.Query(context.Background(), "DROP TABLE IF EXISTS "+name)
	})
	return name
}

// TestPGLivePlan checks the whole path against a real server: the statement
// Postgres accepts, the JSON field names, and the numbers landing where the model
// says they do.
func TestPGLivePlan(t *testing.T) {
	engine := livePGEngine(t)
	table := seedPlanTable(t, engine)

	plan, err := Explain(context.Background(), engine, "SELECT * FROM "+table+" WHERE status = 'open'", Options{Analyze: true})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !plan.Analyzed || plan.Execution <= 0 {
		t.Errorf("a measured plan should report an execution time, got %v", plan.Execution)
	}
	if plan.Planning <= 0 {
		t.Errorf("planning time = %v, want it reported", plan.Planning)
	}

	// No index exists on status, so the only way to answer is to read the table.
	var scan *Node
	plan.Root.Walk(func(n *Node) {
		if n.Has(NoteFullScan) {
			scan = n
		}
	})
	if scan == nil {
		t.Fatalf("expected a sequential scan, got:\n%s", Render(plan))
	}
	if scan.On != table {
		t.Errorf("scan is on %q, want %q", scan.On, table)
	}
	if scan.EstRows <= 0 || scan.ActRows <= 0 {
		t.Errorf("est/actual rows = %d/%d, want both measured", scan.EstRows, scan.ActRows)
	}
	if scan.Cost <= 0 {
		t.Errorf("cost = %v, want the planner's number", scan.Cost)
	}
	if scan.Loops != 1 {
		t.Errorf("loops = %d, want 1", scan.Loops)
	}
	if !strings.Contains(scan.Detail, "status") {
		t.Errorf("detail = %q, want the filter", scan.Detail)
	}
	if hints := Hints(plan); len(hints) == 0 {
		t.Errorf("a 20,000-row scan should raise a hint:\n%s", Render(plan))
	}
}

// TestPGLiveEstimatesOnly: without ANALYZE the server plans but does not run, so
// nothing may be reported as measured.
func TestPGLiveEstimatesOnly(t *testing.T) {
	engine := livePGEngine(t)
	table := seedPlanTable(t, engine)

	plan, err := Explain(context.Background(), engine, "SELECT * FROM "+table, Options{})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	for _, n := range plan.Nodes() {
		if n.ActRows != Unknown || n.Time != 0 {
			t.Errorf("%s reports measurements from an unmeasured plan: rows=%d time=%v", n.Label(), n.ActRows, n.Time)
		}
	}
	if plan.Execution != 0 {
		t.Errorf("execution time = %v, want none without ANALYZE", plan.Execution)
	}
}

// TestPGLiveIndexScan: with an index available the plan changes shape, and the
// full-scan note must not appear.
func TestPGLiveIndexScan(t *testing.T) {
	engine := livePGEngine(t)
	table := seedPlanTable(t, engine)

	ctx := context.Background()
	if _, err := engine.Query(ctx, "CREATE INDEX ON "+table+" (id)"); err != nil {
		t.Fatalf("create index: %v", err)
	}
	if _, err := engine.Query(ctx, "ANALYZE "+table); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	plan, err := Explain(ctx, engine, "SELECT * FROM "+table+" WHERE id = 17", Options{Analyze: true})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	for _, n := range plan.Nodes() {
		if n.Has(NoteFullScan) {
			t.Errorf("an indexed lookup should not scan:\n%s", Render(plan))
		}
	}
}

// TestPGLiveRefusesToMeasureAWrite is the safety rail against a real server: the
// DELETE must not reach it.
func TestPGLiveRefusesToMeasureAWrite(t *testing.T) {
	engine := livePGEngine(t)
	table := seedPlanTable(t, engine)

	if _, err := Explain(context.Background(), engine, "DELETE FROM "+table, Options{Analyze: true}); err == nil {
		t.Fatal("measuring a DELETE must be refused")
	}

	res, err := engine.Query(context.Background(), "SELECT count(*) FROM "+table)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got := cellText(res.Rows[0][0]); got != "20000" {
		t.Errorf("the table has %s rows — the refused statement ran anyway", got)
	}
}
