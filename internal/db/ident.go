package db

import (
	"strings"

	"github.com/jackc/pgx/v5"
)

// maxIdentLen bounds an identifier before it ever reaches the server. Postgres
// truncates at 63 bytes and MySQL at 64; 64 is the permissive ceiling. This is a
// sanity guard, not the security boundary — quoting is. See quote* below.
const maxIdentLen = 64

// validateIdent rejects an identifier that could not be a safe database, user,
// or table name before it is quoted and interpolated into SQL. Quoting (the
// quote* helpers) is what actually prevents injection; this rejects the inputs
// that even correct quoting cannot make sensible: empty names, over-long names,
// and names carrying NUL or control characters (which no engine accepts and
// which would corrupt the statement). Callers pass raw names; every engine runs
// them through here first.
func validateIdent(name string) error {
	if name == "" {
		return errInvalidIdent(name, "it is empty")
	}
	if len(name) > maxIdentLen {
		return errInvalidIdent(name, "it is longer than 64 bytes")
	}
	for _, r := range name {
		if r == 0 {
			return errInvalidIdent(name, "it contains a NUL byte")
		}
		if r < 0x20 || r == 0x7f {
			return errInvalidIdent(name, "it contains a control character")
		}
	}
	return nil
}

// ValidateIdent is the exported form of validateIdent, letting the tui layer
// validate a name live as the user types (in the create-database/user forms)
// with the exact same rules the engines enforce before building SQL. It returns
// a typed *DBError (as error) on rejection so callers get plain-language text.
func ValidateIdent(name string) error { return validateIdent(name) }

// quotePGIdent double-quotes an identifier for Postgres, escaping embedded
// quotes. pgx.Identifier.Sanitize is the canonical implementation; using it
// keeps quoting identical to what the driver itself would do.
func quotePGIdent(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// quoteMySQLIdent backtick-quotes an identifier for MySQL/MariaDB, doubling any
// embedded backtick.
func quoteMySQLIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// quoteSQLiteIdent double-quotes an identifier for SQLite, doubling any embedded
// double quote.
func quoteSQLiteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quotePGLiteral renders s as a Postgres single-quoted string literal. With
// standard_conforming_strings on (the default since 9.1) a backslash is an
// ordinary character, so only the single quote needs doubling. Used for values
// that cannot be parameterised — notably CREATE ROLE ... PASSWORD.
func quotePGLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// quoteMySQLLiteral renders s as a MySQL single-quoted string literal. MySQL
// honours backslash escapes by default, so both the backslash and the single
// quote are doubled.
func quoteMySQLLiteral(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "''")
	return "'" + s + "'"
}
