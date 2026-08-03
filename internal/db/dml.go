package db

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoRowIdentity is returned by the row-mutation builders when a table has no
// primary key, so the row to change can't be pinned down safely. The browser
// surfaces this as a plain "can't edit — no primary key" notice rather than
// risking a WHERE that matches many rows (v3 2.1/2.2).
var ErrNoRowIdentity = errors.New("table has no primary key to identify the row")

// BuildUpdate constructs an UPDATE that sets one column of a single row to newVal,
// identifying that row by its primary-key columns. Identifiers are quoted for the
// engine; values render as quoted literals (a nil *newVal is NULL; a nil key
// value becomes IS NULL), matching how the browser reads cells back as text. The
// SQL is returned so the browser can show it before it runs (v3 2.1) and then
// execute it via Engine.ExecMutation.
func BuildUpdate(kind Kind, database, table string, keyCols []string, keyVals []any, setCol string, newVal *string) (string, error) {
	if len(keyCols) == 0 {
		return "", ErrNoRowIdentity
	}
	q := quoterFor(kind)
	where, err := buildWhere(q, keyCols, keyVals)
	if err != nil {
		return "", err
	}
	set := q.ident(setCol) + " = " + q.value(newVal)
	return fmt.Sprintf("UPDATE %s SET %s WHERE %s", q.table(database, table), set, where), nil
}

// BuildDelete constructs a DELETE that removes the single row identified by its
// primary-key columns (v3 2.2), quoted for the engine. Like BuildUpdate it
// refuses without a key so a DELETE can't sweep more than the intended row.
func BuildDelete(kind Kind, database, table string, keyCols []string, keyVals []any) (string, error) {
	if len(keyCols) == 0 {
		return "", ErrNoRowIdentity
	}
	q := quoterFor(kind)
	where, err := buildWhere(q, keyCols, keyVals)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("DELETE FROM %s WHERE %s", q.table(database, table), where), nil
}

// BuildInsert constructs an INSERT for the given columns and values (v3 2.2).
// Identifiers are quoted; a nil value inserts NULL. Columns the caller leaves out
// of cols aren't named, so their database default (a sequence, a DEFAULT clause)
// applies — which is how a serial primary key stays untouched.
func BuildInsert(kind Kind, database, table string, cols []string, vals []*string) (string, error) {
	if len(cols) == 0 {
		return "", errors.New("no columns given to insert")
	}
	if len(cols) != len(vals) {
		return "", fmt.Errorf("columns (%d) and values (%d) don't line up", len(cols), len(vals))
	}
	q := quoterFor(kind)
	idents := make([]string, len(cols))
	lits := make([]string, len(vals))
	for i := range cols {
		idents[i] = q.ident(cols[i])
		lits[i] = q.value(vals[i])
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		q.table(database, table), strings.Join(idents, ", "), strings.Join(lits, ", ")), nil
}

// BuildTruncate empties a table (v3 2.3). Postgres and MySQL use TRUNCATE; SQLite
// has no TRUNCATE, so an unqualified DELETE (which SQLite optimises into a fast
// whole-table drop) stands in.
func BuildTruncate(kind Kind, database, table string) string {
	q := quoterFor(kind)
	if kind == KindSQLite {
		return "DELETE FROM " + q.table(database, table)
	}
	return "TRUNCATE TABLE " + q.table(database, table)
}

// BuildCount builds an exact row count for a table (v3 2.3), naming the result
// column "count" so the caller can read it back by position regardless of engine.
func BuildCount(kind Kind, database, table string) string {
	return "SELECT COUNT(*) AS count FROM " + quoterFor(kind).table(database, table)
}

// BuildSelect builds the browser's row query with an optional raw WHERE filter
// (v3 2.3). The table is quoted; the WHERE fragment is the user's own SQL, passed
// through verbatim (the same trust model as the query editor — it's their
// database). A non-positive limit omits the LIMIT clause.
func BuildSelect(kind Kind, database, table, where string, limit int) string {
	sql := "SELECT * FROM " + quoterFor(kind).table(database, table)
	if strings.TrimSpace(where) != "" {
		sql += " WHERE " + where
	}
	if limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", limit)
	}
	return sql
}

