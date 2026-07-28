package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/connect"
	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/health"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// NewHealthCommand builds `dbwiz health`: an end-to-end connectivity check
// (Docker daemon → container running → port reachable → auth → query round-trip)
// with a non-zero exit code when anything fails, so CI or a shell `&&` chain can
// gate on it (v3 1.3). It's the scriptable twin of the TUI's doctor screen.
//
// Unlike the other scripting commands it tolerates a stopped container or a dead
// daemon — reporting that state is the whole point — so it resolves its target
// without requiring the container to be up.
func NewHealthCommand() *cobra.Command {
	var targetFlag, password string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check a target end-to-end and exit non-zero if anything fails",
		Long: "Run DBWiz's connectivity self-check against a target: is the Docker daemon\n" +
			"reachable, is the container running, is its port open, do the credentials\n" +
			"authenticate, and does a trivial query round-trip.\n\n" +
			"Exits 0 when everything passes and non-zero otherwise, so it slots into CI or\n" +
			"a shell chain. Use --json for machine-readable output.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ht, err := resolveHealthTarget(cmd.Context(), targetFlag, password)
			if err != nil {
				return err
			}
			return runHealth(cmd.Context(), cmd.OutOrStdout(), ht, asJSON)
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "container to check (defaults to the used/only one)")
	cmd.Flags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	return cmd
}

// healthJSON is the machine-readable report shape.
type healthJSON struct {
	OK     bool            `json:"ok"`
	Checks []healthRowJSON `json:"checks"`
}

type healthRowJSON struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// runHealth renders the report and fails (non-zero exit) when any check failed.
// The report is always printed first, so the failure error just sets the exit
// code — the detail is already on screen.
func runHealth(ctx context.Context, out io.Writer, t health.Target, asJSON bool) error {
	report := health.Run(ctx, t)

	if asJSON {
		payload := healthJSON{OK: report.OK()}
		for _, c := range report.Checks {
			payload.Checks = append(payload.Checks, healthRowJSON{c.Name, c.Status.String(), c.Detail})
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
	} else {
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "CHECK\tSTATUS\tDETAIL")
		for _, c := range report.Checks {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, c.Status.String(), c.Detail)
		}
		tw.Flush()
	}

	if !report.OK() {
		return errors.New("health check failed")
	}
	return nil
}

// resolveHealthTarget picks the target to check, tolerating a stopped container
// (the health checks want to report that, not refuse to run). It mirrors
// resolveTarget's precedence — explicit --target, then the pinned/last target,
// then the sole detected container — but never requires the container to be up.
func resolveHealthTarget(ctx context.Context, targetFlag, password string) (health.Target, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if targetFlag == "" {
		if last := state.Load().Last; last != nil && last.Kind == state.KindSQLite && last.Path != "" {
			return health.Target{SQLite: true, Kind: db.KindSQLite, DBTarget: db.Target{Path: last.Path}}, nil
		}
	}
	c, err := findAnyContainer(ctx, targetFlag)
	if err != nil {
		return health.Target{}, err
	}
	kind, ok := connect.KindOf(c.Engine)
	if !ok {
		return health.Target{}, fmt.Errorf("container %q runs an engine DBWiz can't drive", c.Name)
	}
	target := connect.Target(ctx, c, kind)
	target.Password = passwordFor(password, cliTarget{target: target})
	port := target.Port
	if port == 0 {
		port = c.HostPort
	}
	return health.Target{Container: c.Name, Port: port, Kind: kind, DBTarget: target}, nil
}

// findAnyContainer resolves a container by name/pin/sole-detected without caring
// whether it's running.
func findAnyContainer(ctx context.Context, targetFlag string) (docker.Container, error) {
	containers, err := detect(ctx)
	if err != nil {
		return docker.Container{}, fmt.Errorf("couldn't scan Docker for databases: %w", err)
	}
	name := targetFlag
	if name == "" {
		if last := state.Load().Last; last != nil && last.Kind == state.KindDocker {
			name = last.Container
		}
	}
	if name != "" {
		for _, c := range containers {
			if c.Name == name {
				return c, nil
			}
		}
		return docker.Container{}, unknownContainerError(name, containers)
	}
	switch len(containers) {
	case 0:
		return docker.Container{}, errors.New("no target: no database container detected. Start one, or pass --target")
	case 1:
		return containers[0], nil
	default:
		return docker.Container{}, fmt.Errorf("multiple database containers detected (%s); pick one with `dbwiz use <name>` or pass --target", strings.Join(names(containers), ", "))
	}
}
