package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// detect is the docker scan the use command runs, kept as a package var so tests
// can substitute a deterministic fake instead of shelling out to docker.
// Production never reassigns it.
var detect = docker.Detect

// detectRemote is the remote docker scan (over SSH) that `dbwiz list --ssh`
// runs, kept as a package var for the same reason as detect. Production never
// reassigns it.
var detectRemote = docker.DetectRemote

// NewUseCommand builds `dbwiz use <container>`: it pins a database container as
// the context DBWiz opens next, so a subsequent `dbwiz` launch offers to
// continue straight there. It's the non-interactive counterpart to picking a
// container in the TUI — handy in scripts and shell aliases.
func NewUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use <container>",
		Short: "Set the database container DBWiz opens next",
		Long: "Pin a detected database container as DBWiz's current context.\n\n" +
			"The next time you run `dbwiz`, the home screen offers to continue\n" +
			"straight to this container. Useful in scripts and quick launches where\n" +
			"you don't want to pick from the menu each time.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUse(cmd.Context(), cmd.OutOrStdout(), args[0])
		},
	}
}

// runUse resolves name against the detected containers and, on a match, records
// it as the last-used target. An unknown name fails with the list of names that
// were found, so a typo is self-correcting.
func runUse(ctx context.Context, out io.Writer, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	containers, err := detect(ctx)
	if err != nil {
		return fmt.Errorf("couldn't scan Docker for databases: %w", err)
	}

	for _, c := range containers {
		if c.Name != name {
			continue
		}
		if err := state.SetLastDocker(c.Name, string(c.Engine)); err != nil {
			return fmt.Errorf("couldn't save context: %w", err)
		}
		fmt.Fprintf(out, "Context set: dbwiz will offer %s (%s) next.\n", c.Name, c.Engine)
		if c.State != docker.StateRunning {
			fmt.Fprintf(out, "Note: %s isn't running — start it (from `dbwiz`, or `docker start %s`) before connecting.\n", c.Name, c.Name)
		}
		return nil
	}
	return unknownContainerError(name, containers)
}

// unknownContainerError reports that name wasn't found and lists the database
// containers that were, so the user can fix the name without a second command.
func unknownContainerError(name string, found []docker.Container) error {
	if len(found) == 0 {
		return fmt.Errorf("no database container named %q found (no database containers detected at all)", name)
	}
	names := make([]string, len(found))
	for i, c := range found {
		names[i] = c.Name
	}
	sort.Strings(names)
	return fmt.Errorf("no database container named %q found; detected: %s", name, strings.Join(names, ", "))
}
