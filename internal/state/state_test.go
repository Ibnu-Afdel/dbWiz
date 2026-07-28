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

// TestHistoryKeyDistinguishesTargets verifies docker and sqlite targets produce
// distinct, kind-prefixed keys so their histories never collide.
func TestHistoryKeyDistinguishesTargets(t *testing.T) {
	docker := HistoryKey(Target{Kind: KindDocker, Container: "pg-dev"})
	sqlite := HistoryKey(Target{Kind: KindSQLite, Path: "/tmp/dev.sqlite"})
	if docker != "docker:pg-dev" {
		t.Errorf("docker key = %q", docker)
	}
	if sqlite != "sqlite:/tmp/dev.sqlite" {
		t.Errorf("sqlite key = %q", sqlite)
	}
	if docker == sqlite {
		t.Error("docker and sqlite keys must differ")
	}
}

// TestAddHistoryRoundTrips verifies statements persist newest-first and reload.
func TestAddHistoryRoundTrips(t *testing.T) {
	hermetic(t)
	const key = "docker:pg-dev"
	for _, sql := range []string{"SELECT 1", "SELECT 2", "SELECT 3"} {
		if err := AddHistory(key, sql); err != nil {
			t.Fatal(err)
		}
	}
	got := History(key)
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(got), got)
	}
	if got[0].SQL != "SELECT 3" || got[2].SQL != "SELECT 1" {
		t.Errorf("expected newest-first, got %q…%q", got[0].SQL, got[2].SQL)
	}
	if got[0].At == 0 {
		t.Error("entry should be timestamped")
	}
}

// TestAddHistoryDropsConsecutiveDuplicate verifies re-running the same statement
// doesn't grow the list — it just refreshes the newest entry.
func TestAddHistoryDropsConsecutiveDuplicate(t *testing.T) {
	hermetic(t)
	const key = "docker:pg-dev"
	for range 3 {
		if err := AddHistory(key, "SELECT now()"); err != nil {
			t.Fatal(err)
		}
	}
	if got := History(key); len(got) != 1 {
		t.Fatalf("consecutive duplicates should collapse to 1, got %d", len(got))
	}
	// A different statement in between means both are kept.
	_ = AddHistory(key, "SELECT 1")
	_ = AddHistory(key, "SELECT now()")
	if got := History(key); len(got) != 3 {
		t.Fatalf("want 3 after interleaving, got %d", len(got))
	}
}

// TestAddHistoryCaps verifies the per-target list never grows past historyCap.
func TestAddHistoryCaps(t *testing.T) {
	hermetic(t)
	const key = "docker:pg-dev"
	for i := range historyCap + 25 {
		if err := AddHistory(key, "SELECT "+string(rune('a'+i%26))+string(rune('0'+i/26))); err != nil {
			t.Fatal(err)
		}
	}
	if got := History(key); len(got) != historyCap {
		t.Fatalf("history exceeded cap: %d", len(got))
	}
}

// TestHistoryIsolatedPerTarget verifies two targets keep separate histories.
func TestHistoryIsolatedPerTarget(t *testing.T) {
	hermetic(t)
	_ = AddHistory("docker:a", "SELECT 'a'")
	_ = AddHistory("docker:b", "SELECT 'b'")
	if a := History("docker:a"); len(a) != 1 || a[0].SQL != "SELECT 'a'" {
		t.Errorf("target a leaked: %+v", a)
	}
	if b := History("docker:b"); len(b) != 1 || b[0].SQL != "SELECT 'b'" {
		t.Errorf("target b leaked: %+v", b)
	}
}

// TestHistoryMissingIsEmpty verifies an unknown key yields no entries, not a panic.
func TestHistoryMissingIsEmpty(t *testing.T) {
	hermetic(t)
	if got := History("docker:never-seen"); got != nil {
		t.Errorf("expected nil for unknown key, got %+v", got)
	}
}

// TestAddSavedRoundTrips verifies named queries persist newest-first and reload
// with their name, SQL, and a timestamp.
func TestAddSavedRoundTrips(t *testing.T) {
	hermetic(t)
	const key = "docker:pg-dev"
	if err := AddSaved(key, "customers", "SELECT * FROM customers"); err != nil {
		t.Fatal(err)
	}
	if err := AddSaved(key, "orders", "SELECT * FROM orders"); err != nil {
		t.Fatal(err)
	}
	got := Saved(key)
	if len(got) != 2 {
		t.Fatalf("want 2 saved, got %d: %+v", len(got), got)
	}
	if got[0].Name != "orders" || got[0].SQL != "SELECT * FROM orders" {
		t.Errorf("expected newest-first, got %+v", got[0])
	}
	if got[0].At == 0 {
		t.Error("saved query should be timestamped")
	}
}

