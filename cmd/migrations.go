package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/migrations"
)

// migrationsTimeout bounds the whole read. It is a table listing, a describe per
// candidate table, and two small reads per ledger — short work, but a busy server
// can still be slow to answer.
const migrationsTimeout = 60 * time.Second

// NewMigrationsCommand builds `dbwiz migrations [database]`: what the database
// itself says about the migrations applied to it (v4 3.3).
func NewMigrationsCommand() *cobra.Command {
	var (
		targetFlag, password, database string
		limit                          int
		all, asJSON                    bool
	)
	cmd := &cobra.Command{
		Use:   "migrations [database]",
		Short: "Show which migrations have been applied to a database",
		Long: "Find the table your migration tool keeps its state in, and report what it\n" +
			"says: which tool, how many migrations are recorded, the most recent ones,\n" +
			"and whether any of them failed or was left half-applied.\n\n" +
			"DBWiz recognises the tables the common tools leave behind — Laravel, Rails,\n" +
			"Django, Prisma, Flyway, golang-migrate, Alembic and a dozen more — and falls\n" +
			"back to reporting any table that looks like a ledger. It is not tied to any\n" +
			"of them: it reads a table, nothing else.\n\n" +
			"Which means this reports what has been applied, and cannot report what is\n" +
			"pending: pending lives in your migration files, which DBWiz never reads.\n\n" +
			"Exits non-zero when a ledger records a migration that didn't apply cleanly,\n" +
			"so `dbwiz migrations || alert` works in a deploy script.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := database
			if len(args) > 0 {
				name = args[0]
			}
			if all {
				limit = -1
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), migrationsTimeout)
			defer cancel()
			return runMigrations(ctx, cmd.OutOrStdout(), migrationsOpts{
				target:   targetFlag,
				password: password,
				database: name,
				limit:    limit,
				asJSON:   asJSON,
			})
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	cmd.Flags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	cmd.Flags().StringVarP(&database, "database", "d", "", "database to inspect (defaults to the target's default)")
	cmd.Flags().IntVarP(&limit, "limit", "n", migrations.DefaultLimit, "how many recent migrations to list")
	cmd.Flags().BoolVar(&all, "all", false, "list every recorded migration")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a text report")
	return cmd
}

// migrationsOpts is one resolved `migrations` invocation.
type migrationsOpts struct {
	target   string
	password string
	database string
	limit    int
	asJSON   bool
}

// runMigrations connects, reads whatever ledger the database has, and writes the
// report. A broken ledger is reported *and then* returned as an error, so the
// detail is already on screen by the time main() sets the exit status — the same
// shape as `dbwiz health` and `dbwiz schema diff`.
func runMigrations(ctx context.Context, out io.Writer, o migrationsOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}
	t, err := resolveTarget(ctx, o.target)
	if err != nil {
		return err
	}
	engine, err := open(ctx, t, o.password)
	if err != nil {
		return err
	}
	defer engine.Close()

	name := o.database
	if name == "" {
		name = t.target.Database
	}
	if t.kind == db.KindSQLite {
		name = "" // one database per file; the name would be meaningless
	}

	status, err := migrations.Detect(ctx, engine, name, o.limit)
	if err != nil {
		return cliError(err)
	}

	if o.asJSON {
		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out, string(data)); err != nil {
			return err
		}
	} else if _, err := io.WriteString(out, migrations.Render(status)); err != nil {
		return err
	}

	if status.Trouble() {
		return errors.New("a migration in this database did not apply cleanly")
	}
	return nil
}
