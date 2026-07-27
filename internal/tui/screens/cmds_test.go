package screens

import (
	"path/filepath"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// TestKindOf checks the docker→db engine mapping the connect flow relies on.
func TestKindOf(t *testing.T) {
	cases := []struct {
		in   docker.Engine
		want db.Kind
		ok   bool
	}{
		{docker.EnginePostgres, db.KindPostgres, true},
		{docker.EngineMySQL, db.KindMySQL, true},
		{docker.EngineMariaDB, db.KindMariaDB, true},
		{docker.EngineUnknown, 0, false},
	}
	for _, c := range cases {
		got, ok := kindOf(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("kindOf(%q) = (%v,%v), want (%v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestMergeCreds verifies later ladder rungs only fill gaps, never overwrite a
// value a higher-priority rung already recovered.
func TestMergeCreds(t *testing.T) {
	base := docker.Creds{User: "app"}
	extra := docker.Creds{User: "root", Password: "secret", Database: "db"}
	got := mergeCreds(base, extra)
	if got.User != "app" {
		t.Errorf("User overwritten: got %q, want %q", got.User, "app")
	}
	if got.Password != "secret" || got.Database != "db" {
		t.Errorf("gaps not filled: %+v", got)
	}
}

// TestApplyEngineDefaults checks the conventional admin user/maintenance db are
// backfilled only when the ladder recovered nothing.
func TestApplyEngineDefaults(t *testing.T) {
	pg := db.Target{}
	applyEngineDefaults(&pg, db.KindPostgres)
	if pg.User != "postgres" || pg.Database != "postgres" {
		t.Errorf("postgres defaults: %+v", pg)
	}

	my := db.Target{User: "app"}
	applyEngineDefaults(&my, db.KindMySQL)
	if my.User != "app" {
		t.Errorf("mysql default overwrote user: %+v", my)
	}

	maria := db.Target{}
	applyEngineDefaults(&maria, db.KindMariaDB)
	if maria.User != "root" {
		t.Errorf("mariadb default user: %+v", maria)
	}
}

// TestExpandTilde covers ~ expansion for the SQLite path input.
func TestExpandTilde(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cases := map[string]string{
		"~":              "/home/tester",
		"~/db.sqlite":    "/home/tester/db.sqlite",
		"/abs/path.db":   "/abs/path.db",
		"relative.db":    "relative.db",
		"~notme/file.db": "~notme/file.db", // only ~ and ~/ expand
	}
	for in, want := range cases {
		if got := expandTilde(in); got != want {
			t.Errorf("expandTilde(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLongestCommonPrefix underpins Tab completion.
func TestLongestCommonPrefix(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"dev.sqlite", "dev.db"}, "dev."},
		{[]string{"app.db"}, "app.db"},
		{[]string{"foo", "bar"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := longestCommonPrefix(c.in); got != c.want {
			t.Errorf("longestCommonPrefix(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRememberRecent verifies de-duplication, most-recent-first ordering, and
// the cap on the session recents list.
func TestRememberRecent(t *testing.T) {
	sqliteRecents = nil
	t.Cleanup(func() { sqliteRecents = nil })

	rememberRecent("/a.db")
	rememberRecent("/b.db")
	rememberRecent("/a.db") // re-open moves it to front, no dupe
	if len(sqliteRecents) != 2 {
		t.Fatalf("want 2 recents, got %d: %v", len(sqliteRecents), sqliteRecents)
	}
	if sqliteRecents[0] != "/a.db" || sqliteRecents[1] != "/b.db" {
		t.Errorf("ordering wrong: %v", sqliteRecents)
	}

	for i := range 20 {
		rememberRecent(filepath.Join("/", string(rune('a'+i))+".db"))
	}
	if len(sqliteRecents) > 8 {
		t.Errorf("recents not capped: %d", len(sqliteRecents))
	}
}
