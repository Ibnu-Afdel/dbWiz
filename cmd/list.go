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
	"github.com/Ibnu-Afdel/dbwiz/internal/remote"
)

// NewListCommand builds `dbwiz list [--json]`: the machine-readable inventory of
// detected database containers. It's the scripting counterpart to the home
// screen's container list — a one-liner a script or CI step can pipe into `jq`
// (with --json) or read as an aligned table (the default). It never connects to
// a database, so it needs no credentials.
func NewListCommand() *cobra.Command {
	var asJSON bool
	var sshTarget string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List detected database containers",
		Long: "List the database containers DBWiz detects in Docker, machine-readable.\n\n" +
			"The default output is an aligned table; --json emits an array of objects\n" +
			"suitable for piping into jq. Pass --ssh user@host to scan a remote Docker\n" +
			"daemon over SSH instead of the local one. This is the non-interactive\n" +
			"counterpart to the container list on DBWiz's home screen; it never connects\n" +
			"to a database.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), cmd.OutOrStdout(), asJSON, sshTarget)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	cmd.Flags().StringVar(&sshTarget, "ssh", "", "scan a remote Docker daemon over SSH (user@host[:port])")
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
func runList(ctx context.Context, out io.Writer, asJSON bool, sshTarget string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	containers, err := scanContainers(ctx, sshTarget)
	if err != nil {
		return err
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

// scanContainers returns the detected database containers: locally by default,
// or on a remote Docker daemon over SSH when sshTarget is set — the same remote
// scan the TUI's home screen offers. A malformed SSH target is reported before
// any connection is attempted, and the target is named in a scan failure so a
// script can tell a bad host from an empty result.
func scanContainers(ctx context.Context, sshTarget string) ([]docker.Container, error) {
	if sshTarget == "" {
		containers, err := detect(ctx)
		if err != nil {
			return nil, fmt.Errorf("couldn't scan Docker for databases: %w", err)
		}
		return containers, nil
	}
	spec, err := remote.ParseSSH(sshTarget)
	if err != nil {
		return nil, err
	}
	containers, err := detectRemote(ctx, spec.DockerHost())
	if err != nil {
		return nil, fmt.Errorf("couldn't scan Docker on %s for databases: %w", spec, err)
	}
	return containers, nil
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
