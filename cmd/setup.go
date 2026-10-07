package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/connect"
	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/dburl"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/provision"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// setupRun and setupWait are injection seams over the provision package so the
// command is testable without a live Docker; production never reassigns them.
var (
	setupRun   = provision.Run
	setupWait  = provision.WaitReady
	setupCheck = provision.Check
)

// setupReadyTimeout bounds how long `dbwiz setup` waits for a freshly started
// container to accept connections before reporting it as still coming up.
const setupReadyTimeout = 60 * time.Second

// NewSetupCommand builds `dbwiz setup <engine>`: it provisions the *container*,
// not just a database (v3 1.1). It reproduces Omarchy's stock defaults — same
// name, image, localhost port, and dev-friendly auth — so the result is
// indistinguishable from an Omarchy-provisioned server and connects with zero
// configuration. Unlike Omarchy it never escalates on its own: a
// socket-permission failure is reported with the fix, and sudo is used only
// when the user opts in with --sudo.
func NewSetupCommand() *cobra.Command {
	var noWait bool
	cmd := &cobra.Command{
		Use:   "setup [postgres|mysql|mariadb|postgis]",
		Short: "Provision a new database server container (Omarchy-compatible)",
		Long: "Create a new database server as a Docker container, using the same names,\n" +
			"images, ports, and dev-friendly auth as Omarchy's omarchy-install-docker-dbs,\n" +
			"so DBWiz and Omarchy provisioning are interchangeable.\n\n" +
			"Run with no engine to list the choices. If your user can't reach the Docker\n" +
			"socket (the Omarchy 4 default), add --sudo to go through sudo instead.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listSetupChoices(cmd.OutOrStdout())
			}
			return runSetup(cmd.Context(), cmd.OutOrStdout(), args[0], !noWait)
		},
	}
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "don't wait for the container to accept connections")
	return cmd
}

// listSetupChoices prints the provisionable engines and their dev-auth, so a bare
// `dbwiz setup` is a menu rather than an error.
func listSetupChoices(out io.Writer) error {
	fmt.Fprintln(out, "Provisionable database servers (dbwiz setup <engine>):")
	for _, s := range provision.Specs() {
		fmt.Fprintf(out, "  %-9s %-22s %s\n", s.Key, s.Image, s.Auth)
	}
	return nil
}

// runSetup provisions the container for engine: it pre-checks for a name/port
// clash (a clearer message than docker's), pulls + runs the image, waits for it
// to accept connections, records it as the next context, and prints how to
// connect.
func runSetup(ctx context.Context, out io.Writer, engine string, wait bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	spec, ok := provision.Lookup(engine)
	if !ok {
		return fmt.Errorf("unknown engine %q; choose one of: %s", engine, strings.Join(provision.Keys(), ", "))
	}

	containers, err := detect(ctx)
	if err != nil {
		return fmt.Errorf("couldn't scan Docker before provisioning: %w", err)
	}
	if c := setupCheck(ctx, spec, containers); c != nil {
		return setupConflictError(spec, c)
	}

	fmt.Fprintf(out, "Setting up %s as container %q (image %s)…\n", spec.Key, spec.Name, spec.Image)
	fmt.Fprintln(out, "Pulling the image if it isn't cached — this can take a minute.")
	id, err := setupRun(ctx, spec)
	if err != nil {
		return dockerCliError(err)
	}
	fmt.Fprintf(out, "Started %s (%s).\n", spec.Name, shortID(id))

	if wait {
		fmt.Fprintln(out, "Waiting for it to accept connections…")
		if !setupWait(ctx, spec.HostPort, setupReadyTimeout) {
			fmt.Fprintf(out, "It's up but not accepting connections on 127.0.0.1:%d yet — give it a moment.\n", spec.HostPort)
		}
	}

	// Best-effort: pin it as the next context so a bare `dbwiz` offers to continue
	// straight there. A failure here doesn't fail the provisioning.
	_ = state.SetLastDocker(spec.Name, string(spec.Engine))

	printSetupConnection(out, spec)
	return nil
}

// printSetupConnection tells the user how to reach the new server: its dev auth
// and a ready-to-paste connection URL built from the engine defaults (the same
// zero-config credentials detection will recover).
func printSetupConnection(out io.Writer, spec provision.Spec) {
	fmt.Fprintf(out, "\nReady. Auth: %s\n", spec.Auth)
	if kind, ok := connect.KindOf(spec.Engine); ok {
		t := db.Target{Host: "127.0.0.1", Port: spec.HostPort}
		connect.ApplyEngineDefaults(&t, kind)
		fmt.Fprintf(out, "Connect: %s\n", dburl.URL(kind, t))
	}
	fmt.Fprintf(out, "Open it in DBWiz: dbwiz (it's now the default), or `dbwiz use %s`.\n", spec.Name)
}

// setupConflictError turns a pre-flight conflict into an actionable error: a name
// clash likely means it's already set up; a port clash points at the holder.
func setupConflictError(spec provision.Spec, c *provision.Conflict) error {
	switch c.Kind {
	case provision.NameTaken:
		return fmt.Errorf("%s looks already set up: %s. Start it with `docker start %s`, or remove it (`docker rm -f %s`) to reprovision", spec.Key, c.Detail, spec.Name, spec.Name)
	case provision.PortTaken:
		return fmt.Errorf("can't set up %s: %s. Stop whatever holds host port %d, then retry (MySQL and MariaDB both want 3306; Postgres and PostGIS both want 5432)", spec.Key, c.Detail, spec.HostPort)
	}
	return fmt.Errorf("can't set up %s: %s", spec.Key, c.Detail)
}

// dockerCliError renders a docker.DockerError as a one-line CLI message carrying
// its Detail and Hint (which Error() alone drops), so a provisioning failure
// reads clearly on stderr. Other errors pass through unchanged.
func dockerCliError(err error) error {
	var de *docker.DockerError
	if !errors.As(err, &de) {
		return err
	}
	msg := de.Title
	if de.Detail != "" {
		msg += ": " + de.Detail
	}
	if de.Hint != "" {
		msg += " (" + de.Hint + ")"
	}
	return errors.New(msg)
}

// shortID trims a docker container id to the conventional 12-char short form.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
