package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/explain"
)

// explainTimeout bounds a plan. A plain EXPLAIN is nearly instant; --analyze runs
// the statement, so this matches the query command's generous-but-bounded limit
// rather than a short one.
const explainTimeout = 5 * time.Minute

// NewExplainCommand builds `dbwiz explain "<sql>"`: the query plan for a
// statement, rendered as a readable tree with plain-language notes about what is
// expensive in it (v4 2.3).
func NewExplainCommand() *cobra.Command {
	var (
		targetFlag, password   string
		analyze, asJSON, asRaw bool
	)
	cmd := &cobra.Command{
		Use:   "explain <sql>",
		Short: "Show how the engine will run a statement",
		Long: "Print the query plan for a statement against the target resolved from context\n" +
			"(the container pinned by `dbwiz use`, an explicit --target, or the sole\n" +
			"detected one), as an indented tree with the row counts and costs the engine\n" +
			"reported, followed by notes on what stands out.\n\n" +
			"By default these are the planner's estimates and nothing is executed.\n" +
			"--analyze runs the statement and reports what actually happened, which is\n" +
			"far more useful — and is refused on a statement that writes, because\n" +
			"measuring a DELETE means performing it.\n\n" +
			"--json emits the same plan and notes for a script; --raw prints the engine's\n" +
			"own EXPLAIN output untouched.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON && asRaw {
				return errors.New("choose one of --json or --raw, not both")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), explainTimeout)
			defer cancel()
			return runExplain(ctx, cmd.OutOrStdout(), explainOpts{
				target:    targetFlag,
				password:  password,
				statement: args[0],
				analyze:   analyze,
				asJSON:    asJSON,
				asRaw:     asRaw,
			})
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	cmd.Flags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	cmd.Flags().BoolVar(&analyze, "analyze", false, "run the statement and report measured rows and timings")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a text report")
	cmd.Flags().BoolVar(&asRaw, "raw", false, "print the engine's own EXPLAIN output verbatim")
	return cmd
}

// explainOpts is one resolved `explain` invocation, kept as a struct so
// runExplain stays testable without a cobra command.
type explainOpts struct {
	target    string
	password  string
	statement string
	analyze   bool
	asJSON    bool
	asRaw     bool
}

// runExplain connects, captures the plan, and writes it in the chosen form.
func runExplain(ctx context.Context, out io.Writer, o explainOpts) error {
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

	plan, err := explain.Explain(ctx, engine, o.statement, explain.Options{Analyze: o.analyze})
	if err != nil {
		return cliError(err)
	}

	switch {
	case o.asRaw:
		raw := strings.TrimRight(plan.Raw, "\n")
		_, err := fmt.Fprintln(out, raw)
		return err
	case o.asJSON:
		data, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	default:
		_, err := io.WriteString(out, explain.Render(plan))
		return err
	}
}
