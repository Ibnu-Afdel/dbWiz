package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// NewExtCommand builds `dbwiz ext …`: manage a PostgreSQL server's extensions
// (v3 1.4). `ext list` shows what the running image makes available and which
// are installed; `ext create <name>` installs one, explaining up front when the
// image can't provide it (postgis needs a PostGIS image) instead of surfacing a
// raw server error. Extensions are a Postgres feature, so the command refuses a
// non-Postgres target.
func NewExtCommand() *cobra.Command {
	var targetFlag, password, database string
	parent := &cobra.Command{
		Use:   "ext",
		Short: "Manage PostgreSQL extensions (list, create)",
		Long: "List and install PostgreSQL extensions. `ext list` reflects what the running\n" +
			"image provides (postgis appears only on a PostGIS image); `ext create` installs\n" +
			"one, explaining when the image can't provide it.",
		SilenceUsage: true,
	}
	parent.PersistentFlags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	parent.PersistentFlags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	parent.PersistentFlags().StringVarP(&database, "database", "d", "", "database to act in (defaults to the target's default)")

	var installedOnly bool
	list := &cobra.Command{
		Use:          "list",
		Short:        "List available and installed extensions",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ext, dbname, closeFn, err := openExtensioner(cmd.Context(), targetFlag, password, database)
			if err != nil {
				return err
			}
			defer closeFn()
			return runExtList(cmd.Context(), cmd.OutOrStdout(), ext, dbname, installedOnly)
		},
	}
	list.Flags().BoolVar(&installedOnly, "installed", false, "show only installed extensions")

	create := &cobra.Command{
		Use:          "create <name>",
		Short:        "Install an extension (CREATE EXTENSION)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ext, dbname, closeFn, err := openExtensioner(cmd.Context(), targetFlag, password, database)
			if err != nil {
				return err
			}
			defer closeFn()
			return runExtCreate(cmd.Context(), cmd.OutOrStdout(), ext, dbname, args[0])
		},
	}

	parent.AddCommand(list, create)
	return parent
}

// openExtensioner resolves and connects a Postgres target and returns its
// Extensioner view plus the database to act in and a close function. A
// non-Postgres target is refused before connecting.
func openExtensioner(ctx context.Context, targetFlag, password, database string) (db.Extensioner, string, func(), error) {
	t, err := resolveTarget(ctx, targetFlag)
	if err != nil {
		return nil, "", nil, err
	}
	if t.kind != db.KindPostgres {
		return nil, "", nil, errors.New("extensions are a PostgreSQL feature; the resolved target isn't Postgres")
	}
	engine, err := open(ctx, t, password)
	if err != nil {
		return nil, "", nil, err
	}
	ext, ok := engine.(db.Extensioner)
	if !ok {
		_ = engine.Close()
		return nil, "", nil, errors.New("this engine build doesn't support extensions")
	}
	dbname := database
	if dbname == "" {
		dbname = t.target.Database
	}
	return ext, dbname, func() { _ = engine.Close() }, nil
}

// runExtList prints the extensions the image offers, marking installed ones with
// their version. With installedOnly it drops the rest.
func runExtList(ctx context.Context, out io.Writer, ext db.Extensioner, database string, installedOnly bool) error {
	exts, err := ext.ListExtensions(ctx, database)
	if err != nil {
		return cliError(err)
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tINSTALLED\tDESCRIPTION")
	shown := 0
	for _, e := range exts {
		if installedOnly && !e.Installed() {
			continue
		}
		installed := "-"
		if e.Installed() {
			installed = e.InstalledVersion
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, installed, e.Comment)
		shown++
	}
	if shown == 0 {
		fmt.Fprintln(out, "No extensions to show.")
		return nil
	}
	return tw.Flush()
}

// runExtCreate installs name, but first checks it's actually available in this
// image — the image-capability awareness — so a missing extension gets a helpful
// explanation (and, for postgis, a pointer to `dbwiz setup postgis`) instead of a
// bare "could not open extension control file".
func runExtCreate(ctx context.Context, out io.Writer, ext db.Extensioner, database, name string) error {
	exts, err := ext.ListExtensions(ctx, database)
	if err != nil {
		return cliError(err)
	}
	var found *db.Extension
	for i := range exts {
		if exts[i].Name == name {
			found = &exts[i]
			break
		}
	}
	if found == nil {
		return extNotAvailableError(name)
	}
	if found.Installed() {
		fmt.Fprintf(out, "%s is already installed (version %s).\n", name, found.InstalledVersion)
		return nil
	}
	if err := ext.CreateExtension(ctx, database, name); err != nil {
		return cliError(err)
	}
	fmt.Fprintf(out, "Installed extension %s.\n", name)
	return nil
}

// extNotAvailableError explains that the running image doesn't ship an extension,
// with a concrete fix for the common ones (a capability-aware image).
func extNotAvailableError(name string) error {
	var hint string
	switch name {
	case "postgis", "postgis_topology", "postgis_raster":
		hint = " PostGIS needs a PostGIS image — run `dbwiz setup postgis` for a ready-made one."
	case "vector", "pgvector":
		hint = " pgvector needs a pgvector image (e.g. pgvector/pgvector)."
	}
	return fmt.Errorf("this PostgreSQL image doesn't provide the %q extension.%s", name, hint)
}
