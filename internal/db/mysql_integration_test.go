//go:build integration

package db

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// MySQL integration tests. Enable with KASE_MY=1 (they need a running MySQL or
// MariaDB, which the Postgres tests don't require). Defaults match a local
// mysql8 container; the user must be able to create databases and users.
//
//	KASE_MY_HOST KASE_MY_PORT KASE_MY_USER KASE_MY_PASS
func myTarget(t *testing.T) Target {
	t.Helper()
	if env("KASE_MY", "") == "" {
		t.Skip("set KASE_MY=1 to run MySQL integration tests")
	}
	port, _ := strconv.Atoi(env("KASE_MY_PORT", "3306"))
	return Target{
		Host:     env("KASE_MY_HOST", "127.0.0.1"),
		Port:     port,
		User:     env("KASE_MY_USER", "root"),
		Password: env("KASE_MY_PASS", "password"),
	}
}

// newMyEngine builds the engine under test. KASE_MY_KIND=mariadb exercises the
// MariaDB label/path; anything else uses MySQL. Both share the implementation.
func newMyEngine() *MySQL {
	if env("KASE_MY_KIND", "mysql") == "mariadb" {
		return NewMariaDB()
	}
	return NewMySQL()
}

func connectMy(t *testing.T) *MySQL {
	t.Helper()
	e := newMyEngine()
	if err := e.Connect(context.Background(), myTarget(t)); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func TestMyConnectAndAuth(t *testing.T) {
	connectMy(t)

	bad := myTarget(t)
	bad.Password = "wrong-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	e := newMyEngine()
	err := e.Connect(context.Background(), bad)
	var dberr *DBError
	if !errors.As(err, &dberr) || dberr.Kind != DBErrAuthFailed {
		t.Fatalf("bad password Connect = %v, want DBErrAuthFailed", err)
	}
}

func TestMyDatabasesAdminBrowseQuery(t *testing.T) {
	ctx := context.Background()
	e := connectMy(t)

	name := "dbwiz_it_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	user := "dbwiz_u_" + strconv.FormatInt(time.Now().UnixNano()%100000, 10)

	if err := e.CreateDatabase(ctx, name, CreateOpts{}); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	t.Cleanup(func() { e.DropDatabase(context.Background(), name) })

	dbs, err := e.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if !hasDB(dbs, name) {
		t.Fatalf("new db %q not listed", name)
	}
	for _, sys := range []string{"information_schema", "mysql", "performance_schema", "sys"} {
		if hasDB(dbs, sys) {
			t.Errorf("system schema %q should be excluded", sys)
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
	if users, err := e.ListUsers(ctx); err != nil || !hasUser(users, user) {
		t.Fatalf("ListUsers missing %q (err=%v)", user, err)
	}

	// Browse: create a table in the new schema and inspect it.
	create := "CREATE TABLE " + quoteMySQLIdent(name) + ".widgets (id INT PRIMARY KEY AUTO_INCREMENT, label VARCHAR(50) NOT NULL)"
	if _, err := e.Query(ctx, create); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := e.Query(ctx, "INSERT INTO "+quoteMySQLIdent(name)+".widgets (label) VALUES ('a'),('b')"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	tables, err := e.ListTables(ctx, name)
	if err != nil || !hasTable(tables, "widgets") {
		t.Fatalf("ListTables = %+v (err=%v)", tables, err)
	}
	cols, err := e.DescribeTable(ctx, name, "widgets")
	if err != nil {
		t.Fatalf("DescribeTable: %v", err)
	}
	if len(cols) != 2 || cols[0].Name != "id" || cols[0].Key != "PRI" {
		t.Fatalf("DescribeTable = %+v", cols)
	}
	prev, err := e.PreviewRows(ctx, name, "widgets", 10)
	if err != nil || len(prev.Rows) != 2 {
		t.Fatalf("PreviewRows = %+v (err=%v)", prev.Rows, err)
	}

	// Syntax error → typed QuerySyntax.
	_, err = e.Query(ctx, "SELCT 1")
	var dberr *DBError
	if !errors.As(err, &dberr) || dberr.Kind != DBErrQuerySyntax {
		t.Errorf("syntax error = %v, want DBErrQuerySyntax", err)
	}
}
