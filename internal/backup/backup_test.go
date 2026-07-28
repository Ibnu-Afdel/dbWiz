package backup

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// TestDumpPostgres forwards the password by env (never in the argv).
func TestDumpPostgres(t *testing.T) {
	c, err := Dump(db.KindPostgres, "postgres", "shop", "s3cret")
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if !reflect.DeepEqual(c.Args, []string{"pg_dump", "-U", "postgres", "shop"}) {
		t.Errorf("args = %v", c.Args)
	}
	if !reflect.DeepEqual(c.Env, []string{"PGPASSWORD=s3cret"}) {
		t.Errorf("env = %v", c.Env)
	}
	if strings.Contains(strings.Join(c.Args, " "), "s3cret") {
		t.Error("password must not appear in argv")
	}
}

// TestRestoreMySQL reads from stdin via mysql, MYSQL_PWD in env.
func TestRestoreMySQL(t *testing.T) {
	c, err := Restore(db.KindMySQL, "root", "shop", "pw")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !reflect.DeepEqual(c.Args, []string{"mysql", "-u", "root", "shop"}) {
		t.Errorf("args = %v", c.Args)
	}
	if !reflect.DeepEqual(c.Env, []string{"MYSQL_PWD=pw"}) {
		t.Errorf("env = %v", c.Env)
	}
}

// TestUnsupported rejects SQLite (no container tool).
func TestUnsupported(t *testing.T) {
	if _, err := Dump(db.KindSQLite, "", "x", ""); !errors.Is(err, ErrUnsupported) {
		t.Errorf("dump: want ErrUnsupported, got %v", err)
	}
	if _, err := Restore(db.KindSQLite, "", "x", ""); !errors.Is(err, ErrUnsupported) {
		t.Errorf("restore: want ErrUnsupported, got %v", err)
	}
}

// TestNoPasswordNoEnv forwards nothing when there's no password (trust auth).
func TestNoPasswordNoEnv(t *testing.T) {
	c, _ := Dump(db.KindPostgres, "postgres", "shop", "")
	if len(c.Env) != 0 {
		t.Errorf("no password should forward no env, got %v", c.Env)
	}
}