// quoter renders identifiers, literals, and qualified table names for one engine
// family, reusing the same quoting the drivers use elsewhere so a generated
// statement is quoted identically to a hand-built one.
type quoter struct{ kind Kind }

func quoterFor(kind Kind) quoter { return quoter{kind} }

// ident quotes an identifier (column/table) for the engine.
func (q quoter) ident(name string) string {
	switch q.kind {
	case KindMySQL, KindMariaDB:
		return quoteMySQLIdent(name)
	case KindSQLite:
		return quoteSQLiteIdent(name)
	default:
		return quotePGIdent(name)
	}
}

// literal quotes a string as a SQL string literal for the engine. Postgres and
// SQLite share single-quote doubling; MySQL also escapes backslashes.
func (q quoter) literal(s string) string {
	switch q.kind {
	case KindMySQL, KindMariaDB:
		return quoteMySQLLiteral(s)
	default:
		return quotePGLiteral(s)
	}
}

// value renders an optional new value: nil is the SQL keyword NULL, otherwise a
// quoted string literal. The browser hands values back as text (see gather), so a
// string literal is the right rendering; typed columns coerce it on the server.
func (q quoter) value(v *string) string {
	if v == nil {
		return "NULL"
	}
	return q.literal(*v)
}

// table qualifies a table name the way each engine's browse methods do: MySQL and
// MariaDB prefix the schema (database) when one is known; Postgres relies on the
// ExecMutation-selected connection plus search_path, and SQLite has one database,
// so both leave the table unqualified.
func (q quoter) table(database, table string) string {
	if (q.kind == KindMySQL || q.kind == KindMariaDB) && database != "" {
		return quoteMySQLIdent(database) + "." + quoteMySQLIdent(table)
	}
	return q.ident(table)
}

// QuoteIdent quotes an identifier for the engine. It is the exported form of the
// quoting the builders above already do, for a caller that composes a read they
// don't cover — the migration-ledger reader's ORDER BY (v4 3.1). Quoting stays
// here so a hand-composed statement is quoted exactly like a generated one.
func QuoteIdent(kind Kind, name string) string { return quoterFor(kind).ident(name) }

// TableRef renders a qualified table reference for the engine. It extends
// quoter.table with the one case the mutation builders never needed: a Postgres
// table outside the connection's search_path, which has to name its schema in the
// FROM clause or the read simply fails. MySQL keeps qualifying by database (its
// schema and database are one namespace) and SQLite qualifies by neither.
func TableRef(kind Kind, database, schema, table string) string {
	q := quoterFor(kind)
	if kind == KindPostgres && schema != "" {
		return q.ident(schema) + "." + q.ident(table)
	}
	return q.table(database, table)
}

// buildWhere renders an AND of equality (or IS NULL) predicates pinning a row by
// its key columns. A nil value becomes IS NULL because "= NULL" never matches.
func buildWhere(q quoter, cols []string, vals []any) (string, error) {
	if len(cols) != len(vals) {
		return "", fmt.Errorf("key columns (%d) and values (%d) don't line up", len(cols), len(vals))
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		lit, isNull := q.cell(vals[i])
		if isNull {
			parts[i] = q.ident(c) + " IS NULL"
		} else {
			parts[i] = q.ident(c) + " = " + lit
		}
	}
	return strings.Join(parts, " AND "), nil
}

// cell renders a grid cell value as a literal for a WHERE/VALUES clause. nil (SQL
// NULL) reports isNull so the caller can choose IS NULL; []byte and other types
// are stringified then quoted, mirroring how the browser displays them.
func (q quoter) cell(v any) (lit string, isNull bool) {
	switch t := v.(type) {
	case nil:
		return "", true
	case string:
		return q.literal(t), false
	case []byte:
		return q.literal(string(t)), false
	default:
		return q.literal(fmt.Sprint(t)), false
	}
}
