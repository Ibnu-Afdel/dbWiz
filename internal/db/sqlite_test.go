package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// newSQLiteFile creates a real SQLite database on disk with one seeded table and
// returns its path. It uses the same pure-Go driver the engine uses, so this
// test needs no external service and runs in normal CI.
func newSQLiteFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	pool, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer pool.Close()
	_, err = pool.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT
	);
	INSERT INTO users (name, email) VALUES ('ada', 'ada@x.io'), ('bob', NULL);`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return path
}

func openSQLite(t *testing.T, path string) *SQLite {
	t.Helper()
	e := NewSQLite()
	if err := e.Connect(context.Background(), Target{Path: path}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func TestSQLiteCapabilities(t *testing.T) {
	e := NewSQLite()
	c := e.Capabilities()
	if c.Users || c.Grants || c.MultipleDatabases {
		t.Errorf("SQLite capabilities should all be false, got %+v", c)
	}
	if e.Kind() != KindSQLite {
		t.Errorf("Kind = %v", e.Kind())
	}
}

func TestSQLiteReads(t *testing.T) {
	ctx := context.Background()
	e := openSQLite(t, newSQLiteFile(t))

	tables, err := e.ListTables(ctx, "")
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(tables) != 1 || tables[0].Name != "users" {
		t.Fatalf("ListTables = %+v, want [users]", tables)
	}
	if tables[0].Rows != -1 {
		t.Errorf("SQLite row count should be -1 (unknown), got %d", tables[0].Rows)
	}

	cols, err := e.DescribeTable(ctx, "", "users")
	if err != nil {
		t.Fatalf("DescribeTable: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("DescribeTable returned %d columns, want 3", len(cols))
	}
	if cols[0].Name != "id" || cols[0].Key != "PRI" {
		t.Errorf("col0 = %+v, want id/PRI", cols[0])
	}
	if cols[1].Name != "name" || cols[1].Nullable {
		t.Errorf("col1 = %+v, want name/not-null", cols[1])
	}
	if !cols[2].Nullable {
		t.Errorf("email should be nullable, got %+v", cols[2])
	}

	res, err := e.PreviewRows(ctx, "", "users", 10)
	if err != nil {
		t.Fatalf("PreviewRows: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("PreviewRows returned %d rows, want 2", len(res.Rows))
	}
	// bob's email is NULL and must scan to nil, distinct from an empty string.
	if res.Rows[1][2] != nil {
		t.Errorf("NULL email should be nil, got %#v", res.Rows[1][2])
	}
}

func TestSQLiteQuery(t *testing.T) {
	ctx := context.Background()
	e := openSQLite(t, newSQLiteFile(t))

	res, err := e.Query(ctx, "SELECT count(*) FROM users")
	if err != nil {
		t.Fatalf("Query select: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("count query returned %d rows", len(res.Rows))
	}

	res, err = e.Query(ctx, "INSERT INTO users (name) VALUES ('carol')")
	if err != nil {
		t.Fatalf("Query insert: %v", err)
	}
	if res.RowsAffected != 1 {
		t.Errorf("insert RowsAffected = %d, want 1", res.RowsAffected)
	}

	_, err = e.Query(ctx, "SELECT * FROM nope")
	var dberr *DBError
	if !asDBError(err, &dberr) || dberr.Kind != DBErrObjectMissing {
		t.Errorf("query on missing table = %v, want DBErrObjectMissing", err)
	}
}

func TestSQLiteUnsupported(t *testing.T) {
	ctx := context.Background()
	e := openSQLite(t, newSQLiteFile(t))
	if _, err := e.ListUsers(ctx); !isUnsupported(err) {
		t.Errorf("ListUsers should be unsupported, got %v", err)
	}
	if err := e.CreateDatabase(ctx, "x", CreateOpts{}); !isUnsupported(err) {
		t.Errorf("CreateDatabase should be unsupported, got %v", err)
	}
	if err := e.Grant(ctx, "u", "d", GrantAll); !isUnsupported(err) {
		t.Errorf("Grant should be unsupported, got %v", err)
	}
}

func TestSQLiteNotADatabase(t *testing.T) {
	// A file that exists but isn't a SQLite db must be rejected at Connect.
	path := filepath.Join(t.TempDir(), "notdb.txt")
	if err := os.WriteFile(path, []byte("this is plain text, definitely not sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewSQLite()
	err := e.Connect(context.Background(), Target{Path: path})
	var dberr *DBError
	if !asDBError(err, &dberr) || dberr.Kind != DBErrInvalidInput {
		t.Fatalf("Connect to non-db = %v, want DBErrInvalidInput", err)
	}

	// A missing path is likewise rejected, not created.
	err = e.Connect(context.Background(), Target{Path: filepath.Join(t.TempDir(), "absent.db")})
	if !asDBError(err, &dberr) {
		t.Errorf("Connect to missing path = %v, want *DBError", err)
	}
}

func asDBError(err error, target **DBError) bool {
	if e, ok := err.(*DBError); ok {
		*target = e
		return true
	}
	return false
}

func isUnsupported(err error) bool {
	e, ok := err.(*DBError)
	return ok && e.Kind == DBErrUnsupported
}
