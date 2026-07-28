package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// execDocker runs a command inside a container, streaming stdin/stdout. It's a
// package var so tests can capture the request without a live Docker; production
// never reassigns it.
var execDocker = docker.Exec

// NewDumpCommand builds `dbwiz dump <database>`: a logical backup produced by the
// container's own pg_dump/mysqldump via `docker exec`. Shelling out is the right
// call here (and the only place in DBWiz it is) — the dump tools ship inside the
// database image and speak its exact version. The dump streams to stdout by
// default (so you can pipe or redirect it) or to a file with --output.
func NewDumpCommand() *cobra.Command {
	var targetFlag, password, output string
	cmd := &cobra.Command{
		Use:   "dump <database>",
		Short: "Back up a database with its container's pg_dump/mysqldump",
		Long: "Dump a database to a plain-text SQL backup, produced by the container's own\n" +
			"pg_dump (Postgres) or mysqldump (MySQL/MariaDB) via `docker exec`.\n\n" +
			"The dump streams to stdout by default; use --output to write a file. Works\n" +
			"on Docker targets only — SQLite has no container to exec into.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveTarget(cmd.Context(), targetFlag)
			if err != nil {
				return err
			}
			return runDump(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), t, args[0], password, output)
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	cmd.Flags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write the dump to a file instead of stdout")
	return cmd
}

// runDump streams a dump of dbname. To stdout the dump is the only output (a
// confirmation would corrupt it); to a file the data goes to the file and a
// one-line confirmation goes to stderr.
func runDump(ctx context.Context, stdout, stderr io.Writer, t cliTarget, dbname, password, output string) error {
	spec, err := dumpSpec(t, dbname, passwordFor(password, t))
	if err != nil {
		return err
	}

	sink := stdout
	var file *os.File
	if output != "" {
		file, err = os.Create(output)
		if err != nil {
			return err
		}
		defer file.Close()
		sink = file
	}

	if err := execDocker(ctx, docker.ExecOptions{
		Container: t.container.Name,
		Env:       spec.env,
		Stdout:    sink,
		Args:      spec.args,
	}); err != nil {
		return cliError(err)
	}
	if file != nil {
		if err := file.Close(); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "Dumped %s from %s to %s\n", dbname, t.container.Name, output)
	}
	return nil
}

// NewRestoreCommand builds `dbwiz restore <database> <file> --yes`: it feeds a
// SQL dump into the container's psql/mysql over stdin. Restoring overwrites the
// target database's contents, so — like `drop` — it refuses to run without an
// explicit --yes, the scripting stand-in for a "you're sure?" prompt.
func NewRestoreCommand() *cobra.Command {
	var targetFlag, password string
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore <database> <file>",
		Short: "Restore a SQL dump into a database (requires --yes)",
		Long: "Restore a plain-text SQL dump into a database by streaming it into the\n" +
			"container's own psql (Postgres) or mysql (MySQL/MariaDB) via `docker exec`.\n\n" +
			"This writes into an existing database and can overwrite data, so it refuses\n" +
			"to run without --yes. Works on Docker targets only.",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing to restore into %q without --yes (this can overwrite existing data)", args[0])
			}
			t, err := resolveTarget(cmd.Context(), targetFlag)
			if err != nil {
				return err
			}
			return runRestore(cmd.Context(), cmd.OutOrStdout(), t, args[0], args[1], passwordFor(password, t))
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	cmd.Flags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the restore (required)")
	return cmd
}

// runRestore streams file into dbname via the container's restore tool.
func runRestore(ctx context.Context, out io.Writer, t cliTarget, dbname, file, password string) error {
	spec, err := restoreSpec(t, dbname, password)
	if err != nil {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := execDocker(ctx, docker.ExecOptions{
		Container: t.container.Name,
		Env:       spec.env,
		Stdin:     f,
		Args:      spec.args,
	}); err != nil {
		return cliError(err)
	}
	fmt.Fprintf(out, "Restored %s into %s on %s\n", file, dbname, t.container.Name)
	return nil
}

// execSpec is a resolved docker-exec request: the command to run in the
// container plus the KEY=VALUE env pairs it needs (a password, forwarded by name
// so it never lands in the argv).
type execSpec struct {
	args []string
	env  []string
}

// dumpSpec builds the pg_dump/mysqldump invocation for a target. SQLite has no
// container to exec into, so it's an explicit, friendly error rather than a
// confusing empty-container failure.
func dumpSpec(t cliTarget, dbname, password string) (execSpec, error) {
	if err := validBackupDB(t, dbname); err != nil {
		return execSpec{}, err
	}
	switch t.kind {
	case db.KindPostgres:
		return execSpec{
			args: []string{"pg_dump", "-U", t.target.User, dbname},
			env:  pgEnv(password),
		}, nil
	case db.KindMySQL, db.KindMariaDB:
		return execSpec{
			args: []string{"mysqldump", "-u", t.target.User, dbname},
			env:  mysqlEnv(password),
		}, nil
	default:
		return execSpec{}, errNoContainerBackup
	}
}

// restoreSpec builds the psql/mysql invocation that reads a dump from stdin. For
// Postgres, ON_ERROR_STOP makes a mid-restore failure fail the command rather
// than press on silently.
func restoreSpec(t cliTarget, dbname, password string) (execSpec, error) {
	if err := validBackupDB(t, dbname); err != nil {
		return execSpec{}, err
	}
	switch t.kind {
	case db.KindPostgres:
		return execSpec{
			args: []string{"psql", "-U", t.target.User, "-d", dbname, "-v", "ON_ERROR_STOP=1"},
			env:  pgEnv(password),
		}, nil
	case db.KindMySQL, db.KindMariaDB:
		return execSpec{
			args: []string{"mysql", "-u", t.target.User, dbname},
			env:  mysqlEnv(password),
		}, nil
	default:
		return execSpec{}, errNoContainerBackup
	}
}

// errNoContainerBackup explains why dump/restore don't apply to SQLite.
var errNoContainerBackup = fmt.Errorf("dump/restore work on Docker database containers only (they use the container's own tools); SQLite has no container to exec into")

// validBackupDB rejects a target/name that dump-restore can't safely use: a
// non-container (SQLite) target, an invalid identifier, or a name that a CLI
// would parse as a flag (a leading dash), which would silently mis-target the
// dump.
func validBackupDB(t cliTarget, dbname string) error {
	if t.sqlite || t.container.Name == "" {
		return errNoContainerBackup
	}
	if err := db.ValidateIdent(dbname); err != nil {
		return err
	}
	if strings.HasPrefix(dbname, "-") {
		return fmt.Errorf("database name %q can't start with a dash", dbname)
	}
	return nil
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

// mysqlEnv forwards a MySQL password via MYSQL_PWD, the variable
// mysqldump/mysql read, avoiding the insecure `-p<pass>` on the command line.
func mysqlEnv(password string) []string {
	if password == "" {
		return nil
	}
	return []string{"MYSQL_PWD=" + password}
}

// passwordFor resolves the effective password with the same precedence open()
// uses: an explicit --password, then DBWIZ_PASSWORD, then whatever the credential
// ladder recovered for the target.
func passwordFor(flag string, t cliTarget) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("DBWIZ_PASSWORD"); env != "" {
		return env
	}
	return t.target.Password
}
