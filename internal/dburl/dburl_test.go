package dburl

import (
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

func pgTarget() db.Target {
	return db.Target{Host: "127.0.0.1", Port: 5432, User: "postgres", Password: "s3cret", Database: "shop"}
}

func TestURLPostgres(t *testing.T) {
	got := URL(db.KindPostgres, pgTarget())
	want := "postgresql://postgres:s3cret@127.0.0.1:5432/shop"
	if got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

// TestURLNoPassword must not leave a dangling "user:@" when there's no password
// (the Omarchy trust-auth / empty-root case).
func TestURLNoPassword(t *testing.T) {
	got := URL(db.KindPostgres, db.Target{Port: 5432, User: "postgres", Database: "postgres"})
	want := "postgresql://postgres@127.0.0.1:5432/postgres"
	if got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestURLMariaDBSpeaksMySQL(t *testing.T) {
	got := URL(db.KindMariaDB, db.Target{Port: 3306, User: "root"})
	if !strings.HasPrefix(got, "mysql://") {
		t.Errorf("MariaDB URL should use mysql scheme: %q", got)
	}
}

func TestURLSQLite(t *testing.T) {
	got := URL(db.KindSQLite, db.Target{Path: "/data/app.db"})
	if got != "sqlite:///data/app.db" {
		t.Errorf("sqlite URL = %q", got)
	}
}

func TestRenderEnvBlock(t *testing.T) {
	got, err := Render(db.KindMySQL, db.Target{Port: 3306, User: "root", Database: "shop"}, FormatEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"DB_CONNECTION=mysql", "DB_HOST=127.0.0.1", "DB_PORT=3306", "DB_DATABASE=shop", "DB_USERNAME=root", "DB_PASSWORD="} {
		if !strings.Contains(got, must) {
			t.Errorf("env block missing %q:\n%s", must, got)
		}
	}
}

func TestRenderEnvSQLiteHasNoHost(t *testing.T) {
	got, _ := Render(db.KindSQLite, db.Target{Path: "/data/app.db"}, FormatEnv)
	if strings.Contains(got, "DB_HOST") {
		t.Errorf("sqlite env block should have no host:\n%s", got)
	}
	if !strings.Contains(got, "DB_DATABASE=/data/app.db") {
		t.Errorf("sqlite env block should carry the path:\n%s", got)
	}
}

func TestRenderJDBC(t *testing.T) {
	got, _ := Render(db.KindPostgres, pgTarget(), FormatJDBC)
	if !strings.HasPrefix(got, "jdbc:postgresql://127.0.0.1:5432/shop?") {
		t.Errorf("jdbc = %q", got)
	}
	if !strings.Contains(got, "user=postgres") || !strings.Contains(got, "password=s3cret") {
		t.Errorf("jdbc should carry credentials as params: %q", got)
	}
}

func TestRenderURLFormat(t *testing.T) {
	got, _ := Render(db.KindPostgres, pgTarget(), FormatURL)
	if !strings.HasPrefix(got, "DATABASE_URL=postgresql://") {
		t.Errorf("url format = %q", got)
	}
}

func TestParseFormat(t *testing.T) {
	for _, s := range []string{"url", "env", "jdbc"} {
		if _, err := ParseFormat(s); err != nil {
			t.Errorf("ParseFormat(%q) errored: %v", s, err)
		}
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Error("expected error for unknown format")
	}
}