// TestAddSavedUpsertsByName verifies re-saving under an existing name replaces
// its SQL and floats it to the top rather than duplicating.
func TestAddSavedUpsertsByName(t *testing.T) {
	hermetic(t)
	const key = "docker:pg-dev"
	_ = AddSaved(key, "q", "SELECT 1")
	_ = AddSaved(key, "other", "SELECT 2")
	_ = AddSaved(key, "q", "SELECT 3")
	got := Saved(key)
	if len(got) != 2 {
		t.Fatalf("upsert should not duplicate: %+v", got)
	}
	if got[0].Name != "q" || got[0].SQL != "SELECT 3" {
		t.Errorf("re-save should update SQL and move to front, got %+v", got[0])
	}
}

// TestAddSavedIgnoresBlank verifies a blank name or statement is a no-op.
func TestAddSavedIgnoresBlank(t *testing.T) {
	hermetic(t)
	_ = AddSaved("docker:pg-dev", "", "SELECT 1")
	_ = AddSaved("docker:pg-dev", "named", "")
	if got := Saved("docker:pg-dev"); len(got) != 0 {
		t.Fatalf("blank saves should be ignored, got %+v", got)
	}
}

// TestAddSavedCaps verifies the per-scope list never grows past savedCap.
func TestAddSavedCaps(t *testing.T) {
	hermetic(t)
	const key = "docker:pg-dev"
	for i := range savedCap + 15 {
		if err := AddSaved(key, "q"+string(rune('a'+i%26))+string(rune('0'+i/26)), "SELECT 1"); err != nil {
			t.Fatal(err)
		}
	}
	if got := Saved(key); len(got) != savedCap {
		t.Fatalf("saved exceeded cap: %d", len(got))
	}
}

// TestSavedGlobalIsSeparateFromTargets verifies global queries live in their own
// scope and don't bleed into a target's list (or vice versa).
func TestSavedGlobalIsSeparateFromTargets(t *testing.T) {
	hermetic(t)
	_ = AddSaved("", "everywhere", "SELECT version()")
	_ = AddSaved("docker:pg-dev", "here", "SELECT 1")
	if g := GlobalSaved(); len(g) != 1 || g[0].Name != "everywhere" {
		t.Errorf("global scope wrong: %+v", g)
	}
	if pt := Saved("docker:pg-dev"); len(pt) != 1 || pt[0].Name != "here" {
		t.Errorf("per-target scope wrong: %+v", pt)
	}
}

// TestDeleteSavedRemovesByName verifies delete drops the named entry from the
// right scope and leaves others intact, and that emptying a target's list drops
// the map key.
func TestDeleteSavedRemovesByName(t *testing.T) {
	hermetic(t)
	const key = "docker:pg-dev"
	_ = AddSaved(key, "keep", "SELECT 1")
	_ = AddSaved(key, "drop", "SELECT 2")
	_ = AddSaved("", "gkeep", "SELECT 3")

	if err := DeleteSaved(key, "drop"); err != nil {
		t.Fatal(err)
	}
	got := Saved(key)
	if len(got) != 1 || got[0].Name != "keep" {
		t.Fatalf("delete removed wrong entry: %+v", got)
	}
	// Deleting a missing name is a no-op.
	if err := DeleteSaved(key, "never"); err != nil {
		t.Fatal(err)
	}
	if len(Saved(key)) != 1 {
		t.Error("deleting a missing name should be a no-op")
	}
	// Global scope untouched by a per-target delete.
	if g := GlobalSaved(); len(g) != 1 || g[0].Name != "gkeep" {
		t.Errorf("global scope disturbed: %+v", g)
	}
	// Emptying the target's list drops the key entirely.
	if err := DeleteSaved(key, "keep"); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load().Saved[key]; ok {
		t.Error("emptied target key should be removed from the map")
	}
}

// TestSavedMissingIsEmpty verifies an unknown key yields no entries, not a panic.
func TestSavedMissingIsEmpty(t *testing.T) {
	hermetic(t)
	if got := Saved("docker:never-seen"); got != nil {
		t.Errorf("expected nil for unknown key, got %+v", got)
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
