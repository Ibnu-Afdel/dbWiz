package connect

import (
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
		got, ok := KindOf(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("KindOf(%q) = (%v,%v), want (%v,%v)", c.in, got, ok, c.want, c.ok)
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
	ApplyEngineDefaults(&pg, db.KindPostgres)
	if pg.User != "postgres" || pg.Database != "postgres" {
		t.Errorf("postgres defaults: %+v", pg)
	}

	my := db.Target{User: "app"}
	ApplyEngineDefaults(&my, db.KindMySQL)
	if my.User != "app" {
		t.Errorf("mysql default overwrote user: %+v", my)
	}

	maria := db.Target{}
	ApplyEngineDefaults(&maria, db.KindMariaDB)
	if maria.User != "root" {
		t.Errorf("mariadb default user: %+v", maria)
	}
}
