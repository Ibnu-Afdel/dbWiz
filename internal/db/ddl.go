package db

import "context"

// DDLer is implemented by engines that can hand back the DDL for one table —
// the CREATE TABLE statement, plus whatever separate statements that table's
// indexes need. Like Extensioner it is an optional interface callers type-assert
// (eng.(db.DDLer)) rather than a method on Engine.
//
// It is deliberately not derived from DescribeTable: the browse view's columns
// carry name, type, nullability and key, which is enough to *compare* two tables
// (that is what internal/schema does) but not enough to *recreate* one — no
// defaults, no identity/serial, no check or foreign-key constraints, no indexes.
// Each engine therefore answers from the source that is exact for it:
//
//   - MySQL/MariaDB — SHOW CREATE TABLE, the server's own rendering.
//   - SQLite — the original text in sqlite_master, which is what the file stores.
//   - Postgres — has no SHOW CREATE TABLE, so the statement is rebuilt from the
//     catalog (pg_attribute + pg_get_constraintdef + pg_get_indexdef).
//
// The returned string is one or more complete, semicolon-terminated statements
// and never has a trailing newline; callers join tables with a blank line.
type DDLer interface {
	TableDDL(ctx context.Context, database, table string) (string, error)
}
