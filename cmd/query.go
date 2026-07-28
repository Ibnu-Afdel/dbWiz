package cmd

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/export"
)

// queryTimeout bounds a one-shot scripted query. It's generous — a report query
// can be slow — but bounded so a wedged server fails the command instead of
// hanging a pipeline.
const queryTimeout = 5 * time.Minute

// querier is the slice of the engine the query command uses. A narrow interface
// keeps the command testable with a tiny fake; db.Engine satisfies it.
type querier interface {
	Query(ctx context.Context, sql string) (db.Result, error)
}

// NewQueryCommand builds `dbwiz query "<sql>" [--json|--csv]`: a one-shot query
// against the target resolved from context. The default output is an aligned
// table (a NULL shows as the word NULL, distinct from an empty string); --json
// and --csv reuse the same serialisers the TUI's export action uses, so a result
// looks identical however it left DBWiz. A statement that returns no rows
// (INSERT/UPDATE/DDL) reports the affected-row count instead.
func NewQueryCommand() *cobra.Command {
	var (
		targetFlag, password string
		asJSON, asCSV        bool
	)
	cmd := &cobra.Command{
		Use:   "query <sql>",
		Short: "Run one SQL statement and print the result",
		Long: "Run a single SQL statement against the target resolved from context (the\n" +
			"container pinned by `dbwiz use`, an explicit --target, or the sole detected\n" +
			"one) and print the result.\n\n" +
			"Default output is an aligned table; --json emits an array of row objects and\n" +
			"--csv emits RFC 4180 CSV, both ready to pipe into jq or a spreadsheet. A\n" +
			"statement that returns no rows prints the number of rows affected.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := queryFormat(asJSON, asCSV)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), queryTimeout)
			defer cancel()
			t, err := resolveTarget(ctx, targetFlag)
			if err != nil {
				return err
			}
			engine, err := open(ctx, t, password)
			if err != nil {
				return err
			}
			defer engine.Close()
			return runQuery(ctx, cmd.OutOrStdout(), engine, args[0], format)
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	cmd.Flags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	cmd.Flags().BoolVar(&asCSV, "csv", false, "emit CSV instead of a table")
	return cmd
}

// queryFormat resolves the output flags to one format, rejecting the ambiguous
// combination rather than silently preferring one.
func queryFormat(asJSON, asCSV bool) (string, error) {
	if asJSON && asCSV {
		return "", fmt.Errorf("choose one of --json or --csv, not both")
	}
	switch {
	case asJSON:
		return "json", nil
	case asCSV:
		return "csv", nil
	default:
		return "table", nil
	}
}

// runQuery executes sql and renders the result in the chosen format. A result
// with columns is a row set; one without is a statement whose only outcome is a
// count of rows affected — reported in each format so a script can read it too.
func runQuery(ctx context.Context, out io.Writer, q querier, sql, format string) error {
	res, err := q.Query(ctx, sql)
	if err != nil {
		return cliError(err)
	}
	hasRows := len(res.Columns) > 0

	switch format {
	case "json":
		if !hasRows {
			fmt.Fprintf(out, "{\n  \"rows_affected\": %d\n}\n", res.RowsAffected)
			return nil
		}
		data, err := export.JSON(res.Columns, res.Rows)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "csv":
		if !hasRows {
			return writeAffectedCSV(out, res.RowsAffected)
		}
		data, err := export.CSV(res.Columns, res.Rows)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	default:
		return writeQueryTable(out, res, hasRows)
	}
}

// writeAffectedCSV emits a rows-affected count as a valid one-column CSV so a
// non-row statement still produces parseable --csv output.
func writeAffectedCSV(out io.Writer, affected int64) error {
	_, err := fmt.Fprintf(out, "rows_affected\n%d\n", affected)
	return err
}

// writeQueryTable renders a result as an aligned table with a summary footer, or
// — for a statement that returned no rows — just the affected-row count.
func writeQueryTable(out io.Writer, res db.Result, hasRows bool) error {
	if !hasRows {
		fmt.Fprintf(out, "OK — %d row(s) affected%s\n", res.RowsAffected, durationSuffix(res.Duration))
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	writeTabRow(tw, res.Columns)
	for _, row := range res.Rows {
		writeTabRow(tw, cellsToStrings(res.Columns, row))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "(%d row(s)%s)\n", len(res.Rows), durationSuffix(res.Duration))
	return nil
}

// writeTabRow writes one tab-separated record to the tabwriter.
func writeTabRow(tw io.Writer, cells []string) {
	for i, c := range cells {
		if i > 0 {
			fmt.Fprint(tw, "\t")
		}
		fmt.Fprint(tw, c)
	}
	fmt.Fprintln(tw)
}

// cellsToStrings formats a row's cells for the table: a NULL becomes the word
// NULL (distinct from an empty string), []byte and string are written verbatim,
// and anything else uses fmt.Sprint. Short cells missing from a row render empty.
func cellsToStrings(columns []string, row []any) []string {
	out := make([]string, len(columns))
	for i := range columns {
		if i >= len(row) {
			out[i] = ""
			continue
		}
		switch v := row[i].(type) {
		case nil:
			out[i] = "NULL"
		case []byte:
			out[i] = string(v)
		case string:
			out[i] = v
		default:
			out[i] = fmt.Sprint(v)
		}
	}
	return out
}

// durationSuffix renders a query duration for the table footer, or "" when it
// wasn't recorded.
func durationSuffix(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return ", " + d.Round(time.Millisecond).String()
}
