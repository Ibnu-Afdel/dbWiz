package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// NewListCommand builds `dbwiz list [--json]`: the machine-readable inventory of
// detected database containers. It's the scripting counterpart to the home
// screen's container list — a one-liner a script or CI step can pipe into `jq`
// (with --json) or read as an aligned table (the default). It never connects to
// a database, so it needs no credentials.
func NewListCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List detected database containers",
		Long: "List the database containers DBWiz detects in Docker, machine-readable.\n\n" +
			"The default output is an aligned table; --json emits an array of objects\n" +
			"suitable for piping into jq. This is the non-interactive counterpart to the\n" +
			"container list on DBWiz's home screen; it never connects to a database.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), cmd.OutOrStdout(), asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	return cmd
}

// listRow is one container in the machine-readable list. It carries just the
// facts detection already recovered — no credentials — so the JSON is safe to
// pipe anywhere.
type listRow struct {
	Name    string `json:"name"`
	Engine  string `json:"engine"`
	State   string `json:"state"`
	Port    int    `json:"port,omitempty"`
	Image   string `json:"image"`
	Omarchy bool   `json:"omarchy"`
}

// runList scans Docker and renders the detected containers. An empty result is a
// success, not an error: --json prints `[]`, the table prints a short notice, so
// a script can tell "docker ran, nothing there" from "docker failed" by exit
// code alone.
func runList(ctx context.Context, out io.Writer, asJSON bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	containers, err := detect(ctx)
	if err != nil {
		return fmt.Errorf("couldn't scan Docker for databases: %w", err)
	}

	rows := make([]listRow, 0, len(containers))
	for _, c := range containers {
		rows = append(rows, listRow{
			Name:    c.Name,
			Engine:  string(c.Engine),
			State:   stateString(c.State),
			Port:    c.HostPort,
			Image:   c.Image,
			Omarchy: c.Source == docker.SourceOmarchy,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	if asJSON {
		data, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
		return nil
	}
	return writeListTable(out, rows)
}

// writeListTable prints the rows as an aligned, greppable table. With no
// containers it prints a one-line notice rather than a bare header, so the
// output is self-explanatory when run by hand.
func writeListTable(out io.Writer, rows []listRow) error {
	if len(rows) == 0 {
		fmt.Fprintln(out, "No database containers detected.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tENGINE\tSTATE\tPORT\tIMAGE")
	for _, r := range rows {
		port := "-"
		if r.Port != 0 {
			port = fmt.Sprint(r.Port)
		}
		name := r.Name
		if r.Omarchy {
			name += " *"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", name, r.Engine, r.State, port, r.Image)
	}
	return tw.Flush()
}

// stateString labels a container's run state for the list.
func stateString(s docker.ContainerState) string {
	if s == docker.StateRunning {
		return "running"
	}
	return "stopped"
}
