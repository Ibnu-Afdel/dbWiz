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
	RoleFlags         bool // engine has togglable role flags (Postgres LOGIN/CREATEDB)
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

// User is a database user/role. CanLogin and CreateDB are only meaningful when
// the engine reports Capabilities.RoleFlags (Postgres); other engines leave them
// zero.
type User struct {
	Name     string
	CanLogin bool
	CreateDB bool
}

// CreateOpts carries options for CreateDatabase. In v1 only Owner is used.
type CreateOpts struct {
	Owner string // user to own the new database; empty means engine default
}

// GrantLevel is the coarse permission level applied by Grant/Revoke: a single
// "everything on this database" grant. It backs the create-database-with-user
// shortcut; the per-privilege matrix (v2 3.1) goes through SetGrant instead.
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

	// AlterUser sets an existing role's flags (v2 3.2). Only meaningful where
	// Capabilities.RoleFlags is true (Postgres LOGIN/CREATEDB); other engines
	// return DBErrUnsupported. SetPassword changes an existing user's password;
	// available on any engine with Users.
	AlterUser(ctx context.Context, name string, canLogin, createDB bool) error
	SetPassword(ctx context.Context, name, password string) error
	Grant(ctx context.Context, user, database string, level GrantLevel) error
	Revoke(ctx context.Context, user, database string, level GrantLevel) error

	// Grant matrix (v2 3.1), database scope. DatabasePrivileges reports the
	// privilege columns this engine offers, in display order (empty for engines
	// without grants). ListGrants reports which of those the user currently holds
	// on database — the effective privilege where the engine can report it
	// (Postgres folds in PUBLIC/role inheritance). SetGrant grants (grant=true) or
	// revokes one privilege; priv must be one of DatabasePrivileges.
	DatabasePrivileges() []Privilege
	ListGrants(ctx context.Context, user, database string) ([]Privilege, error)
	SetGrant(ctx context.Context, user, database string, priv Privilege, grant bool) error

	// Query runs an arbitrary statement. Cancellation flows through ctx.
	Query(ctx context.Context, sql string) (Result, error)

	// ExecMutation runs a data-modifying statement (built by BuildUpdate/
	// BuildInsert/BuildDelete) against a specific database, selecting it first
	// where the engine needs to — Postgres reconnects to database, MySQL/SQLite
	// take it as already qualified in the SQL. It is separate from Query so the
	// browser's write actions (v3 2.x) don't depend on whatever database the
	// ad-hoc query pool happens to be pointed at. Cancellation flows through ctx.
	ExecMutation(ctx context.Context, database, sql string) (Result, error)
}
