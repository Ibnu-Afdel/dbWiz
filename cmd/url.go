package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/dburl"
)

// setClipboard is an injection seam over the OS clipboard so --clipboard is
// testable and degrades gracefully on a headless box; production never
// reassigns it.
var setClipboard = clipboard.WriteAll

// NewURLCommand builds `dbwiz url [database]`: it prints a ready-to-paste
// connection string for the resolved target in one of three shapes — a single
// DATABASE_URL (default), a generic DB_* block, or a JDBC URL (v3 1.2). This is
// the "wire this into my .env" convenience; the TUI's [y] yank is its interactive
// twin.
func NewURLCommand() *cobra.Command {
	var targetFlag, password, format string
	var toClipboard bool
	cmd := &cobra.Command{
		Use:   "url [database]",
		Short: "Print a connection string for a target (url, env, or jdbc)",
		Long: "Print a ready-to-paste connection string for the resolved database target.\n\n" +
			"--format selects the shape: url (a single DATABASE_URL, the default), env (a\n" +
			"generic DB_* block), jdbc (a JDBC URL), or all (every shape). Pass a database\n" +
			"name to override the target's default. The recovered password is included, so\n" +
			"treat the output as a credential.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbname := ""
			if len(args) == 1 {
				dbname = args[0]
			}
			t, err := resolveTarget(cmd.Context(), targetFlag)
			if err != nil {
				return err
			}
			return runURL(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), t, dbname, password, format, toClipboard)
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	cmd.Flags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	cmd.Flags().StringVarP(&format, "format", "f", "url", "output shape: url, env, jdbc, or all")
	cmd.Flags().BoolVar(&toClipboard, "clipboard", false, "also copy the output to the system clipboard")
	return cmd
}

// runURL renders the connection string(s) for the target and prints them, with
// the database name and password resolved the same way every scripting command
// does.
func runURL(ctx context.Context, out, stderr io.Writer, t cliTarget, dbname, password, format string, toClipboard bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	target := t.target
	target.Password = passwordFor(password, t)
	if dbname != "" && !t.sqlite {
		target.Database = dbname
	}

	formats, err := parseURLFormats(format)
	if err != nil {
		return err
	}
	blocks := make([]string, 0, len(formats))
	for _, f := range formats {
		s, err := dburl.Render(t.kind, target, f)
		if err != nil {
			return err
		}
		blocks = append(blocks, s)
	}
	text := strings.Join(blocks, "\n\n")
	fmt.Fprintln(out, text)

	if toClipboard {
		if err := setClipboard(text); err != nil {
			// A headless machine has no clipboard; the text already went to stdout, so
			// this is a note, not a failure.
			fmt.Fprintf(stderr, "(couldn't copy to clipboard: %v)\n", err)
		} else {
			fmt.Fprintln(stderr, "Copied to clipboard.")
		}
	}
	return nil
}

// parseURLFormats resolves the --format value, expanding "all" to every shape in
// display order.
func parseURLFormats(format string) ([]dburl.Format, error) {
	if format == "all" {
		return dburl.Formats(), nil
	}
	f, err := dburl.ParseFormat(format)
	if err != nil {
		return nil, err
	}
	return []dburl.Format{f}, nil
}
