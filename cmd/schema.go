package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/schema"
)

// NewSchemaCommand builds `dbwiz schema …`: structural tools over a database
// (v4 Phase 1). `schema diff` compares two databases — the dev-vs-staging
// question — and exits non-zero when they differ, so it slots into CI the same
// way `dbwiz health` does.
func NewSchemaCommand() *cobra.Command {
	var targetFlag, password string
	parent := &cobra.Command{
		Use:   "schema",
		Short: "Inspect and compare database structure",
		Long: "Structural tools: capture what tables and columns a database has, and\n" +
			"compare two databases to see what drifted between them.",
		SilenceUsage: true,
	}
	parent.PersistentFlags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	parent.PersistentFlags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")

	parent.AddCommand(
		newSchemaDiffCommand(&targetFlag, &password),
		newSchemaDumpCommand(&targetFlag, &password),
	)
	return parent
}

// newSchemaDumpCommand builds `dbwiz schema dump`: the DDL of a database's
// tables, in a form that can be replayed. It is not a data dump — `dbwiz dump`
// (v2 4.4) is the one that carries rows.
func newSchemaDumpCommand(targetFlag, password *string) *cobra.Command {
	var database, table, output string

	cmd := &cobra.Command{
		Use:   "dump [database]",
		Short: "Export a database's structure as DDL",
		Long: "Print the CREATE TABLE statements for every table in a database (and the\n" +
			"CREATE INDEX statements that go with them), in sorted order.\n\n" +
			"Each engine answers from the source that is exact for it: SHOW CREATE TABLE\n" +
			"on MySQL, the stored statement on SQLite, and a catalog-rebuilt statement on\n" +
			"Postgres, which has no SHOW CREATE TABLE.\n\n" +
			"This is structure only — no rows. Use `dbwiz dump` for data.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := database
			if len(args) > 0 {
				name = args[0]
			}
			return runSchemaDump(cmd.Context(), cmd.OutOrStdout(), schemaDumpOpts{
				target:   *targetFlag,
				password: *password,
				database: name,
				table:    table,
				output:   output,
			})
		},
	}
	cmd.Flags().StringVarP(&database, "database", "d", "", "database to export (defaults to the target's default)")
	cmd.Flags().StringVar(&table, "table", "", "export only this table")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to this file instead of stdout")
	return cmd
}

// schemaDumpOpts is one resolved `schema dump` invocation.
type schemaDumpOpts struct {
	target   string
	password string
	database string
	table    string
	output   string
}

// runSchemaDump connects, asks the engine for each table's DDL, and writes the
// statements out. Writing to a file goes through the same path as stdout, with
// the confirmation printed to stderr so a redirected dump stays clean.
func runSchemaDump(ctx context.Context, out io.Writer, o schemaDumpOpts) error {
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

	ddler, ok := engine.(db.DDLer)
	if !ok {
		return fmt.Errorf("%s can't export DDL", t.kind)
	}

	name := o.database
	if name == "" {
		name = t.target.Database
	}
	if t.kind == db.KindSQLite {
		name = "" // one database per file; the name would be meaningless
	}

	refs, err := dumpTargets(ctx, engine, name, o.table)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return fmt.Errorf("no tables to export in %s", sideLabel(t, name))
	}

	var doc strings.Builder
	fmt.Fprintf(&doc, "-- DBWiz schema dump of %s (%s)\n-- structure only; no rows\n\n", sideLabel(t, name), t.kind)
	for i, ref := range refs {
		ddl, err := ddler.TableDDL(ctx, name, ref)
		if err != nil {
			return cliError(err)
		}
		doc.WriteString(ddl)
		doc.WriteString("\n")
		if i < len(refs)-1 {
			doc.WriteString("\n")
		}
	}

	if o.output == "" {
		_, err := io.WriteString(out, doc.String())
		return err
	}
	if err := os.WriteFile(o.output, []byte(doc.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", o.output, err)
	}
	fmt.Fprintf(os.Stderr, "Wrote the structure of %s to %s (%d table(s)).\n", sideLabel(t, name), o.output, len(refs))
	return nil
}

// dumpTargets lists the table references to export, in the same sorted,
// schema-qualified form Capture uses — so `schema dump` and `schema diff` always
// agree about which tables exist and what they are called.
func dumpTargets(ctx context.Context, src schema.Source, database, only string) ([]string, error) {
	snap, err := schema.Capture(ctx, src, database)
	if err != nil {
		return nil, cliError(err)
	}
	var refs []string
	for _, t := range snap.Tables {
		ref := t.Qualified()
		if only != "" && ref != only && t.Name != only {
			continue
		}
		refs = append(refs, ref)
	}
	if only != "" && len(refs) == 0 {
		return nil, fmt.Errorf("no table named %q in %s", only, database)
	}
	return refs, nil
}

