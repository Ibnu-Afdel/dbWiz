package explain

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// The fixtures in explain_test.go pin the parsers against captured output. This
// file goes the other way and drives a *real* engine end to end, which is the
// only way to catch DBWiz asking for a plan in a spelling the engine rejects.
//
// SQLite is the engine that makes that possible without an external service: the
// driver is pure Go, so this runs in ordinary CI. The Postgres equivalent is
// tag-gated in postgres_live_test.go.

// newPlanDB creates a real SQLite database with a table big enough to be worth
// indexing, plus an index on one column, so both plan shapes are reachable.
func newPlanDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.db")
	pool, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(`
		CREATE TABLE notes (
			id INTEGER PRIMARY KEY,
			slug TEXT NOT NULL,
			body TEXT
		);
		CREATE UNIQUE INDEX idx_notes_slug ON notes(slug);
		INSERT INTO notes (slug, body) VALUES ('a', 'x'), ('b', 'y'), ('c', NULL);
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return path
}

// openPlanEngine connects the real SQLite engine to path.
func openPlanEngine(t *testing.T) db.Engine {
	t.Helper()
	engine, err := db.New(db.KindSQLite)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Connect(context.Background(), db.Target{Path: newPlanDB(t)}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine
}

// TestLiveSQLiteFullScan drives Explain against a real database: a query with no
// usable index has to come back as a scan of the table, with the note the hint
// depends on.
func TestLiveSQLiteFullScan(t *testing.T) {
	engine := openPlanEngine(t)

	plan, err := Explain(context.Background(), engine, "SELECT * FROM notes WHERE body = 'x'", Options{})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if plan.Root.On != "notes" {
		t.Errorf("plan is about %q, want the notes table", plan.Root.On)
	}
	if !plan.Root.Has(NoteFullScan) {
		t.Errorf("an unindexed filter should scan; got %q notes %v (raw: %s)", plan.Root.Op, plan.Root.Notes, plan.Raw)
	}
	if hints := Hints(plan); len(hints) == 0 {
		t.Error("a scan with no row counts should still raise its hint")
	}
	if !strings.Contains(Render(plan), "-> Full scan on notes") {
		t.Errorf("report should name the scan:\n%s", Render(plan))
	}
}

// TestLiveSQLiteIndexSearch is the other half: with an index available the plan
// must come back as a search, and must not be flagged.
func TestLiveSQLiteIndexSearch(t *testing.T) {
	engine := openPlanEngine(t)

	plan, err := Explain(context.Background(), engine, "SELECT * FROM notes WHERE slug = 'a'", Options{})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if plan.Root.Op != "Index search" {
		t.Errorf("op = %q, want an index search (raw: %s)", plan.Root.Op, plan.Raw)
	}
	if !strings.Contains(plan.Root.Detail, "idx_notes_slug") {
		t.Errorf("detail = %q, want the index named (raw: %s)", plan.Root.Detail, plan.Raw)
	}
	if hints := Hints(plan); len(hints) != 0 {
		t.Errorf("an indexed lookup is the good case, got %+v", hints)
	}
}

// TestLiveSQLiteRejectsBadSQL: a statement the engine can't parse must surface as
// an error rather than an empty plan.
func TestLiveSQLiteRejectsBadSQL(t *testing.T) {
	engine := openPlanEngine(t)

	if _, err := Explain(context.Background(), engine, "SELECT * FROM nowhere", Options{}); err == nil {
		t.Fatal("explaining a missing table should fail")
	}
}
