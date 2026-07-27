package db

// Blank imports register the database/sql drivers DBWiz uses so that
// sql.Open("pgx"|"mysql"|"sqlite", ...) works from the engine implementations
// added in the drivers phase. Registering them here — the package that owns the
// engine layer — keeps the driver dependencies anchored and out of the tui.
//
// pgx is used through its database/sql-compatible stdlib adapter (native pgx API
// only if a later step needs it). modernc.org/sqlite is the pure-Go, CGO-free
// SQLite driver; never mattn/go-sqlite3.
import (
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)
