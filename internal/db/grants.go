package db

import (
	"fmt"
	"slices"
)

// Privilege is a single database-scope permission that can be granted to or
// revoked from a user. Its string value is the exact SQL keyword the engine
// uses (e.g. "SELECT", "CONNECT"). A Privilege is only ever interpolated into a
// GRANT/REVOKE statement after being checked against the engine's own
// DatabasePrivileges set (knownPrivilege) — never straight from caller input.
// That whitelist check is the security boundary for the privilege token, the
// same role identifier quoting plays for names (see ident.go).
type Privilege string

// Postgres database-scope privileges. GRANT ... ON DATABASE accepts exactly
// these three; the table/column privileges (SELECT/INSERT/…) live at object
// scope and are a later concern — v2 3.1 ships the database matrix only.
const (
	PrivConnect   Privilege = "CONNECT"
	PrivCreate    Privilege = "CREATE" // also a MySQL schema privilege
	PrivTemporary Privilege = "TEMPORARY"
)

// MySQL schema-scope privileges, applied to `db`.* — the database-wide grants an
// application user typically needs. Kept to the common, comprehensible set
// rather than every server privilege.
const (
	PrivSelect Privilege = "SELECT"
	PrivInsert Privilege = "INSERT"
	PrivUpdate Privilege = "UPDATE"
	PrivDelete Privilege = "DELETE"
	PrivDrop   Privilege = "DROP"
	PrivAlter  Privilege = "ALTER"
	PrivIndex  Privilege = "INDEX"
)

// pgDatabasePrivileges and mysqlDatabasePrivileges are the ordered privilege
// columns each engine's grant matrix shows.
var (
	pgDatabasePrivileges    = []Privilege{PrivConnect, PrivCreate, PrivTemporary}
	mysqlDatabasePrivileges = []Privilege{PrivSelect, PrivInsert, PrivUpdate, PrivDelete, PrivCreate, PrivDrop, PrivAlter, PrivIndex}
)

// knownPrivilege reports whether p is one of set. It is the whitelist gate every
// SetGrant runs before interpolating a privilege keyword into SQL.
func knownPrivilege(set []Privilege, p Privilege) bool {
	return slices.Contains(set, p)
}

// errInvalidPrivilege rejects a privilege token that isn't in the engine's
// database-scope set, before it can reach a statement.
func errInvalidPrivilege(p Privilege) *DBError {
	return &DBError{
		Kind:   DBErrInvalidInput,
		Title:  "Unknown privilege",
		Detail: fmt.Sprintf("%q isn't a privilege this engine grants at database scope.", string(p)),
		Hint:   "pick one of the listed privileges",
	}
}