// newSchemaDiffCommand builds `dbwiz schema diff`. The two sides are given as
// database names; --against-target / --against-file move the second side to a
// different server or SQLite file, which covers the three real cases: two
// databases on one server, the same database on two servers, and two SQLite
// files.
func newSchemaDiffCommand(targetFlag, password *string) *cobra.Command {
	var againstTarget, againstFile, againstPassword string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "diff [database-a] [database-b]",
		Short: "Compare the structure of two databases",
		Long: "Compare two databases table by table and column by column, and report what\n" +
			"would have to change to turn the first into the second.\n\n" +
			"Both sides default to the resolved target's default database, so:\n" +
			"  dbwiz schema diff dev staging                 two databases on one server\n" +
			"  dbwiz schema diff app --against-target prod   the same database on two servers\n" +
			"  dbwiz schema diff --against-file old.db       two SQLite files\n\n" +
			"Only structure is compared — never row data. Exits 0 when the two match and\n" +
			"1 when they differ, so `dbwiz schema diff dev staging || notify` works.",
		Args:         cobra.MaximumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if againstTarget != "" && againstFile != "" {
				return errors.New("pass either --against-target or --against-file, not both")
			}
			var dbA, dbB string
			if len(args) > 0 {
				dbA = args[0]
			}
			if len(args) > 1 {
				dbB = args[1]
			}
			return runSchemaDiff(cmd.Context(), cmd.OutOrStdout(), schemaDiffOpts{
				target:          *targetFlag,
				password:        *password,
				dbA:             dbA,
				dbB:             dbB,
				againstTarget:   againstTarget,
				againstFile:     againstFile,
				againstPassword: againstPassword,
				asJSON:          asJSON,
			})
		},
	}
	cmd.Flags().StringVar(&againstTarget, "against-target", "", "compare against a database on this container instead")
	cmd.Flags().StringVar(&againstFile, "against-file", "", "compare against this SQLite file instead")
	cmd.Flags().StringVar(&againstPassword, "against-password", "", "password for --against-target (defaults to the same resolution as --password)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a text report")
	return cmd
}

// schemaDiffOpts is the resolved invocation, kept as a struct so runSchemaDiff
// stays testable without a cobra command.
type schemaDiffOpts struct {
	target          string
	password        string
	dbA, dbB        string
	againstTarget   string
	againstFile     string
	againstPassword string
	asJSON          bool
}

// runSchemaDiff captures both sides, compares them, prints the report, and
// signals difference through the exit code. The report is printed before the
// "they differ" error is returned, so the detail is already on screen by the
// time main() sets the exit status — the same shape as `dbwiz health`.
func runSchemaDiff(ctx context.Context, out io.Writer, o schemaDiffOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}

	from, closeA, err := captureSide(ctx, o.target, o.password, o.dbA, false, "")
	if err != nil {
		return err
	}
	defer closeA()

	// The second side reuses the first side's target unless redirected.
	targetB, passwordB := o.target, o.password
	if o.againstTarget != "" {
		targetB = o.againstTarget
	}
	if o.againstPassword != "" {
		passwordB = o.againstPassword
	}
	dbB := o.dbB
	if dbB == "" {
		dbB = o.dbA // "same database, other server" is the common cross-target case
	}

	to, closeB, err := captureSide(ctx, targetB, passwordB, dbB, o.againstFile != "", o.againstFile)
	if err != nil {
		return err
	}
	defer closeB()

	if from.Label == to.Label {
		return fmt.Errorf("both sides resolve to %s — name a second database, or pass --against-target/--against-file", from.Label)
	}

	report := schema.Diff(from, to)

	if o.asJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
	} else {
		fmt.Fprint(out, schema.Render(report))
	}

	if !report.Identical() {
		return errors.New("the databases differ")
	}
	return nil
}

// captureSide opens one side of the comparison and snapshots it. When file is
// set the side is a SQLite path opened directly (no Docker involved); otherwise
// the normal target resolver runs. The returned close function is always safe to
// call.
func captureSide(ctx context.Context, target, password, database string, isFile bool, file string) (schema.Snapshot, func(), error) {
	noop := func() {}

	t := cliTarget{}
	if isFile {
		t = cliTarget{sqlite: true, kind: db.KindSQLite, target: db.Target{Path: file}}
	} else {
		resolved, err := resolveTarget(ctx, target)
		if err != nil {
			return schema.Snapshot{}, noop, err
		}
		t = resolved
	}

	engine, err := open(ctx, t, password)
	if err != nil {
		return schema.Snapshot{}, noop, err
	}
	closeFn := func() { _ = engine.Close() }

	name := database
	if name == "" {
		name = t.target.Database
	}
	// SQLite hosts one database per file, so a database name would be meaningless
	// there — the file itself is the side.
	if t.kind == db.KindSQLite {
		name = ""
	}

	snap, err := schema.Capture(ctx, engine, name)
	if err != nil {
		closeFn()
		return schema.Snapshot{}, noop, cliError(err)
	}
	snap.Label = sideLabel(t, name)
	return snap, closeFn, nil
}

// sideLabel names a side of the diff the way the user would: the SQLite file
// path, or container/database for a server.
func sideLabel(t cliTarget, database string) string {
	if t.sqlite {
		return t.target.Path
	}
	if database == "" {
		return t.label()
	}
	return t.label() + "/" + database
}
