package migrations

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// The fake in migrations_test.go pins the logic; this file drives a *real* engine
// end to end, which is the only way to catch a statement the fake accepts and a
// server rejects — a quoting slip, or a read that assumes a column type the
// driver hands back differently.
//
// SQLite is the engine that makes that possible with no external service (the
// driver is pure Go), so this runs in ordinary CI.

// newLedgerDB creates a real SQLite database managed by a golang-migrate-shaped
// ledger plus two ordinary tables, so detection has to actually discriminate.
func newLedgerDB(t *testing.T, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	pool, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);
		CREATE TABLE orders (id INTEGER PRIMARY KEY, total INTEGER);
		CREATE TABLE schema_migrations (version TEXT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL);
	` + extra); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return path
}

// openLedgerEngine connects the real SQLite engine to a seeded file.
func openLedgerEngine(t *testing.T, extra string) db.Engine {
	t.Helper()
	engine, err := db.New(db.KindSQLite)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Connect(context.Background(), db.Target{Path: newLedgerDB(t, extra)}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine
}

// TestLiveSQLiteCleanLedger: a real database at a known version, read through the
// real driver.
func TestLiveSQLiteCleanLedger(t *testing.T) {
	engine := openLedgerEngine(t, `INSERT INTO schema_migrations VALUES ('20260714120000', 0);`)

	status, err := Detect(context.Background(), engine, "", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(status.Ledgers) != 1 {
		t.Fatalf("found %+v, want the one ledger", status.Ledgers)
	}
	l := status.Ledgers[0]
	if l.Tool != "golang-migrate" || !l.Pointer {
		t.Errorf("got %+v, want a golang-migrate pointer ledger", l)
	}
	if len(l.Latest) != 1 || l.Latest[0].ID != "20260714120000" {
		t.Errorf("entries = %+v", l.Latest)
	}
	if status.Trouble() {
		t.Errorf("a clean ledger reported trouble: %+v", l.Latest)
	}
	if report := Render(status); !strings.Contains(report, "at version 20260714120000") {
		t.Errorf("report should name the version:\n%s", report)
	}
}

// TestLiveSQLiteDirtyLedger: SQLite stores a boolean as an integer, so this is
// where the truthiness helper is proved against a driver rather than a literal.
func TestLiveSQLiteDirtyLedger(t *testing.T) {
	engine := openLedgerEngine(t, `INSERT INTO schema_migrations VALUES ('20260714120000', 1);`)

	status, err := Detect(context.Background(), engine, "", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !status.Trouble() {
		t.Fatalf("a dirty ledger must report trouble: %+v", status.Ledgers[0].Latest)
	}
	if report := Render(status); !strings.Contains(report, "! marked dirty") {
		t.Errorf("report should flag it:\n%s", report)
	}
}

// TestLiveSQLiteNoLedger: an ordinary database nothing manages must come back
// with a clean "nothing here" rather than an error or a false positive.
func TestLiveSQLiteNoLedger(t *testing.T) {
	engine := openLedgerEngine(t, `DROP TABLE schema_migrations;`)

	status, err := Detect(context.Background(), engine, "", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(status.Ledgers) != 0 {
		t.Fatalf("found %+v, want nothing", status.Ledgers)
	}
	if report := Render(status); !strings.Contains(report, "No migration ledger found.") {
		t.Errorf("report:\n%s", report)
	}
}

// TestLiveSQLiteHistoryOrder: a Laravel-shaped ledger with several rows, read
// through the real driver, has to come back newest-first with its exact count.
func TestLiveSQLiteHistoryOrder(t *testing.T) {
	engine := openLedgerEngine(t, `
		DROP TABLE schema_migrations;
		CREATE TABLE migrations (id INTEGER PRIMARY KEY, migration TEXT NOT NULL, batch INTEGER NOT NULL);
		INSERT INTO migrations (migration, batch) VALUES
			('2024_10_12_000000_create_users_table', 1),
			('2024_10_12_100000_create_password_resets', 1),
			('2026_07_14_120000_create_orders_table', 2);
	`)

	status, err := Detect(context.Background(), engine, "", 2)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	l := status.Ledgers[0]
	if l.Tool != "Laravel" {
		t.Fatalf("got %q, want Laravel", l.Tool)
	}
	if l.Applied != 3 {
		t.Errorf("applied = %d, want the exact count even though 2 rows were read", l.Applied)
	}
	if len(l.Latest) != 2 || l.Latest[0].Name != "2026_07_14_120000_create_orders_table" {
		t.Fatalf("entries = %+v, want the two newest first", l.Latest)
	}
}
