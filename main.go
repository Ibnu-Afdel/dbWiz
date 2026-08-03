package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/cmd"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui"
)

// version is the build version reported by --version. It defaults to "dev" and
// is overridden at release time via the linker:
//
//	go build -ldflags "-X main.version=v1.0.0" .
//
// GoReleaser/the release workflow set it from the git tag.
var version = "dev"

// rootCmd is the base command for the CLI. With no subcommand it launches the
// TUI; subcommands (added from v2 on) stay available for scripting. Cobra still
// handles --help and --version without entering the TUI.
var rootCmd = &cobra.Command{
	Use:   "dbwiz",
	Short: "DBWiz — browse and manage local databases running in Docker",
	Long: "DBWiz detects the databases running in your local Docker containers and " +
		"lets you browse, administer, and query them from a friendly terminal UI.\n\n" +
		"Run with no arguments to launch the interactive UI.",
	Version: version,
	// Print just "dbwiz <version>" for --version rather than the default template.
	// Don't print usage on runtime errors from the TUI; usage is for arg errors.
	SilenceUsage: true,
	// main() prints the returned error once and exits non-zero; without this cobra
	// would also print it, so a failing subcommand would report twice.
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run()
	},
}

// main executes the root command.
func main() {
	// Scripting subcommands (v2 on) live in package cmd; the bare `dbwiz` still
	// launches the TUI via rootCmd's RunE.
	rootCmd.AddCommand(
		cmd.NewKeysCommand(),
		cmd.NewUseCommand(),
		cmd.NewListCommand(),
		cmd.NewSetupCommand(),
		cmd.NewCreateCommand(),
		cmd.NewDropCommand(),
		cmd.NewQueryCommand(),
		cmd.NewExplainCommand(),
		cmd.NewDumpCommand(),
		cmd.NewRestoreCommand(),
		cmd.NewURLCommand(),
		cmd.NewHealthCommand(),
		cmd.NewExtCommand(),
		cmd.NewSchemaCommand(),
		cmd.NewMigrationsCommand(),
	)
	// "dbwiz v1.0.0" rather than cobra's default "dbwiz version v1.0.0".
	rootCmd.SetVersionTemplate("dbwiz {{.Version}}\n")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
