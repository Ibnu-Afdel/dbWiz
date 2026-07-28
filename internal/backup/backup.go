// Package backup builds the dump and restore commands DBWiz runs inside a
// database container via `docker exec` (v2 4.4 / v3 2.4). It is the one place the
// per-engine tool choice lives — pg_dump/psql for Postgres, mysqldump/mysql for
// MySQL/MariaDB — so the CLI (`dbwiz dump`/`restore`) and the TUI backup screen
// produce identical commands.
//
// A password is returned as a KEY=VALUE env pair, meant to be forwarded to docker
// by name only (docker.ExecOptions), so it never lands in the argv. The package
// imports db for the engine kind but not docker or tui.
package backup

import (
	"errors"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// ErrUnsupported means the engine has no in-container dump/restore tool DBWiz
// drives — SQLite (no container) or anything not Postgres/MySQL/MariaDB.
var ErrUnsupported = errors.New("dump/restore is available on Docker Postgres, MySQL, and MariaDB only")

// Command is a tool invocation to run inside a container: the argv plus the env
// (KEY=VALUE, forwarded by name) it needs.
type Command struct {
	Args []string
	Env  []string
}

// Dump builds the logical-backup command (pg_dump / mysqldump) that writes the
// database to stdout.
func Dump(kind db.Kind, user, database, password string) (Command, error) {
	switch kind {
	case db.KindPostgres:
		return Command{Args: []string{"pg_dump", "-U", user, database}, Env: pgEnv(password)}, nil
	case db.KindMySQL, db.KindMariaDB:
		return Command{Args: []string{"mysqldump", "-u", user, database}, Env: mysqlEnv(password)}, nil
	}
	return Command{}, ErrUnsupported
}

// Restore builds the command (psql / mysql) that reads a dump from stdin into the
// database. For Postgres, ON_ERROR_STOP makes a mid-restore failure fail the
// command rather than press on silently.
func Restore(kind db.Kind, user, database, password string) (Command, error) {
	switch kind {
	case db.KindPostgres:
		return Command{Args: []string{"psql", "-U", user, "-d", database, "-v", "ON_ERROR_STOP=1"}, Env: pgEnv(password)}, nil
	case db.KindMySQL, db.KindMariaDB:
		return Command{Args: []string{"mysql", "-u", user, database}, Env: mysqlEnv(password)}, nil
	}
	return Command{}, ErrUnsupported
}

// pgEnv forwards a Postgres password via PGPASSWORD, the variable pg_dump/psql
// read. An empty password forwards nothing (the container may trust local
// connections).
func pgEnv(password string) []string {
	if password == "" {
		return nil
	}
	return []string{"PGPASSWORD=" + password}
}

// mysqlEnv forwards a MySQL password via MYSQL_PWD, avoiding the insecure
// `-p<pass>` on the command line.
func mysqlEnv(password string) []string {
	if password == "" {
		return nil
	}
	return []string{"MYSQL_PWD=" + password}
}
