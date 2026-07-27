// Package db defines the engine abstraction over the databases DBWiz drives
// (Postgres, MySQL/MariaDB, SQLite). It is pure logic plus database I/O: it
// must never import the tui package. The tui layer depends on db, not the
// reverse.
package db

import "context"

// Kind identifies a database engine family. The UI uses it for labels and to
// map a detected container to a concrete Engine implementation.
type Kind int

const (
	KindPostgres Kind = iota
	KindMySQL
	KindMariaDB
	KindSQLite
)

// String returns the human-facing name of the engine kind.
func (k Kind) String() string {
	switch k {
	case KindPostgres:
		return "PostgreSQL"
	case KindMySQL:
		return "MySQL"
	case KindMariaDB:
		return "MariaDB"
	case KindSQLite:
		return "SQLite"
	default:
		return "unknown"
	}
}

// Capabilities describes which features an engine supports. Capability flags
// drive the UI directly: a false flag means the corresponding panel is simply
// not shown — no stub screen, no error. SQLite reports all of these false.
type Capabilities struct {
	Users             bool // engine has a concept of database users/roles
	Grants            bool // engine supports GRANT/REVOKE
	MultipleDatabases bool // engine hosts more than one database per server
}

// Target describes how to reach a database. For server engines the network
// fields apply; for SQLite only Path is used.
type Target struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string // initial database to connect to (maintenance DB for admin ops)

	Path string // SQLite file path
}

// Database is a single database (schema) hosted by an engine.
type Database struct {
	Name  string
	Owner string
}

// Table is a single table within a database.
type Table struct {
	Schema string // empty for engines without schemas
	Name   string
	Rows   int64 // estimated row count; -1 when unknown
}

// Column describes one column of a table for the browser's describe view.
type Column struct {
	Name     string
	Type     string
	Nullable bool
	Key      string // "PRI", "FK", "" etc. — engine-specific label
}

// User is a database user/role.
type User struct {
	Name string
}

// CreateOpts carries options for CreateDatabase. In v1 only Owner is used.
type CreateOpts struct {
	Owner string // user to own the new database; empty means engine default
}

// GrantLevel is the coarse permission level applied by Grant/Revoke. v1 only
// implements GrantAll; the full matrix arrives in v2.
type GrantLevel int

const (
	GrantAll GrantLevel = iota
)

// Engine is the single abstraction every database driver implements. All
// methods take a context so long operations are cancellable from the TUI.
//
// One live connection exists per selected target; it is closed and replaced on
// switch. Identifier arguments (database/user/table names) are validated and
// quoted per engine inside the implementation — callers pass raw names.
type Engine interface {
	Kind() Kind
	Capabilities() Capabilities

	// Connect opens the single live connection to target. Close releases it.
	Connect(ctx context.Context, target Target) error
	Close() error

	ListDatabases(ctx context.Context) ([]Database, error)
	CreateDatabase(ctx context.Context, name string, opts CreateOpts) error
	DropDatabase(ctx context.Context, name string) error

	// Browse pillar.
	ListTables(ctx context.Context, database string) ([]Table, error)
	DescribeTable(ctx context.Context, database, table string) ([]Column, error)
	PreviewRows(ctx context.Context, database, table string, limit int) (Result, error)

	ListUsers(ctx context.Context) ([]User, error)
	CreateUser(ctx context.Context, name, password string) error
	DropUser(ctx context.Context, name string) error
	Grant(ctx context.Context, user, database string, level GrantLevel) error
	Revoke(ctx context.Context, user, database string, level GrantLevel) error

	// Query runs an arbitrary statement. Cancellation flows through ctx.
	Query(ctx context.Context, sql string) (Result, error)
}
