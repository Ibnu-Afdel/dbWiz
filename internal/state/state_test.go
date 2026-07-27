package state

import (
	"os"
	"path/filepath"
	"testing"
)

// hermetic points the state file at a throwaway dir so tests never read or write
// the developer's real ~/.local/state.
func hermetic(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// TestLoadMissingIsEmpty verifies a missing file yields an empty state, not an
// error — the app must start regardless.
func TestLoadMissingIsEmpty(t *testing.T) {
	hermetic(t)
	if got := Load(); got.Last != nil || len(got.RecentSQLite) != 0 {
		t.Fatalf("expected empty state, got %+v", got)
	}
}

// TestLoadCorruptIsEmpty verifies a garbage file is treated as empty rather than
// propagating a parse error.
func TestLoadCorruptIsEmpty(t *testing.T) {
	hermetic(t)
	path, _ := statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got.Last != nil {
		t.Fatalf("corrupt state should load empty, got %+v", got)
	}
}

// TestSetLastDockerRoundTrips verifies a saved Docker target reloads intact.
func TestSetLastDockerRoundTrips(t *testing.T) {
	hermetic(t)
	if err := SetLastDocker("pg-dev", "postgres"); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if got.Last == nil || got.Last.Kind != KindDocker || got.Last.Container != "pg-dev" || got.Last.Engine != "postgres" {
		t.Fatalf("unexpected last target: %+v", got.Last)
	}
	if got.Last.Label() != "pg-dev (postgres)" {
		t.Errorf("label = %q", got.Last.Label())
	}
}

// TestSetLastSQLiteAlsoRecords verifies opening a SQLite file both sets it as the
// last target and adds it to the recent list.
func TestSetLastSQLiteAlsoRecords(t *testing.T) {
	hermetic(t)
	if err := SetLastSQLite("/tmp/dev.sqlite"); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if got.Last == nil || got.Last.Kind != KindSQLite || got.Last.Path != "/tmp/dev.sqlite" {
		t.Fatalf("unexpected last target: %+v", got.Last)
	}
	if len(got.RecentSQLite) != 1 || got.RecentSQLite[0] != "/tmp/dev.sqlite" {
		t.Fatalf("recent list = %v", got.RecentSQLite)
	}
	if got.Last.Label() != "dev.sqlite" {
		t.Errorf("label = %q", got.Last.Label())
	}
}

// TestAddRecentDedupesAndCaps verifies the recent list moves a re-opened path to
// the front, keeps no duplicates, and never grows past the cap.
func TestAddRecentDedupesAndCaps(t *testing.T) {
	hermetic(t)
	for i := range recentCap + 3 {
		if err := AddRecentSQLite(filepath.Join("/tmp", "f", string(rune('a'+i))+".sqlite")); err != nil {
			t.Fatal(err)
		}
	}
	// Re-open an earlier one: it should jump to the front, not duplicate.
	first := Load().RecentSQLite[0]
	if err := AddRecentSQLite(Load().RecentSQLite[2]); err != nil {
		t.Fatal(err)
	}
	got := Load().RecentSQLite
	if len(got) > recentCap {
		t.Fatalf("recent list exceeded cap: %d", len(got))
	}
	if got[0] == first {
		t.Errorf("re-opened path should move to front, still %q", got[0])
	}
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Errorf("duplicate in recent list: %q", p)
		}
		seen[p] = true
	}
}
