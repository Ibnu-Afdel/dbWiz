package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestSQLiteLifecycleNoLeak proves the single-live-pool invariant: reconnecting
// an engine closes the previous pool, and a final Close drains it to zero open
// connections. It uses SQLite so it needs no external server. The test lives in
// package db so it can read the unexported pool's Stats().
func TestSQLiteLifecycleNoLeak(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "life.db")
	// An empty file is a valid (new) SQLite database; create it so Connect, which
	// opens without creating, has something to open.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewSQLite()
	var prev *sql.DB
	for i := range 50 {
		if err := e.Connect(ctx, Target{Path: path}); err != nil {
			t.Fatalf("iter %d connect: %v", i, err)
		}
		// Force a real connection to open so the leak check has something to see.
		if _, err := e.Query(ctx, "SELECT 1"); err != nil {
			t.Fatalf("iter %d query: %v", i, err)
		}
		if prev != nil && prev.Stats().OpenConnections != 0 {
			t.Fatalf("iter %d: previous pool still has %d open connections",
				i, prev.Stats().OpenConnections)
		}
		prev = e.pool
	}

	last := e.pool
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := last.Stats().OpenConnections; got != 0 {
		t.Errorf("after Close, pool has %d open connections, want 0", got)
	}
	if e.pool != nil {
		t.Error("pool should be nil after Close")
	}
	// Close is idempotent.
	if err := e.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
