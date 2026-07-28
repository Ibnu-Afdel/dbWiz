//go:build integration

package db

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"
)

// Integration tests run only under `-tags integration` against a live server.
// Point them at one with env vars; defaults match a local Omarchy-style
// container. The connecting user must be a superuser (create db/role).
//
//	KASE_PG_HOST KASE_PG_PORT KASE_PG_USER KASE_PG_PASS KASE_PG_DB
func pgTarget(t *testing.T) Target {
	t.Helper()
	port, _ := strconv.Atoi(env("KASE_PG_PORT", "5432"))
	return Target{
		Host:     env("KASE_PG_HOST", "127.0.0.1"),
		Port:     port,
		User:     env("KASE_PG_USER", "fawz"),
		Password: env("KASE_PG_PASS", "password"),
		Database: env("KASE_PG_DB", "postgres"),
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func connectPG(t *testing.T) *Postgres {
	t.Helper()
	e := NewPostgres()
	if err := e.Connect(context.Background(), pgTarget(t)); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func TestPGConnectAndAuth(t *testing.T) {
	connectPG(t) // connects or fails the test

	bad := pgTarget(t)
	bad.Password = "definitely-wrong-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	e := NewPostgres()
	err := e.Connect(context.Background(), bad)
	var dberr *DBError
	if !errors.As(err, &dberr) || dberr.Kind != DBErrAuthFailed {
		t.Fatalf("bad password Connect = %v, want DBErrAuthFailed", err)
	}
}

func TestPGDatabasesAndAdmin(t *testing.T) {
	ctx := context.Background()
	e := connectPG(t)

	name := "dbwiz_it_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	user := name + "_u"

	if err := e.CreateDatabase(ctx, name, CreateOpts{}); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	t.Cleanup(func() { e.DropDatabase(context.Background(), name) })

	dbs, err := e.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if !hasDB(dbs, name) {
		t.Fatalf("new db %q not in ListDatabases", name)
	}
	for _, tmpl := range []string{"template0", "template1"} {
		if hasDB(dbs, tmpl) {
			t.Errorf("template db %q should be excluded", tmpl)
		}
	}

	// Users + grants.
	if err := e.CreateUser(ctx, user, "pw123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() { e.DropUser(context.Background(), user) })
	if err := e.Grant(ctx, user, name, GrantAll); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := e.Revoke(ctx, user, name, GrantAll); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Per-privilege matrix (v2 3.1): grant CREATE, confirm ListGrants reflects it,
	// then revoke and confirm it's gone. A brand-new role gets CONNECT via PUBLIC,
	// so this asserts on CREATE, which PUBLIC does not carry.
	if err := e.SetGrant(ctx, user, name, PrivCreate, true); err != nil {
		t.Fatalf("SetGrant CREATE: %v", err)
	}
	held, err := e.ListGrants(ctx, user, name)
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if !containsPriv(held, PrivCreate) {
		t.Errorf("after GRANT CREATE, ListGrants = %v, want it to include CREATE", held)
	}
	if err := e.SetGrant(ctx, user, name, PrivCreate, false); err != nil {
		t.Fatalf("SetGrant revoke CREATE: %v", err)
	}
	held, _ = e.ListGrants(ctx, user, name)
	if containsPriv(held, PrivCreate) {
		t.Errorf("after REVOKE CREATE, ListGrants = %v, want CREATE gone", held)
	}
	// An unknown privilege is rejected before it reaches the server.
	if err := e.SetGrant(ctx, user, name, Privilege("DROP TABLE users; --"), true); err == nil {
		t.Error("SetGrant should reject a privilege outside the engine's set")
	}

	// Role flags + password change (v2 3.2): flip CREATEDB on and confirm
	// ListUsers reflects it, then change the password.
	if err := e.AlterUser(ctx, user, true, true); err != nil {
		t.Fatalf("AlterUser: %v", err)
	}
	flagged, err := e.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers after AlterUser: %v", err)
	}
	if !userHasCreateDB(flagged, user) {
		t.Errorf("after AlterUser CREATEDB, %q should report CreateDB=true", user)
	}
	if err := e.SetPassword(ctx, user, "newpw456"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	users, err := e.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if !hasUser(users, user) {
		t.Errorf("new user %q not listed", user)
	}

	// Duplicate create → typed ObjectExists.
	err = e.CreateDatabase(ctx, name, CreateOpts{})
	var dberr *DBError
	if !errors.As(err, &dberr) || dberr.Kind != DBErrObjectExists {
		t.Errorf("duplicate CreateDatabase = %v, want DBErrObjectExists", err)
	}

	// Refuse dropping the connected database.
	if err := e.DropDatabase(ctx, e.current); err == nil {
		t.Error("dropping the connected database should be refused")
	}
}

func TestPGBrowseAndQuery(t *testing.T) {
	ctx := context.Background()
	e := connectPG(t)

	name := "dbwiz_it_browse_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := e.CreateDatabase(ctx, name, CreateOpts{}); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	t.Cleanup(func() { e.Connect(context.Background(), pgTarget(t)); e.DropDatabase(context.Background(), name) })

	// Postgres databases are isolated, so switch the pool into the new database
	// before creating objects in it.
	if err := e.Connect(ctx, withDB(pgTarget(t), name)); err != nil {
		t.Fatalf("reconnect to new db: %v", err)
	}
	if _, err := e.Query(ctx, "CREATE TABLE widgets (id serial primary key, label text not null)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := e.Query(ctx, "INSERT INTO widgets (label) VALUES ('a'), ('b')"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	tables, err := e.ListTables(ctx, name)
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if !hasTable(tables, "widgets") {
		t.Fatalf("widgets not in %+v", tables)
	}

	cols, err := e.DescribeTable(ctx, name, "widgets")
	if err != nil {
		t.Fatalf("DescribeTable: %v", err)
	}
	if len(cols) != 2 || cols[0].Name != "id" || cols[0].Key != "PRI" {
		t.Fatalf("DescribeTable = %+v", cols)
	}
	if cols[1].Nullable {
		t.Errorf("label should be NOT NULL")
	}

	prev, err := e.PreviewRows(ctx, name, "widgets", 10)
	if err != nil {
		t.Fatalf("PreviewRows: %v", err)
	}
	if len(prev.Rows) != 2 {
		t.Errorf("PreviewRows rows = %d, want 2", len(prev.Rows))
	}

	// Exec result reports RowsAffected.
	res, err := e.Query(ctx, "UPDATE widgets SET label = 'x'")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if res.RowsAffected != 2 {
		t.Errorf("update RowsAffected = %d, want 2", res.RowsAffected)
	}

	// Syntax error → typed QuerySyntax.
	_, err = e.Query(ctx, "SELCT 1")
	var dberr *DBError
	if !errors.As(err, &dberr) || dberr.Kind != DBErrQuerySyntax {
		t.Errorf("syntax error = %v, want DBErrQuerySyntax", err)
	}
}

func TestPGQueryCancellation(t *testing.T) {
	e := connectPG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := e.Query(ctx, "SELECT pg_sleep(60)")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("pg_sleep(60) should have been cancelled")
	}
	if elapsed > 5*time.Second {
		t.Errorf("cancellation took %v; expected the server-side query to be killed promptly", elapsed)
	}
	var dberr *DBError
	if !errors.As(err, &dberr) || (dberr.Kind != DBErrCanceled && dberr.Kind != DBErrTimeout) {
		t.Errorf("cancelled query = %v, want Canceled/Timeout", err)
	}
}

// TestPGExtensions covers v3 1.4: the available list reflects the image and
// includes the always-present plpgsql, and CREATE EXTENSION installs a
// contrib extension the stock image ships (uuid-ossp), which then reports as
// installed. Re-running is a no-op (IF NOT EXISTS).
func TestPGExtensions(t *testing.T) {
	e := connectPG(t)
	ctx := context.Background()

	exts, err := e.ListExtensions(ctx, "")
	if err != nil {
		t.Fatalf("ListExtensions: %v", err)
	}
	if !hasExt(exts, "plpgsql") {
		t.Fatalf("expected plpgsql to be available/installed; got %d extensions", len(exts))
	}
	if !hasExt(exts, "uuid-ossp") {
		t.Skip("uuid-ossp not available in this image; skipping install check")
	}

	if err := e.CreateExtension(ctx, "", "uuid-ossp"); err != nil {
		t.Fatalf("CreateExtension uuid-ossp: %v", err)
	}
	// Idempotent.
	if err := e.CreateExtension(ctx, "", "uuid-ossp"); err != nil {
		t.Fatalf("CreateExtension uuid-ossp (again): %v", err)
	}
	exts, err = e.ListExtensions(ctx, "")
	if err != nil {
		t.Fatalf("ListExtensions after create: %v", err)
	}
	for _, x := range exts {
		if x.Name == "uuid-ossp" && !x.Installed() {
			t.Errorf("uuid-ossp should report installed after CREATE EXTENSION")
		}
	}
	// Clean up so the run is repeatable.
	_, _ = e.Query(ctx, `DROP EXTENSION IF EXISTS "uuid-ossp"`)
}

func hasExt(exts []Extension, name string) bool {
	for _, e := range exts {
		if e.Name == name {
			return true
		}
	}
	return false
}

func withDB(tgt Target, db string) Target { tgt.Database = db; return tgt }

func hasDB(dbs []Database, name string) bool {
	for _, d := range dbs {
		if d.Name == name {
			return true
		}
	}
	return false
}

func hasUser(users []User, name string) bool {
	for _, u := range users {
		if u.Name == name {
			return true
		}
	}
	return false
}

func containsPriv(privs []Privilege, p Privilege) bool {
	for _, q := range privs {
		if q == p {
			return true
		}
	}
	return false
}

func userHasCreateDB(users []User, name string) bool {
	for _, u := range users {
		if u.Name == name {
			return u.CreateDB
		}
	}
	return false
}

func hasTable(tables []Table, name string) bool {
	for _, t := range tables {
		if t.Name == name {
			return true
		}
	}
	return false
}
