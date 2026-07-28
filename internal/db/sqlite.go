package db

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ibnu-Afdel/dbwiz/internal/debuglog"
)

// sqliteMagic is the 16-byte header every SQLite 3 database file begins with.
// Checking it lets Connect reject a path that exists but isn't a database with a
// clear message, instead of a cryptic error on the first query.
var sqliteMagic = []byte("SQLite format 3\x00")

// SQLite is the Engine implementation for local SQLite files via
// modernc.org/sqlite — the pure-Go, CGO-free driver (registered as "sqlite").
// It is a single file with no server, so it reports no user, grant, or
// multi-database capability; the tui hides those panels rather than showing
// stubs.
type SQLite struct {
	pool *sql.DB
	path string
}

// NewSQLite returns an unconnected SQLite engine.
func NewSQLite() *SQLite { return &SQLite{} }

var _ Engine = (*SQLite)(nil)

func (s *SQLite) Kind() Kind { return KindSQLite }

// wrap classifies err, returning a true nil error when err is nil.
func (s *SQLite) wrap(err error) error {
	if err == nil {
		return nil
	}
	return classifySQLite(err)
}

func (s *SQLite) Capabilities() Capabilities {
	return Capabilities{Users: false, Grants: false, MultipleDatabases: false}
}

// Connect opens target.Path after verifying it exists and carries the SQLite
// header. An empty or freshly created zero-length file is accepted (SQLite
// writes the header on first use); anything else with the wrong magic is
// rejected as "not a database".
func (s *SQLite) Connect(ctx context.Context, target Target) error {
	if target.Path == "" {
		return errNotADatabase("", nil)
	}
	if err := validateSQLiteFile(target.Path); err != nil {
		return err
	}
	debuglog.LogConnect("sqlite", "", 0, "", target.Path)
	// mode=rw opens without creating; the file's existence was just verified.
	pool, err := sql.Open("sqlite", target.Path)
	if err != nil {
		return classifySQLite(err)
	}
	// SQLite allows one writer; a single connection avoids "database is locked"
	// churn and keeps the file handle count predictable.
	pool.SetMaxOpenConns(1)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		_ = pool.Close()
		return classifySQLite(err)
	}

	if s.pool != nil {
		_ = s.pool.Close()
	}
	s.pool = pool
	s.path = target.Path
	return nil
}

// validateSQLiteFile confirms path is a readable file whose header is the SQLite
// magic (or is empty, which SQLite treats as a new database).
func validateSQLiteFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return errNotADatabase(path, err)
	}
	if info.IsDir() {
		return errNotADatabase(path, fmt.Errorf("path is a directory"))
	}
	if info.Size() == 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return errNotADatabase(path, err)
	}
	defer f.Close()
	header := make([]byte, len(sqliteMagic))
	if _, err := f.Read(header); err != nil {
		return errNotADatabase(path, err)
	}
	if !bytes.Equal(header, sqliteMagic) {
		return errNotADatabase(path, nil)
	}
	return nil
}

func (s *SQLite) Close() error {
	if s.pool == nil {
		return nil
	}
	err := s.pool.Close()
	s.pool = nil
	return err
}

// ListDatabases returns the single file as one entry so the browser has a label,
// even though MultipleDatabases is false.
func (s *SQLite) ListDatabases(ctx context.Context) ([]Database, error) {
	return []Database{{Name: filepath.Base(s.path)}}, nil
}

func (s *SQLite) CreateDatabase(ctx context.Context, name string, opts CreateOpts) error {
	return errUnsupported("creating databases", KindSQLite)
}

func (s *SQLite) DropDatabase(ctx context.Context, name string) error {
	return errUnsupported("dropping databases", KindSQLite)
}

func (s *SQLite) ListTables(ctx context.Context, database string) ([]Table, error) {
	// sqlite_master lists schema objects; hide the internal sqlite_* tables. Row
	// counts aren't cheaply available, so report -1 (unknown).
	const q = `SELECT name FROM sqlite_master
	           WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
	           ORDER BY name`
	rows, err := s.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, classifySQLite(err)
	}
	defer rows.Close()
	var out []Table
	for rows.Next() {
		t := Table{Rows: -1}
		if err := rows.Scan(&t.Name); err != nil {
			return nil, classifySQLite(err)
		}
		out = append(out, t)
	}
	return out, s.wrap(rows.Err())
}

func (s *SQLite) DescribeTable(ctx context.Context, database, table string) ([]Column, error) {
	if err := validateIdent(table); err != nil {
		return nil, err
	}
	// PRAGMA table_info can't be parameterised; the identifier is validated and
	// quoted. Columns: cid, name, type, notnull, dflt_value, pk.
	q := fmt.Sprintf("PRAGMA table_info(%s)", quoteSQLiteIdent(table))
	rows, err := s.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, classifySQLite(err)
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, classifySQLite(err)
		}
		key := ""
		if pk > 0 {
			key = "PRI"
		}
		out = append(out, Column{
			Name:     name,
			Type:     ctype,
			Nullable: notnull == 0,
			Key:      key,
		})
	}
	return out, s.wrap(rows.Err())
}

func (s *SQLite) PreviewRows(ctx context.Context, database, table string, limit int) (Result, error) {
	if err := validateIdent(table); err != nil {
		return Result{}, err
	}
	if limit <= 0 {
		limit = 100
	}
	q := fmt.Sprintf("SELECT * FROM %s LIMIT %d", quoteSQLiteIdent(table), limit)
	res, err := runSQL(ctx, s.pool, q)
	if err != nil {
		return Result{}, classifySQLite(err)
	}
	return res, nil
}

func (s *SQLite) ListUsers(ctx context.Context) ([]User, error) {
	return nil, errUnsupported("users", KindSQLite)
}

func (s *SQLite) CreateUser(ctx context.Context, name, password string) error {
	return errUnsupported("users", KindSQLite)
}

func (s *SQLite) DropUser(ctx context.Context, name string) error {
	return errUnsupported("users", KindSQLite)
}

func (s *SQLite) Grant(ctx context.Context, user, database string, level GrantLevel) error {
	return errUnsupported("grants", KindSQLite)
}

func (s *SQLite) Revoke(ctx context.Context, user, database string, level GrantLevel) error {
	return errUnsupported("grants", KindSQLite)
}

func (s *SQLite) AlterUser(ctx context.Context, name string, canLogin, createDB bool) error {
	return errUnsupported("users", KindSQLite)
}

func (s *SQLite) SetPassword(ctx context.Context, name, password string) error {
	return errUnsupported("users", KindSQLite)
}

func (s *SQLite) DatabasePrivileges() []Privilege { return nil }

func (s *SQLite) ListGrants(ctx context.Context, user, database string) ([]Privilege, error) {
	return nil, errUnsupported("grants", KindSQLite)
}

func (s *SQLite) SetGrant(ctx context.Context, user, database string, priv Privilege, grant bool) error {
	return errUnsupported("grants", KindSQLite)
}

func (s *SQLite) Query(ctx context.Context, sql string) (Result, error) {
	res, err := runSQL(ctx, s.pool, sql)
	if err != nil {
		return Result{}, classifySQLite(err)
	}
	return res, nil
}
